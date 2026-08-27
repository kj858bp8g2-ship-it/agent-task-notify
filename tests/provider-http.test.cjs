const {test} = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const {spawn} = require('node:child_process');
const path = require('node:path');

test('real provider transport enforces JSON, authentication, status and redirect boundaries', async () => {
  let reply = {}, received = [];
  const server = http.createServer(async (req, res) => {
    const chunks=[]; for await (const chunk of req) chunks.push(chunk);
    received.push({url:req.url, headers:req.headers, bytes:Buffer.concat(chunks)});
    if (reply.disconnect) { req.socket.destroy(); return; }
    res.writeHead(reply.status || 200, {'Content-Type':'application/json', ...(reply.headers || {})});
    res.end(reply.body);
  });
  await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
  const port=server.address().port;
  async function send(provider, response) {
    reply=response; received=[];
    const script=`$ErrorActionPreference='Stop'; Import-Module './src/Providers.psm1'; $s=@{provider='${provider}';continuous=$false}; $c=@{endpoint='http://127.0.0.1:${port}/${provider === 'bark' ? 'base/synthetic-key' : 'synthetic-topic'}';token='synthetic-token';allowUnauthenticated=$false}; $p=@{title='中文测试';message='通用通知';body='通用通知';call=1;sound='alarm';level='critical';volume=7;priority=4;topic='wrong-topic'}; try { Send-ATNPush $s $c $p | ConvertTo-Json -Compress } catch { @{retryable=$_.Exception.Data['retryable'];diagnostic=$_.Exception.Data['diagnostic'];message=$_.Exception.Message} | ConvertTo-Json -Compress }`;
    const child=spawn('pwsh',['-NoProfile','-Command','-'],{cwd:path.join(__dirname,'..'),stdio:['pipe','pipe','pipe'],windowsHide:true});
    let out='',err='';child.stdout.on('data',x=>out+=x);child.stderr.on('data',x=>err+=x);
    // An ASCII script avoids the host shell's stdin code page; payload remains Unicode.
    const asciiScript=script.replace(/'([^']*[^\x00-\x7f][^']*)'/g, (_,s)=>'"'+Array.from(s,c=>c.charCodeAt(0)>127?'`u{'+c.charCodeAt(0).toString(16)+'}':c).join('')+'"');
    child.stdin.end(asciiScript+'\n');
    const exit=await new Promise(resolve=>child.on('close',resolve));
    assert.equal(exit,0,err);assert.equal(err,'');
    const result=JSON.parse(out.replace(/\x1b\[[0-9;?]*[a-zA-Z]/g,'').trim());
    assert.doesNotMatch(JSON.stringify(result),/synthetic-token|synthetic-key|127\.0\.0\.1/);
    return result;
  }
  try {
    assert.deepEqual(await send('ntfy',{body:'{"id":"test-id","event":"message","topic":"synthetic-topic"}'}),{accepted:true});
    assert.equal(received.length,1);assert.equal(received[0].url,'/');
    assert.equal(received[0].headers.authorization,'Bearer synthetic-token');
    const body=JSON.parse(received[0].bytes.toString('utf8'));
    assert.equal(body.title,'中文测试');assert.equal(body.topic,'synthetic-topic');
    for (const key of ['call','sound','level','volume','body']) assert.equal(key in body,false);
    assert.deepEqual(await send('bark',{body:'{"code":200}'}),{accepted:true});
    assert.equal(received[0].url,'/base/synthetic-key');assert.equal(received[0].headers.authorization,undefined);
    assert.equal('call' in JSON.parse(received[0].bytes),false);
    for (const provider of ['bark','ntfy']) {
      for (const [status,retryable] of [[400,false],[408,true],[425,true],[429,true],[503,true],[302,false]]) {
        const result=await send(provider,{status,body:'secret-response',headers:{Location:`http://127.0.0.1:${port}/redirect`}});
        assert.equal(result.retryable,retryable);assert.equal(result.diagnostic,`http:${status}`);assert.equal(received.length,1);
      }
      for (const body of ['not-json','{}','[]','{"id":"partial"}']) {
        const result=await send(provider,{body});assert.equal(result.retryable,true);assert.equal(result.diagnostic,'malformed-response',`${provider}: ${body}`);
      }
    }
    assert.equal((await send('bark',{body:'{"code":400}'})).retryable,false);
    assert.equal((await send('bark',{body:'{"code":503}'})).retryable,true);
    assert.equal((await send('ntfy',{body:'{"id":"test","event":"message","topic":"other"}'})).retryable,true);
    assert.equal((await send('ntfy',{body:'{"id":"test","event":["message"],"topic":["synthetic-topic"]}'})).retryable,true);
    const disconnected=await send('ntfy',{disconnect:true});
    assert.equal(disconnected.retryable,true);assert.equal(disconnected.diagnostic,'transport');
  } finally { await new Promise(resolve=>server.close(resolve)); }
});
