import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { mkdir, mkdtemp, copyFile, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { tmpdir } from 'node:os';

assert.ok(process.env.QA_SAVE_SERVER, 'Set QA_SAVE_SERVER to a locally built Go binary. This test restarts only that owned child process.');
const root=resolve(import.meta.dirname,'../..'),out=process.env.QA_OUTPUT_DIR||'test-results/saves';await mkdir(out,{recursive:true});
const directory=await mkdtemp(join(tmpdir(),'flytogether-save-browser-')),database=join(directory,'games.db');
const port=process.env.QA_SAVE_PORT||'62524',base=`http://127.0.0.1:${port}`;
let child,activeDatabase=database;
const wait=async(predicate,message,timeout=18000)=>{const end=Date.now()+timeout;while(Date.now()<end){if(await predicate())return;await new Promise(r=>setTimeout(r,120));}throw new Error(message);};
const start=async()=>{
 child=spawn(resolve(process.env.QA_SAVE_SERVER),[],{cwd:root,env:{...process.env,LISTEN_ADDR:`127.0.0.1:${port}`,PUBLIC_URL:base,WEB_DIR:join(root,'web/dist'),SAVE_PATH:activeDatabase,LINGXI_REALTIME_API_KEY:'',LINGXI_REALTIME_MODEL:''},stdio:['ignore','ignore','pipe']});
 child.stderr.on('data',()=>{});
 await wait(async()=>{if(child.exitCode!==null)throw Error('Owned save-test server exited');try{return (await fetch(base+'/readyz',{signal:AbortSignal.timeout(1500)})).ok;}catch{return false;}},'save server did not become ready');
};
const stop=async(signal='SIGTERM')=>{const p=child;if(!p||p.exitCode!==null||p.signalCode)return;await new Promise((resolve,reject)=>{const timer=setTimeout(()=>{p.kill('SIGKILL');reject(Error('Owned child did not stop'));},9000);p.once('exit',()=>{clearTimeout(timer);resolve();});p.kill(signal);});child=undefined;};
const restart=async(signal)=>{await stop(signal);await start();};
const python=(code,...args)=>{const r=spawnSync('python3',['-c',code,...args],{encoding:'utf8'});assert.equal(r.status,0,r.stderr);return r.stdout;};
const sql=query=>python('import sqlite3,sys\nc=sqlite3.connect(sys.argv[1],timeout=10)\nc.executescript(sys.argv[2])\nc.commit()\nc.close()',database,query);
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const errors=[],results=[];
const context=async()=>{const c=await browser.newContext({baseURL:base,viewport:{width:1360,height:1000}});await c.addInitScript(()=>{localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');window.__saveMic=0;navigator.mediaDevices.getUserMedia=async()=>{window.__saveMic++;throw Error('No microphone in save test');};});return c;};
const state=async c=>(await(await c.request.get('/api/session')).json()).room;
const post=async(c,path,data)=>{const r=await c.request.post(path,{data});const v=await r.json();assert.equal(r.status(),200,JSON.stringify(v));return v;};
const command=async(c,action,extra={})=>{const r=await state(c);return post(c,`/api/rooms/${r.id}/commands`,{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action,...extra});};
try{
 await start();const h=await context(),page=await h.newPage();page.on('pageerror',e=>errors.push(e.message));
 const created=await h.request.post('/api/rooms',{data:{names:['孩子','家长'],props:true,board:'cross',mode:'quick',waiting:false}});assert.equal(created.status(),201);let r=await created.json();
 await page.goto(base+'/play',{waitUntil:'networkidle'});
 await page.getByRole('button',{name:/道具口袋/}).click();await page.getByRole('button',{name:'使用起飞卡',exact:true}).click();
 r=await state(h);assert.equal(r.state.active_prop,'launch');const afterProp=r;
 await restart();await wait(async()=>await page.getByText('上次对局已恢复，骰子、道具和席位已保留',{exact:false}).isVisible(),'live browser rejected restarted instance');
 r=await state(h);assert.equal(r.id,afterProp.id);assert.equal(r.state.id,afterProp.state.id);assert.equal(r.status,'paused');assert.equal(r.restored,true);assert.equal(r.state.active_prop,'launch');assert.equal(r.state.players[0].props.launch,0);assert.notEqual(r.instance_id,afterProp.instance_id);
 await page.getByRole('button',{name:'返回首页',exact:true}).click();await page.getByRole('region',{name:'已保存的对局'}).waitFor();await page.screenshot({path:join(out,'saved-home.png')});
 await page.getByRole('button',{name:/继续当前游戏/}).click();await wait(async()=>(await state(h)).status==='playing','resume failed');
 r=await state(h);const rollBody={command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action:'roll'};
 r=await post(h,`/api/rooms/${r.id}/commands`,rollBody);assert.equal(r.state.phase,'move');await page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();const rolled=r;
 await restart('SIGKILL');await wait(async()=>await page.locator('.game-status-overlay').isVisible(),'hard restart not shown paused');
 const restored=await state(h);assert.equal(restored.status,'paused');assert.deepEqual(restored.state,rolled.state);assert.equal(restored.voice,'disconnected');assert.equal(restored.coach,undefined);
 await post(h,`/api/rooms/${r.id}/commands`,rollBody);assert.deepEqual((await state(h)).state,rolled.state,'retry changed saved dice');
 await page.screenshot({path:join(out,'restored-dice-phase.png')});
 await page.locator('.game-status-overlay').getByRole('button',{name:'继续游戏',exact:true}).click();await page.getByRole('button',{name:'小熊号',exact:true}).click();await page.getByRole('button',{name:'确认飞行',exact:true}).click();await wait(async()=>(await state(h)).state.players[0].pieces[0].progress===0,'restored legal move did not fly');
 results.push({case:'prop + real dice + hard restart',passed:true,dice:rolled.state.dice_rolls,retained_game_and_phase:true,same_command_not_reexecuted:true,live_browser_accepted_new_instance:true});
 // A real SQLite trigger aborts storage writes; there is no application test endpoint.
 await page.locator('.roll-button').waitFor({state:'visible'});await wait(async()=>!(await page.locator('.roll-button').isDisabled()),'roll did not unlock');
 const beforeFailure=await state(h);sql("CREATE TRIGGER reject_game_write BEFORE UPDATE ON rooms BEGIN SELECT RAISE(ABORT,'controlled save failure'); END;");
 const response=page.waitForResponse(res=>res.url().endsWith('/commands'));await page.locator('.roll-button').click();const failed=await response;assert.equal(failed.status(),503);const failedBody=failed.request().postDataJSON();
 await page.getByText('暂时无法保存，本次操作未生效，请稍后重试',{exact:true}).waitFor();assert.deepEqual((await state(h)).state,beforeFailure.state);assert.equal(await page.getByRole('dialog',{name:'3D 掷骰结果'}).count(),0);
 await page.screenshot({path:join(out,'save-failure.png')});sql('DROP TRIGGER reject_game_write;');
 await post(h,`/api/rooms/${beforeFailure.id}/commands`,failedBody);const recovered=await state(h);assert.equal(recovered.state.players[beforeFailure.state.current].stats.rolls,beforeFailure.state.players[beforeFailure.state.current].stats.rolls+1);
 results.push({case:'storage failure rollback',passed:true,no_uncommitted_popup:true,inventory_and_history_unchanged:true,retried_original_command_once:true});
 // Mixed-room identities and bot pause survive a real process restart.
 const fresh=await h.request.post('/api/rooms/new',{data:{names:['房主','待入座','机器人'],network:true,ai:true,difficulty:'hard',props:true,board:'cross',mode:'quick'}});assert.equal(fresh.status(),201);r=await fresh.json();
 const guest=await context();await post(guest,`/api/rooms/${r.id}/join`,{invite:r.network.invite,name:'朋友'});const gp=await guest.newPage();gp.on('pageerror',e=>errors.push(e.message));await page.goto(base+'/play',{waitUntil:'networkidle'});await gp.goto(base+'/play',{waitUntil:'networkidle'});
 await gp.getByRole('button',{name:'我准备好了',exact:true}).click();await page.getByRole('button',{name:'开始游戏',exact:true}).click();
 for(let i=0;i<100;i++){r=await state(h);if(r.state.current===2)break;const actor=r.state.current===0?h:guest;if(r.state.phase==='roll')await command(actor,'roll');else await command(actor,'move',{piece_id:r.state.legal_moves[0].piece_id});}
 assert.equal(r.state.current,2);const netID=r.id,gameID=r.state.id,invite=r.network.invite;
 await stop('SIGKILL');const durable=JSON.parse(python('import sqlite3,sys,json\nc=sqlite3.connect(sys.argv[1])\nr=c.execute("SELECT payload FROM rooms WHERE id=?",(sys.argv[2],)).fetchone()\nprint(r[0].decode() if isinstance(r[0],bytes) else r[0])',database,netID));
 await start();await wait(async()=>await page.locator('.game-status-overlay').isVisible(),'network restart not visible');r=await state(h);assert.equal(r.status,'paused');assert.equal(r.state.id,gameID);assert.deepEqual(r.state,durable.state);assert.equal(r.state.current,2);assert.equal((await state(guest)).me.player_id,'player-1');assert.equal(r.network.invite,invite);assert.equal(r.network.seats[2].robot,true);
 const frozen=r.state.version;await new Promise(r=>setTimeout(r,2800));assert.equal((await state(h)).state.version,frozen,'bot acted before host resumed');
 await page.screenshot({path:join(out,'restored-mixed-room.png')});await page.locator('.game-status-overlay').getByRole('button',{name:'继续游戏',exact:true}).click();await wait(async()=>(await state(h)).state.version>frozen,'bot never resumed');
 const forbidden=await guest.request.post(`/api/rooms/${netID}/discard`,{data:{}});assert.equal(forbidden.status(),403);
 results.push({case:'mixed seats and bot restart',passed:true,original_cookie_seats_retained:true,invite_preserved:true,bot_frozen_until_resume:true,guest_cannot_delete:true});
 // The ordinary Python backup API includes committed WAL data; restore into a separate test DB.
 const backup=join(directory,'backup.db');const saved=spawnSync('python3',[join(root,'scripts/save_backup.py'),'--database',database,'--output',backup],{encoding:'utf8'});assert.equal(saved.status,0,saved.stderr);
 await stop();activeDatabase=join(directory,'restored-copy.db');await copyFile(backup,activeDatabase);await start();assert.equal((await state(h)).state.id,gameID);assert.equal((await state(guest)).me.player_id,'player-1');
 await page.reload({waitUntil:'networkidle'});await page.getByRole('button',{name:'返回首页',exact:true}).click();await page.getByRole('button',{name:'删除这份存档',exact:true}).click();await page.getByRole('button',{name:'确认删除存档',exact:true}).click();await wait(async()=>{const x=await state(h);return !!x&&x.id!==netID;},'discard did not return to a fresh home');
 await restart();assert.notEqual((await state(h))?.id,netID);assert.equal((await state(guest)),null);
 results.push({case:'live WAL backup + restore + discard',passed:true,backup_integrity_verified:true,deleted_room_not_resurrected:true});
 for(const p of [page,gp])assert.equal(await p.evaluate(()=>window.__saveMic),0);assert.deepEqual(errors,[]);
 await writeFile(join(out,'results.json'),JSON.stringify({results,console_errors:errors,microphone_requests:0,process_restarts_are_real:true,storage_fault:'SQLite abort trigger only'},null,2));console.log(JSON.stringify({passed:true,results,console_errors:errors}));
}finally{await browser.close();await stop();}
