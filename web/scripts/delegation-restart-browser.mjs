import {chromium} from 'playwright';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {resolve,join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {mkdir,writeFile} from 'node:fs/promises';
import assert from 'node:assert/strict';
assert.ok(process.env.QA_SERVER_BIN,'Set QA_SERVER_BIN to the built Go server');
const root=resolve(fileURLToPath(new URL('../..',import.meta.url))),out=resolve(process.env.QA_OUTPUT_DIR||'test-results/delegation-restart');await mkdir(out,{recursive:true});
const port=process.env.QA_PORT||'62551',base='http://127.0.0.1:'+port;let server;
const start=async()=>{if(await fetch(base+'/healthz').then(r=>r.ok).catch(()=>false))throw Error('QA port already occupied');server=spawn(resolve(process.env.QA_SERVER_BIN),[],{cwd:root,env:{...process.env,LISTEN_ADDR:'127.0.0.1:'+port,SAVE_PATH:join(out,'games.db'),WEB_DIR:join(root,'web/dist'),ROOM_DELEGATION_SECONDS:'5'},stdio:'ignore'});for(let n=0;n<100;n++){if(server.exitCode!==null)throw Error('QA server failed to start');if(await fetch(base+'/healthz').then(r=>r.ok).catch(()=>false))return;await new Promise(r=>setTimeout(r,100));}throw Error('QA server not ready');};
const stop=async()=>{if(server&&server.exitCode===null){server.kill('SIGTERM');await once(server,'exit');}};
const browser=await chromium.launch({channel:'chrome',headless:true});
try{
 await start();const contexts=[];for(let i=0;i<2;i++){const c=await browser.newContext({baseURL:base});await c.addInitScript(()=>{localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-experience',JSON.stringify({board:'flat',motion:'reduced'}));});contexts.push(c);}
 const [h,g]=contexts;let r=await(await h.request.post('/api/rooms',{data:{network:true,names:['重启房主','家人'],props:true,mode:'classic',board:'cross'}})).json();assert.equal((await g.request.post(`/api/rooms/${r.id}/join`,{data:{invite:r.network.invite,name:'家人'}})).status(),200);
 const pages=await Promise.all(contexts.map(c=>c.newPage()));for(const p of pages)await p.goto('/play',{waitUntil:'networkidle'});
 const state=async()=>(await(await h.request.get('/api/session')).json()).room;
 const cmd=async(c,action)=>{r=await state();const res=await c.request.post(`/api/rooms/${r.id}/commands`,{data:{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action}});assert.equal(res.status(),200);};
 await pages[1].getByRole('button',{name:'我准备好了',exact:true}).click();await cmd(h,'start');await cmd(h,'pause');await cmd(h,'auto_delegate_on');await cmd(h,'delegate');await cmd(g,'delegate');r=await state();const saved=r;
 await stop();await start();await pages[0].reload({waitUntil:'networkidle'});await pages[1].reload({waitUntil:'networkidle'});r=await state();assert.equal(r.status,'paused');assert.equal(r.restored,true);assert.notEqual(r.instance_id,saved.instance_id);assert.deepEqual(r.state,saved.state);assert.equal(r.network.invite,saved.network.invite);assert.equal(r.me.player_id,saved.me.player_id);assert.equal(r.network.seats[0].auto_delegate,true);assert.ok(r.network.seats.every(s=>s.delegated));
 await new Promise(r=>setTimeout(r,2600));assert.deepEqual((await state()).state,saved.state);await pages[0].screenshot({path:join(out,'restored-delegation.png'),fullPage:true});
 await cmd(h,'reclaim');await cmd(h,'resume');assert.equal((await state()).network.seats[0].delegated,undefined);assert.equal((await state()).state.players[0].stats.rolls,0);
 const result={passed:true,actual_process_restart:true,paused_until_explicit_resume:true,persisted:['same seats and invite','exact dice/board/inventory/stats','delegation control and offline consent'],no_secret_actions_after_restart:true};await writeFile(join(out,'results.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
}finally{await browser.close();await stop();}
