const {test} = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
let {spawn} = require('node:child_process');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const {createHash} = require('node:crypto');
const repo = path.join(__dirname,'..');
const entry = path.join(repo,'scripts','agent-task-notify.ps1');
const quote = x => "'"+x.replaceAll("'","''")+"'";
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms));

function processRun(args, input='', timeout=15000) {
  return new Promise((resolve,reject)=> {
    const child=spawn('pwsh',args,{cwd:repo,windowsHide:true,stdio:['pipe','pipe','pipe']});
    let out='',err='',exitedAt;
    child.stdout.on('data',x=>out+=x.toString('utf8'));child.stderr.on('data',x=>err+=x.toString('utf8'));
    child.on('exit',()=>exitedAt=Date.now());
    const timer=setTimeout(()=>{child.kill();reject(new Error('Process/stdout did not close independently'));},timeout);
    child.on('error',reject);
    child.on('close',code=>{clearTimeout(timer);resolve({code,out:out.replace(/\x1b\[[0-9;?]*[a-zA-Z]/g,''),err,exitedAt,closedAt:Date.now()});});
    child.stdin.end(input);
  });
}
async function script(code) {
  const result=await processRun(['-NoProfile','-NonInteractive','-Command','-'],code+'\n');
  assert.equal(result.code,0,result.err);assert.equal(result.err,'');return result;
}
function signaledProcess(code,timeout=15000) {
  const child=spawn('pwsh',['-NoProfile','-NonInteractive','-Command',code],{cwd:repo,windowsHide:true,stdio:['pipe','pipe','pipe']});
  let out='',err='',closed=false,timedOut=false;
  const listeners=new Set();
  const changed=()=>{for(const listener of listeners)listener();};
  const completion=new Promise(resolve=> {
    const timer=setTimeout(()=>{timedOut=true;child.kill();},timeout);
    child.stdout.on('data',chunk=>{out+=chunk.toString('utf8');changed();});
    child.stderr.on('data',chunk=>err+=chunk.toString('utf8'));
    child.on('error',()=>{err='Test subprocess could not start';});
    child.on('close',code=>{closed=true;clearTimeout(timer);changed();resolve({code,stderrEmpty:err==='',timedOut});});
  });
  return {
    completion,
    waitFor(pattern) {
      return new Promise((resolve,reject)=> {
        const timer=setTimeout(()=>finish(new Error('Test subprocess signal timed out')),10000);
        const finish=(error,match)=>{clearTimeout(timer);listeners.delete(check);error?reject(error):resolve(match);};
        const check=()=> {
          const match=out.split(/\r?\n/).map(line=>line.match(pattern)).find(Boolean);
          if(match)finish(null,match);
          else if(closed)finish(new Error('Test subprocess closed before its fixed signal'));
        };
        listeners.add(check);check();
      });
    },
    release(){child.stdin.end('ATN_RELEASE\n');},
    async stop(){if(!closed)child.kill();await completion;}
  };
}
async function setupLockProcess(children,code,timeout=15000) {
  const setup=signaledProcess("$ErrorActionPreference='Stop'; "+code,timeout);
  children.push(setup);
  assert.deepEqual(await setup.completion,{code:0,stderrEmpty:true,timedOut:false});
}
async function hook(directory,event) {
  // CP936 intentionally contradicts the UTF-8 bytes written to stdin.
  const command=`[Console]::InputEncoding=[Text.Encoding]::GetEncoding(936); & ${quote(entry)} -Mode Hook -Agent codex -DataDirectory ${quote(directory)}`;
  const result=await processRun(['-NoProfile','-NonInteractive','-Command',command],Buffer.from(JSON.stringify(event),'utf8'));
  assert.equal(result.code,0,result.err);assert.equal(result.err,'');assert.deepEqual(JSON.parse(result.out),{continue:true});return result;
}
async function jobs(directory) {
  let names=[];try { names=await fs.readdir(path.join(directory,'jobs')); } catch { return []; }
  return Promise.all(names.filter(n=>n.endsWith('.json')).map(async name=>({key:name.slice(0,-5),...JSON.parse(await fs.readFile(path.join(directory,'jobs',name),'utf8'))})));
}
const durableStateTimeoutMessage='Expected durable state was not reached';
const durableStateTimeoutCode='ATN_TEST_DURABLE_STATE_TIMEOUT';
async function eventually(read,predicate,timeout=15000) {
  const deadline=Date.now()+timeout;
  while(Date.now()<deadline) { const value=await read();if(predicate(value))return value;await delay(100); }
  throw Object.assign(new Error(durableStateTimeoutMessage),{code:durableStateTimeoutCode});
}
const retryCapStatus=new Set(['pending','sending','sent','failed']);
const retryCapDiagnostic=new Set(['http:400','http:401','http:403','http:404','http:408','http:429','http:500','http:502','http:503','http:504','ambiguous-send','spawn-failed','credential','worker-error']);
const retryCapBound=value=>Number.isFinite(value)?Math.min(180000,Math.max(0,Math.floor(value))):0;
function retryCapSnapshot(rawJobs,rawRequestElapsedMs,rawElapsedMs) {
  const jobs=Array.isArray(rawJobs)?rawJobs.slice(0,2):[];
  const requests=Array.isArray(rawRequestElapsedMs)?rawRequestElapsedMs.slice(0,6):[];
  return {
    schemaVersion:1,
    jobCount:jobs.length,
    jobs:jobs.map(job=>({
      status:retryCapStatus.has(job?.status)?job.status:'unknown',
      attempts:Number.isFinite(job?.attempts)?Math.min(5,Math.max(0,Math.floor(job.attempts))):0,
      diagnostic:job?.diagnostic==null||job.diagnostic===''?'none':retryCapDiagnostic.has(job?.diagnostic)?job.diagnostic:'other'
    })),
    requestCount:requests.length,
    requestElapsedMs:requests.map(retryCapBound),
    elapsedMs:retryCapBound(rawElapsedMs)
  };
}
function retryCapFailure(error,rawJobs,rawRequestElapsedMs,rawElapsedMs) {
  if(error?.code!==durableStateTimeoutCode||error.message!==durableStateTimeoutMessage)return error;
  return new Error(`${durableStateTimeoutMessage}; retry-cap-snapshot=${JSON.stringify(retryCapSnapshot(rawJobs,rawRequestElapsedMs,rawElapsedMs))}`);
}

const durableWaitPhases=new Set(['retry-cap','extension','permanent']);
const durableExtensionStatus=new Set(['none','pending','sending','sent','failed','ambiguous']);
const durableExtensionDiagnostic=new Set([...retryCapDiagnostic,'ambiguous-extension','transport','malformed-response']);
function durableWaitSnapshot(phase,rawJobs,rawRequestElapsedMs,rawElapsedMs) {
  const base=retryCapSnapshot(rawJobs,rawRequestElapsedMs,rawElapsedMs);
  return {
    ...base,phase:durableWaitPhases.has(phase)?phase:'other',
    jobs:base.jobs.map((job,index)=>({
      ...job,
      extensionStatus:durableExtensionStatus.has(rawJobs[index]?.extensionStatus)?rawJobs[index].extensionStatus:'unknown',
      extensionDiagnostic:rawJobs[index]?.extensionDiagnostic==null||rawJobs[index].extensionDiagnostic===''?'none':durableExtensionDiagnostic.has(rawJobs[index].extensionDiagnostic)?rawJobs[index].extensionDiagnostic:'other'
    }))
  };
}
function durableWaitFailure(error,phase,rawJobs,rawRequestElapsedMs,rawElapsedMs) {
  if(error?.code!==durableStateTimeoutCode||error.message!==durableStateTimeoutMessage)return error;
  if(phase==='retry-cap')return retryCapFailure(error,rawJobs,rawRequestElapsedMs,rawElapsedMs);
  return Object.assign(new Error(`${durableStateTimeoutMessage}; durable-wait-snapshot=${JSON.stringify(durableWaitSnapshot(phase,rawJobs,rawRequestElapsedMs,rawElapsedMs))}`),{code:durableStateTimeoutCode});
}
async function observedDurableWait(read,predicate,timeout,phase,requestTimes,startedAt) {
  let last=[];
  try {return await eventually(async()=>{const value=await read();last=value;return value;},predicate,timeout);}
  catch(error) {
    if(error?.code!==durableStateTimeoutCode||error.message!==durableStateTimeoutMessage)throw error;
    // Never add a diagnostic filesystem read that can mask this timeout.
    throw durableWaitFailure(error,phase,last,requestTimes().map(at=>at-startedAt),Date.now()-startedAt);
  }
}

test('retry-cap diagnostic snapshot is fixed-schema, bounded, and redacted',()=> {
  const snapshot=retryCapSnapshot([
    {status:'PRIVATE-pending',attempts:-4,diagnostic:'https://private.example/secret',jobKey:'PRIVATE_JOB_KEY'},
    {status:'failed',attempts:99,diagnostic:'http:503',rawResponse:'PRIVATE_BODY'},
    {status:'sent',attempts:1,diagnostic:'ambiguous-send'}
  ],[-5,1.9,999999,2,3,4,5],999999);
  assert.deepEqual(snapshot,{
    schemaVersion:1,
    jobCount:2,
    jobs:[
      {status:'unknown',attempts:0,diagnostic:'other'},
      {status:'failed',attempts:5,diagnostic:'http:503'}
    ],
    requestCount:6,
    requestElapsedMs:[0,1,180000,2,3,4],
    elapsedMs:180000
  });
  assert.doesNotMatch(JSON.stringify(snapshot),/PRIVATE|https|secret|jobKey|rawResponse/);
});

test('retry-cap diagnostic decorates only the durable-state timeout',()=> {
  const timeout=new Error('Expected durable state was not reached');
  timeout.code='ATN_TEST_DURABLE_STATE_TIMEOUT';
  const decorated=retryCapFailure(timeout,[],[],0);
  assert.notEqual(decorated,timeout);
  assert.match(decorated.message,/retry-cap-snapshot=/);
  const readFailure=new SyntaxError('unexpected synthetic JSON');
  assert.equal(retryCapFailure(readFailure,[],[],0),readFailure);
  assert.equal(readFailure.message,'unexpected synthetic JSON');
});

test('retry-cap diagnostic distinguishes no diagnostic and fixed startup failures',()=> {
  const diagnostics=[null,'','spawn-failed','credential','worker-error','ambiguous-send','PRIVATE_FAILURE'];
  assert.deepEqual(diagnostics.map(diagnostic=>retryCapSnapshot([{status:'pending',attempts:0,diagnostic}],[],0).jobs[0].diagnostic),['none','none','spawn-failed','credential','worker-error','ambiguous-send','other']);
});

test('durable-wait diagnostic phases bound and redact extension state',()=> {
  const raw=[{status:'sent',attempts:99,diagnostic:null,extensionStatus:'sending',extensionDiagnostic:'ambiguous-extension',key:'PRIVATE_ID',body:'PRIVATE_BODY'},
    {status:'PRIVATE_STATUS',attempts:-1,diagnostic:'https://PRIVATE/TOKEN',extensionStatus:'PRIVATE_EXTENSION',extensionDiagnostic:'PRIVATE_PATH'}];
  for(const phase of ['extension','permanent','PRIVATE_PHASE']) {
    const snapshot=durableWaitSnapshot(phase,raw,[1,999999,-1,2,3,4,5],Infinity);
    assert.deepEqual(snapshot,{
      schemaVersion:1,phase:phase==='PRIVATE_PHASE'?'other':phase,jobCount:2,
      jobs:[{status:'sent',attempts:5,diagnostic:'none',extensionStatus:'sending',extensionDiagnostic:'ambiguous-extension'},
        {status:'unknown',attempts:0,diagnostic:'other',extensionStatus:'unknown',extensionDiagnostic:'other'}],
      requestCount:6,requestElapsedMs:[1,180000,0,2,3,4],elapsedMs:0
    });
    assert.doesNotMatch(JSON.stringify(snapshot),/PRIVATE|https|TOKEN|body|key/);
  }
  for(const status of ['none','pending','sending','sent','failed','ambiguous']) {
    assert.equal(durableWaitSnapshot('extension',[{extensionStatus:status}],[],0).jobs[0].extensionStatus,status);
  }
});

test('durable-wait diagnostics preserve cap format and unknown error identity',()=> {
  const timeout=Object.assign(new Error(durableStateTimeoutMessage),{code:durableStateTimeoutCode});
  assert.equal(durableWaitFailure(timeout,'retry-cap',[],[],0).message,retryCapFailure(timeout,[],[],0).message);
  for(const phase of ['extension','permanent']) {
    const decorated=durableWaitFailure(timeout,phase,[],[],0);
    assert.match(decorated.message,new RegExp(`"phase":"${phase}"`));
    assert.equal(decorated.code,durableStateTimeoutCode);
    for(const error of [new SyntaxError('PRIVATE_PARSE'),Object.assign(new Error('PRIVATE_MESSAGE'),{code:durableStateTimeoutCode}),Object.assign(new Error(durableStateTimeoutMessage),{code:'PRIVATE_CODE'})]) {
      assert.equal(durableWaitFailure(error,phase,[],[],0),error);
    }
  }
});

test('durable-wait observer decorates only timeout from last successful read',async()=> {
  let reads=0,timings=0;
  const read=async()=>{reads++;if(reads===2)throw Object.assign(new Error(durableStateTimeoutMessage),{code:durableStateTimeoutCode});return [{status:'sent',attempts:1,extensionStatus:'pending',extensionDiagnostic:null}];};
  await assert.rejects(observedDurableWait(read,()=>false,15000,'extension',()=>{timings++;return [Date.now()];},Date.now()),error=>{
    assert.equal(error.code,durableStateTimeoutCode);
    assert.match(error.message,/"phase":"extension"/);
    assert.match(error.message,/"extensionStatus":"pending"/);
    return true;
  });
  assert.equal(reads,2,'A timeout must not perform an additional diagnostic read');assert.equal(timings,1);
  const failure=new SyntaxError('PRIVATE_READ_ERROR');
  await assert.rejects(observedDurableWait(async()=>{throw failure;},()=>false,15000,'permanent',()=>{throw new Error('diagnostic must not run');},Date.now()),error=>error===failure);
  const wanted=[{status:'failed'}];
  assert.equal(await observedDurableWait(async()=>wanted,()=>true,15000,'permanent',()=>{throw new Error('success must not read timings');},Date.now()),wanted);
});

test('lock setup timeout closes before temporary directory removal',{timeout:15000},async t=> {
  const temporaryParent=path.resolve(os.tmpdir());
  const directory=await fs.mkdtemp(path.join(temporaryParent,'atn-lock-'));
  const children=[],observed=[],sequence=[];
  const originalSpawn=spawn;
  let output='';
  // Observe real close events independently of the setup helper, including RED cleanup.
  spawn=(...args)=> {
    const child=originalSpawn(...args),record={child,closed:false};
    record.completion=new Promise(resolve=>child.once('close',()=>{record.closed=true;sequence.push('close');resolve();}));
    child.stdout.on('data',chunk=>output+=chunk.toString('utf8'));
    observed.push(record);
    return child;
  };
  try {
    await assert.rejects(setupLockProcess(children,`$held=[IO.File]::Open(${quote(path.join(directory,'synthetic.lock'))},[IO.FileMode]::Create,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None); try { [Console]::WriteLine('ATN_SETUP_READY'); Start-Sleep -Seconds 60 } finally { $held.Dispose() }`,5000));
    assert.match(output,/ATN_SETUP_READY/,'The real setup must reach its open-file gate before timing out');
    await Promise.all(children.map(child=>child.stop()));
    assert.deepEqual([...sequence],['close'],'Setup close must precede permission to remove its temporary directory');
  } finally {
    spawn=originalSpawn;
    assert.equal(spawn,originalSpawn,'The real spawn function is restored');
    // Also reap independently observed children if the regression fails before registration.
    for(const record of observed)if(!record.closed)record.child.kill();
    await Promise.all(observed.map(record=>record.completion));
    const absolute=path.resolve(directory);
    assert.equal(path.dirname(absolute),temporaryParent);
    assert.ok(path.basename(absolute).startsWith('atn-lock-'));
    sequence.push('remove');
    await fs.rm(absolute,{recursive:true,force:true});
  }
  assert.deepEqual(sequence,['close','remove']);
  assert.equal(await fs.stat(directory).then(()=>true,()=>false),false);
  t.diagnostic('setup-timeout order=close,remove directoryRemoved=true spawnRestored=true');
});

for(const releaseShortLock of [true,false]) {
  test(releaseShortLock?'real worker waits through a short maintenance lock and sends once':'real worker bounds startup waiting while a long lock keeps the job pending',{timeout:30000},async t=> {
    const temporaryParent=path.resolve(os.tmpdir());
    const directory=await fs.mkdtemp(path.join(temporaryParent,'atn-lock-'));
    const key='b'.repeat(64),children=[];
    let requests=0;
    const server=http.createServer(async(req,res)=>{for await(const chunk of req){}requests++;res.writeHead(200,{'Content-Type':'application/json'});res.end('{"code":200}');});
    try {
      await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
      await setupLockProcess(children,`$runtime=Import-Module ./src/Runtime.psm1 -PassThru; Import-Module ./src/Storage.psm1; Set-ATNCredential ${quote(directory)} bark @{endpoint='http://127.0.0.1:${server.address().port}/synthetic-key'}; & $runtime { param($directory,$key) $job=New-ATNJob codex (Get-ATNSettings $directory) 300 stopped ([DateTimeOffset]::UtcNow) -RingSeconds 30; Write-ATNJson (Join-Path $directory "jobs/$key.json") $job } ${quote(directory)} ${quote(key)}`);
      const before=await jobs(directory);
      const holder=signaledProcess(`$ErrorActionPreference='Stop'; Import-Module ./src/Storage.psm1; $held=Enter-ATNLock ${quote(path.join(directory,'jobs',key+'.json.lock'))}; if($null -eq $held){throw 'Synthetic lock unavailable'}; try { [Console]::WriteLine('ATN_LOCK_READY'); if([Console]::ReadLine() -cne 'ATN_RELEASE'){throw 'Synthetic release missing'} } finally { $held.Dispose() }; [Console]::WriteLine('ATN_LOCK_RELEASED')`);
      children.push(holder);
      await holder.waitFor(/^ATN_LOCK_READY$/);
      const worker=signaledProcess(`$ErrorActionPreference='Stop'; $runtime=Import-Module ./src/Runtime.psm1 -PassThru; & $runtime {
        param($directory,$key)
        $originalLock=Get-Command Enter-ATNLock
        try {
          function Enter-ATNLock {
            param([string]$Path,[int]$WaitMilliseconds=0)
            [Console]::WriteLine('ATN_WORKER_LOCK:' + $WaitMilliseconds)
            & $originalLock -Path $Path -WaitMilliseconds $WaitMilliseconds
          }
          $clock=[Diagnostics.Stopwatch]::StartNew()
          Invoke-ATNWorker $key $directory
          [Console]::WriteLine('ATN_WORKER_DONE:' + $clock.ElapsedMilliseconds)
        } finally { Set-Item Function:Enter-ATNLock -Value $originalLock.ScriptBlock }
      } ${quote(directory)} ${quote(key)}`);
      children.push(worker);
      const entered=await worker.waitFor(/^ATN_WORKER_LOCK:([0-9]+)$/);
      assert.equal(Number(entered[1]),2000,'Worker must pass the bounded wait to the real job lock');
      if(releaseShortLock) {
        await delay(100);
        assert.equal(requests,0,'No request may start while maintenance owns the lock');
        holder.release();
        await holder.waitFor(/^ATN_LOCK_RELEASED$/);
        assert.deepEqual(await holder.completion,{code:0,stderrEmpty:true,timedOut:false});
      }
      const finished=await worker.waitFor(/^ATN_WORKER_DONE:([0-9]+)$/);
      assert.deepEqual(await worker.completion,{code:0,stderrEmpty:true,timedOut:false});
      const elapsed=Number(finished[1]);
      const after=await jobs(directory);
      if(releaseShortLock) {
        assert.ok(elapsed>=100&&elapsed<5000,'Short lock must be acquired within the bounded process budget');
        assert.equal(requests,1);assert.equal(after.length,1);
        assert.equal(after[0].status,'sent');assert.equal(after[0].attempts,1);
      } else {
        assert.ok(elapsed>=1900&&elapsed<5000,'Long lock wait must take about two seconds, not return immediately or wait forever');
        assert.equal(requests,0);assert.deepEqual(after,before,'Long lock must leave the pending job untouched');
      }
      t.diagnostic(`startup-lock waitMilliseconds=2000 elapsedMs=${elapsed} requestCount=${requests} status=${after[0].status} attempts=${after[0].attempts}`);
    } finally {
      await Promise.all(children.map(child=>child.stop()));
      if(server.listening)await new Promise(resolve=>server.close(resolve));
      const absolute=path.resolve(directory);
      assert.equal(path.dirname(absolute),temporaryParent,'Cleanup is confined to this test temporary parent');
      assert.ok(path.basename(absolute).startsWith('atn-lock-'),'Cleanup is confined to this generated directory');
      await fs.rm(absolute,{recursive:true,force:true});
    }
  });
}

test('invalid input is neutral and Doctor exposes only bounded fixed input classes', async()=> {
  const directory=await fs.mkdtemp(path.join(os.tmpdir(),'atn-input-'));
  try {
    for (const input of [Buffer.from([0xff]), '{PRIVATE', ' ', '{}', '{"session_id":"synthetic","turn_id":17,"hook_event_name":"UserPromptSubmit","permission":"approve"}']) {
      const result=await processRun(['-NoProfile','-File',entry,'-Mode','Hook','-Agent','codex','-DataDirectory',directory],input);
      assert.equal(result.code,0);assert.equal(result.err,'');assert.deepEqual(JSON.parse(result.out),{continue:true});
    }
    const doctor=await processRun(['-NoProfile','-File',entry,'-Mode','Doctor','-DataDirectory',directory]);
    assert.equal(doctor.code,0,doctor.err);
    assert.deepEqual(JSON.parse(doctor.out).input,{'invalid-utf8':1,'invalid-json':2,'invalid-shape':2});
    assert.doesNotMatch(doctor.out,/PRIVATE|synthetic|approve|atn-input|https/);
    assert.equal(await fs.stat(path.join(directory,'runs')).then(()=>true,()=>false),false);
  } finally {await fs.rm(directory,{recursive:true,force:true});}
});

test('real hook strictly decodes UTF-8, detaches handles, retries and extends a frozen safe payload', {timeout:60000}, async()=> {
  assert.equal(await fs.stat(entry).then(()=>true,()=>false),true,'Runtime entry point must exist');
  const directory=await fs.mkdtemp(path.join(os.tmpdir(),"atn process ' space-"));
  const received=[];let attempt=0;
  const server=http.createServer(async(req,res)=> {
    const chunks=[];for await(const chunk of req)chunks.push(chunk);
    received.push({at:Date.now(),body:JSON.parse(Buffer.concat(chunks).toString('utf8'))});
    const current=++attempt;
    if(current===1)await delay(2000);
    res.writeHead(current===1?503:200,{'Content-Type':'application/json'});res.end('{"code":200}');
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  try {
    await script(`Import-Module ./src/Storage.psm1; Write-ATNJson ${quote(path.join(directory,'settings.json'))} @{minSeconds=1;longTaskSeconds=2;mediumRingSeconds=31;longRingSeconds=31}; Set-ATNCredential ${quote(directory)} bark @{endpoint='http://127.0.0.1:${server.address().port}/synthetic-key'}`);
    const dry=await processRun(['-NoProfile','-File',entry,'-Mode','Preview','-Agent','codex','-DataDirectory',directory]);
    assert.equal(dry.code,0,dry.err);assert.match(JSON.parse(dry.out).title,/Codex/);assert.equal(received.length,0);assert.equal((await jobs(directory)).length,0);
    const input={hook_event_name:'UserPromptSubmit',session_id:'合成会话🙂',turn_id:'合成轮次🚀',prompt:'PRIVATE_PROMPT_DO_NOT_STORE',cwd:'PRIVATE_PATH_DO_NOT_STORE'};
    await hook(directory,input);await delay(1100);
    const expectedSessionKey=createHash('sha256').update('["codex","合成会话🙂"]','utf8').digest('hex');
    assert.ok((await fs.readdir(path.join(directory,'sessions'))).includes(expectedSessionKey+'.json'),'Identity hashes correctly decoded Unicode, not consistent mojibake');
    const extensionStartedAt=Date.now();
    const ended=await hook(directory,{...input,hook_event_name:'Stop'});
    const pending=(await eventually(()=>jobs(directory),j=>j.length===1))[0];
    // Additional workers run while the first worker owns the durable job lock.
    const duplicates=await Promise.all([0,1].map(()=>processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey',pending.key])));
    for(const duplicate of duplicates)assert.equal(duplicate.code,0,duplicate.err);
    await fs.writeFile(path.join(directory,'settings.json'),JSON.stringify({provider:'ntfy',minSeconds:999,longTaskSeconds:2000,sound:'changed',continuous:false}));
    const done=(await observedDurableWait(()=>jobs(directory),j=>j[0]?.extensionStatus==='sent',25000,'extension',()=>received.map(request=>request.at),extensionStartedAt))[0];
    assert.equal(done.status,'sent');assert.equal(done.attempts,2);assert.equal(received.length,3);
    assert.ok(ended.closedAt-ended.exitedAt<1000,'Worker must not hold Hook stdout open');
    assert.ok(received[1].at>ended.closedAt,'Worker remains alive after Hook stdout closes');
    assert.ok(received[1].at-received[0].at>=6800,'Retry waits after failed response');
    assert.ok(received[2].at-received[1].at>=850,'One extension at target minus 30');
    for(const request of received) {
      assert.match(request.body.title,/Codex/);assert.match(request.body.body,/任务已停止/);assert.equal(request.body.call,1);assert.equal(request.body.sound,'alarm');
      assert.doesNotMatch(JSON.stringify(request.body),/合成|PRIVATE|synthetic|changed|成功/);
    }
    assert.deepEqual(received[1].body,received[2].body,'Extension uses same frozen source and settings');
    await hook(directory,{...input,hook_event_name:'Stop'});await delay(300);assert.equal(received.length,3);
    for(const subdir of ['sessions','runs','jobs']) for(const name of await fs.readdir(path.join(directory,subdir))) {
      if(!name.endsWith('.json'))continue;
      assert.match(name,/^[a-f0-9]{64}\.json$/);assert.doesNotMatch(await fs.readFile(path.join(directory,subdir,name),'utf8'),/合成|PRIVATE|synthetic-key|127\.0\.0\.1/);
    }
    const bad=await processRun(['-NoProfile','-File',entry,'-Mode','Hook','-Agent','codex','-DataDirectory',directory],Buffer.from([0xff,0xfe]));
    assert.equal(bad.code,0);assert.equal(bad.err,'');assert.deepEqual(JSON.parse(bad.out),{continue:true});
    const traversal=await processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey','../private-secret']);
    assert.notEqual(traversal.code,0);assert.doesNotMatch(traversal.err,/private-secret|atn process/);
  } finally { await new Promise(resolve=>server.close(resolve));await fs.rm(directory,{recursive:true,force:true}); }
});

test('real worker reaches five-attempt cap and permanent rejection never retries', {timeout:180000}, async()=> {
  assert.equal(await fs.stat(entry).then(()=>true,()=>false),true,'Runtime entry point must exist');
  const directory=await fs.mkdtemp(path.join(os.tmpdir(),'atn-cap-'));
  const requests=[];let status=503;
  const server=http.createServer(async(req,res)=>{for await(const chunk of req){}requests.push(Date.now());res.writeHead(status,{'Content-Type':'application/json'});res.end('{"code":200}');});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  try {
    await script(`Import-Module ./src/Storage.psm1; Write-ATNJson ${quote(path.join(directory,'settings.json'))} @{minSeconds=1;longTaskSeconds=2;continuous=$false}; Set-ATNCredential ${quote(directory)} bark @{endpoint='http://127.0.0.1:${server.address().port}/synthetic-key'}`);
    const input={hook_event_name:'UserPromptSubmit',session_id:'synthetic-session',turn_id:'retry-cap'};
    await hook(directory,input);await delay(1100);const retryCapStartedAt=Date.now();await hook(directory,{...input,hook_event_name:'Stop'});
    const done=(await observedDurableWait(()=>jobs(directory),j=>j[0]?.status==='failed',130000,'retry-cap',()=>requests,retryCapStartedAt))[0];
    assert.equal(done.attempts,5);assert.equal(done.diagnostic,'http:503');assert.equal(requests.length,5);
    for(const [i,min] of [[1,4800],[2,14800],[3,29800],[4,59800]])assert.ok(requests[i]-requests[i-1]>=min);
    const again=await processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey',done.key]);assert.equal(again.code,1);assert.equal(requests.length,5);
    status=400;
    const permanentStartedAt=Date.now();
    await hook(directory,{...input,turn_id:'permanent'});await delay(1100);await hook(directory,{...input,turn_id:'permanent',hook_event_name:'Stop'});
    const all=await observedDurableWait(()=>jobs(directory),j=>j.length===2&&j.every(x=>x.status==='failed'),15000,'permanent',()=>requests,permanentStartedAt);
    assert.equal(all.find(j=>j.key!==done.key).attempts,1);assert.equal(requests.length,6);
  } finally { await new Promise(resolve=>server.close(resolve));await fs.rm(directory,{recursive:true,force:true}); }
});

test('failed Worker exits safely and explicit Preview is the only preview that sends', {timeout:30000}, async()=> {
  const directory=await fs.mkdtemp(path.join(os.tmpdir(),'atn-preview-'));
  let requests=0;
  const server=http.createServer(async(req,res)=>{for await(const chunk of req){}requests++;res.writeHead(200,{'Content-Type':'application/json'});res.end('{"code":200}');});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  try {
    await script(`Import-Module ./src/Runtime.psm1; Import-Module ./src/Storage.psm1; Write-ATNJson ${quote(path.join(directory,'settings.json'))} @{continuous=$false}; $j=New-ATNPreview codex ${quote(directory)}; $j.status='failed'; Write-ATNJson ${quote(path.join(directory,'jobs','a'.repeat(64)+'.json'))} $j; Set-ATNCredential ${quote(directory)} bark @{endpoint='http://127.0.0.1:${server.address().port}/synthetic-key'}`);
    const failed=await processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey','a'.repeat(64)]);
    assert.equal(failed.code,1,'Failed Worker must fail visibly, not silently succeed');assert.doesNotMatch(failed.err,/synthetic|atn-preview|127\.0\.0\.1/);assert.equal(requests,0);
    const real=await processRun(['-NoProfile','-File',entry,'-Mode','Preview','-Agent','codex','-DataDirectory',directory,'-SendRealPush','-RingSeconds','30']);
    assert.equal(real.code,0,real.err);assert.equal(real.err,'');assert.deepEqual(JSON.parse(real.out),{accepted:true});assert.equal(requests,1);
  } finally { await new Promise(resolve=>server.close(resolve));await fs.rm(directory,{recursive:true,force:true}); }
});
