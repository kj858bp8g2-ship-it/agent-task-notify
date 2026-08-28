import { spawn as nodeSpawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { isAbsolute } from 'node:path';

export function createAgentTaskNotify(options = {}) {
  const spawn = options.spawn ?? nodeSpawn;
  const hookPath = options.hookPath ?? fileURLToPath(new URL('../../scripts/agent-task-notify.ps1', import.meta.url));
  return async (context) => {
    const { client, agentTaskNotifyDataDirectory } = context;
    const native = Object.hasOwn(options, 'executable') || Object.hasOwn(context, 'agentTaskNotifyExecutable');
    const executable = Object.hasOwn(options, 'executable') ? options.executable : context.agentTaskNotifyExecutable;
    const active = new Map(), seen = new Set(), queues = new Map(), depths = new Map(), reported = new Set();
    const timeoutMs = Math.min(10000, Math.max(10, options.timeoutMs ?? 5000));
    // One fixed code per failure class per plugin instance, never host error text.
    const diagnose = code => {if (!reported.has(code)) {reported.add(code);try {(options.diagnostic ?? console.error)(code);} catch {}}};
    const metadata = id => new Promise(resolve => {
      let done=false;
      const finish=value=>{if(done)return;done=true;clearTimeout(timer);resolve(value)};
      const timer=setTimeout(()=>{diagnose('bridge-metadata-timeout');finish(null)},timeoutMs);
      Promise.resolve().then(()=>client.session.get({path:{id}})).then(value=>{if(!done)finish(value)},()=>{if(!done){diagnose('bridge-metadata');finish(null)}});
    });
    const send = (event, sessionId, runId) => new Promise(resolve => {
      if (native && (typeof executable !== 'string' || !isAbsolute(executable) || /[\u0000-\u001f\u007f]/u.test(executable))) {
        diagnose('bridge-native-unavailable');resolve(false);return;
      }
      const args = native ? ['hook','--agent','opencode'] : ['-NoLogo','-NoProfile','-NonInteractive','-File',hookPath,'-Mode','Hook','-Agent','opencode'];
      const dataDirectory = options.dataDirectory ?? agentTaskNotifyDataDirectory ?? process.env.ATN_DATA_DIRECTORY;
      if (dataDirectory) args.push(native ? '--data-directory' : '-DataDirectory',dataDirectory);
      let child, done=false, timer;
      const finish=code=>{
        if(done)return;done=true;clearTimeout(timer);
        if(code){diagnose(code);try{child?.kill()}catch{}}
        resolve(!code);
      };
      try {
        child = spawn(native ? executable : 'pwsh', args, { windowsHide:true, shell:false, stdio:['pipe','ignore','ignore'] });
        child.on('error',()=>finish('bridge-spawn'));child.on('close',code=>finish(code===0?null:'bridge-exit'));
        child.stdin.on('error',()=>finish('bridge-stdin'));
        timer=setTimeout(()=>finish('bridge-timeout'),timeoutMs);
        try{child.stdin.end(JSON.stringify({schemaVersion:1,event,sessionId,runId})+'\n')}catch{finish('bridge-stdin')}
      } catch {finish('bridge-spawn')}
    });
    return { event: async ({event}) => {
      const properties=event?.properties;
      const info=properties?.info;
      const start=event?.type==='message.updated' && info?.role==='user';
      const stop=event?.type==='session.idle' || event?.type==='session.error';
      if (!start && !stop) return;
      const id=start ? info.sessionID : properties?.sessionID;
      if (typeof id!=='string' || !id || (start && (typeof info.id!=='string' || !info.id))) return;
      if ((depths.get(id) ?? 0)>=32 || (queues.size>=256 && !queues.has(id))) {diagnose('bridge-queue-full');return}
      depths.set(id,(depths.get(id) ?? 0)+1);
      const next=(queues.get(id) ?? Promise.resolve()).then(async()=>{
        if (start) {
          const key=JSON.stringify([id,info.id]);
          if(seen.has(key)) return;
          const response=await metadata(id);
          if (!response) return;
          if (!response.data || typeof response.data!=='object') {diagnose('bridge-metadata');return}
          if (response.data.parentID) return;
          seen.add(key);
          if(seen.size>2048)seen.delete(seen.values().next().value);
          if(active.has(id)) return;
          if(active.size>=256){diagnose('bridge-queue-full');return}
          if(await send('started',id,info.id))active.set(id,info.id);
        } else if(active.has(id)) {
          const run=active.get(id); active.delete(id);
          await send(event.type==='session.error'?'failed':'stopped',id,run);
        }
      }).catch(()=>{diagnose('bridge-event')});
      queues.set(id,next);
      await next;
      const remaining=depths.get(id)-1;if(remaining)depths.set(id,remaining);else depths.delete(id);
      if(queues.get(id)===next) queues.delete(id);
    }};
  };
}
