const {test} = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const {spawn} = require('node:child_process');
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
async function eventually(read,predicate,timeout=15000) {
  const deadline=Date.now()+timeout;
  while(Date.now()<deadline) { const value=await read();if(predicate(value))return value;await delay(100); }
  throw new Error('Expected durable state was not reached');
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
    const ended=await hook(directory,{...input,hook_event_name:'Stop'});
    const pending=(await eventually(()=>jobs(directory),j=>j.length===1))[0];
    // Additional workers run while the first worker owns the durable job lock.
    const duplicates=await Promise.all([0,1].map(()=>processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey',pending.key])));
    for(const duplicate of duplicates)assert.equal(duplicate.code,0,duplicate.err);
    await fs.writeFile(path.join(directory,'settings.json'),JSON.stringify({provider:'ntfy',minSeconds:999,longTaskSeconds:2000,sound:'changed',continuous:false}));
    const done=(await eventually(()=>jobs(directory),j=>j[0]?.extensionStatus==='sent',25000))[0];
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
    await hook(directory,input);await delay(1100);await hook(directory,{...input,hook_event_name:'Stop'});
    const done=(await eventually(()=>jobs(directory),j=>j[0]?.status==='failed',130000))[0];
    assert.equal(done.attempts,5);assert.equal(done.diagnostic,'http:503');assert.equal(requests.length,5);
    for(const [i,min] of [[1,4800],[2,14800],[3,29800],[4,59800]])assert.ok(requests[i]-requests[i-1]>=min);
    const again=await processRun(['-NoProfile','-File',entry,'-Mode','Worker','-DataDirectory',directory,'-JobKey',done.key]);assert.equal(again.code,1);assert.equal(requests.length,5);
    status=400;
    await hook(directory,{...input,turn_id:'permanent'});await delay(1100);await hook(directory,{...input,turn_id:'permanent',hook_event_name:'Stop'});
    const all=await eventually(()=>jobs(directory),j=>j.length===2&&j.every(x=>x.status==='failed'));
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
