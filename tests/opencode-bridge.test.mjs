import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { EventEmitter } from 'node:events';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

function completedChild(onInput) {
  const child=new EventEmitter();child.stdin=new EventEmitter();
  child.stdin.end=input=>{onInput(input);queueMicrotask(()=>child.emit('close',0));};
  return child;
}
test('native bridge filters, deduplicates, and sends only IDs through hidden stdin', async () => {
  assert.ok(existsSync(new URL('../integrations/opencode/bridge.mjs', import.meta.url)), 'bridge exists');
  const { createAgentTaskNotify } = await import('../integrations/opencode/bridge.mjs');
  const host = await import('../integrations/opencode/agent-task-notify.mjs');
  assert.equal(Object.values(host).length, 1, 'loader receives only one plugin');
  assert.equal(typeof Object.values(host)[0], 'function');
  const calls=[];
  const plugin=await createAgentTaskNotify({hookPath:'C:/package/scripts/agent-task-notify.ps1', spawn(exe,args,options) {
    const call={exe,args,options}; calls.push(call);
    return completedChild(input=>call.input=JSON.parse(input));
  }})({client:{session:{async get({path}) {return {data:{id:path.id,parentID:path.id==='child'?'parent':undefined}}}}}});
  const event=async(type,properties)=>plugin.event({event:{type,properties}});
  await event('session.created',{info:{id:'root'}});
  await event('message.updated',{info:{id:'a',sessionID:'root',role:'assistant',text:'PRIVATE'}});
  await event('message.updated',{info:{id:'c',sessionID:'child',role:'user',text:'PRIVATE'}});
  assert.equal(calls.length,0);
  await event('message.updated',{info:{id:'u',sessionID:'root',role:'user',text:'PRIVATE'}});
  await event('message.updated',{info:{id:'u',sessionID:'root',role:'user'}});
  await event('message.updated',{info:{id:'extra',sessionID:'root',role:'user'}});
  await event('session.idle',{sessionID:'root'});
  await event('session.idle',{sessionID:'root'});
  assert.deepEqual(calls.map(c=>c.input),[
    {schemaVersion:1,event:'started',sessionId:'root',runId:'u'},
    {schemaVersion:1,event:'stopped',sessionId:'root',runId:'u'}
  ]);
  for(const call of calls) { assert.equal(call.exe,'pwsh'); assert.equal(call.options.windowsHide,true); assert.equal(call.options.shell,false); assert.ok(!JSON.stringify(call).includes('PRIVATE')); assert.ok(call.args.includes('-File')); }
  await event('message.updated',{info:{id:'v',sessionID:'root',role:'user'}});
  await event('session.error',{sessionID:'root',error:{message:'PRIVATE'}});
  assert.equal(calls.at(-1).input.event,'failed');
});
test('idle waits for pending parent lookup', async()=>{
  const {createAgentTaskNotify}=await import('../integrations/opencode/bridge.mjs');
  let release; const waiting=new Promise(r=>release=r); const sent=[];
  const plugin=await createAgentTaskNotify({spawn(){return completedChild(x=>sent.push(JSON.parse(x)))}})({client:{session:{async get(){await waiting;return {data:{id:'root'}}}}}});
  const start=plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user'}}}});
  const stop=plugin.event({event:{type:'session.idle',properties:{sessionID:'root'}}});
  release(); await Promise.all([start,stop]);
  assert.deepEqual(sent.map(x=>x.event),['started','stopped']);
});

test('Stop cannot overtake a delayed Start subprocess completion', async()=>{
  const {createAgentTaskNotify}=await import('../integrations/opencode/bridge.mjs');
  const directory=await mkdtemp(join(tmpdir(),'atn-bridge-'));const file=join(directory,'events.jsonl');const completions=[];
  try {
    const plugin=await createAgentTaskNotify({dataDirectory:directory,spawn(){
      const child=spawn(process.execPath,['-e',`let s='';process.stdin.on('data',x=>s+=x);process.stdin.on('end',()=>{let e=JSON.parse(s);setTimeout(()=>require('fs').appendFileSync(process.argv[1],e.event+'\\n'),e.event==='started'?250:0)});`,file],{stdio:['pipe','ignore','ignore'],windowsHide:true});
      completions.push(new Promise(resolve=>child.on('close',resolve)));return child;
    }})({client:{session:{async get(){return {data:{id:'root'}}}}}});
    await Promise.all([
      plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user'}}}}),
      plugin.event({event:{type:'session.idle',properties:{sessionID:'root'}}})
    ]);
    await Promise.all(completions);
    assert.deepEqual((await readFile(file,'utf8')).trim().split('\n'),['started','stopped']);
  } finally {await rm(directory,{recursive:true,force:true});}
});

test('bridge bounds stalled children/metadata and redacts all error classes', async()=>{
  const {createAgentTaskNotify}=await import('../integrations/opencode/bridge.mjs');
  for (const mode of ['spawn','stdin','exit','timeout','metadata','metadata-timeout','metadata-shape']) {
    const codes=[];let killed=false;let spawned=0;
    const plugin=await createAgentTaskNotify({timeoutMs:30,diagnostic:code=>codes.push(code),spawn(){
      spawned++;if(mode==='spawn')throw Error('PRIVATE https://secret.invalid');
      const child=new EventEmitter();child.stdin=new EventEmitter();child.kill=()=>{killed=true};
      child.stdin.end=()=>{if(mode==='stdin')queueMicrotask(()=>child.stdin.emit('error',Error('PRIVATE')));else if(mode==='exit')queueMicrotask(()=>child.emit('close',1));};
      return child;
    }})({client:{session:{async get(){if(mode==='metadata')throw Error('PRIVATE');if(mode==='metadata-timeout')return new Promise(()=>{});if(mode==='metadata-shape')return {};return {data:{id:'root'}};}}}});
    await plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user'}}}});
    assert.deepEqual(codes,[`bridge-${mode==='metadata-shape'?'metadata':mode}`]);if(mode==='timeout')assert.equal(killed,true);
    const before=spawned;await plugin.event({event:{type:'session.idle',properties:{sessionID:'root'}}});assert.equal(spawned,before,'Failed Start must not lead to a late/out-of-order Stop');
  }
});

test('bridge environment data default yields to explicit option', async()=>{
  const {createAgentTaskNotify}=await import('../integrations/opencode/bridge.mjs');
  const directory=await mkdtemp(join(tmpdir(),'atn-bridge-data-'));const previous=process.env.ATN_DATA_DIRECTORY;
  try {
    process.env.ATN_DATA_DIRECTORY=directory;
    for(const explicit of [undefined,join(directory,'explicit')]) {
      let actual;
      const plugin=await createAgentTaskNotify({dataDirectory:explicit,spawn(exe,args){actual=args[args.indexOf('-DataDirectory')+1];return completedChild(()=>{})}})({client:{session:{async get(){return {data:{id:'root'}}}}}});
      await plugin.event({event:{type:'message.updated',properties:{info:{sessionID:'root',id:'u',role:'user'}}}});
      assert.equal(actual,explicit??directory);
    }
  } finally {if(previous===undefined)delete process.env.ATN_DATA_DIRECTORY;else process.env.ATN_DATA_DIRECTORY=previous;await rm(directory,{recursive:true,force:true});}
});
