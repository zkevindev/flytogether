import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdir,writeFile} from 'node:fs/promises';
assert.ok(process.env.QA_BASE_URL&&process.env.QA_RESTART_PYTHON&&process.env.QA_RESTART_HELPER,'Set URL and an authorized restart helper outside Git. This test restarts the deployed service.');
const out=process.env.QA_OUTPUT_DIR||'test-results/saves-deployed';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const wait=async fn=>{for(let i=0;i<150;i++){if(await fn())return;await new Promise(r=>setTimeout(r,150));}throw Error('state did not recover');};
try{
 const context=async()=>{const c=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width:1360,height:1000}});await c.addInitScript(()=>{localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');window.__mic=0;navigator.mediaDevices.getUserMedia=async()=>{window.__mic++;throw Error('No microphone in save test');};});return c;};
 const h=await context(),g=await context(),page=await h.newPage(),gp=await g.newPage(),errors=[];for(const p of[page,gp])p.on('pageerror',e=>errors.push(e.message));
 const state=async c=>(await(await c.request.get('/api/session')).json()).room;
 const post=async(c,path,data)=>{const r=await c.request.post(path,{data});assert.ok(r.ok(),`${r.status()} ${await r.text()}`);return r.json();};
 let r=await post(h,'/api/rooms/new',{names:['房主','朋友','机器人'],preset:process.env.QA_PRESET||'custom',network:true,ai:true,difficulty:'hard',props:true,board:'cross',mode:'quick'});
 await post(g,`/api/rooms/${r.id}/join`,{invite:r.network.invite,name:'朋友'});
 await page.goto('/play',{waitUntil:'networkidle'});await gp.goto('/play',{waitUntil:'networkidle'});await gp.getByRole('button',{name:'我准备好了',exact:true}).click();await page.getByRole('button',{name:'开始游戏',exact:true}).click();
 const cmd=async(action,value)=>{r=await state(h);return post(h,`/api/rooms/${r.id}/commands`,{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action,prop_id:value});};
 await cmd('prop','launch');r=await state(h);const body={command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action:'roll'};
 r=await post(h,`/api/rooms/${r.id}/commands`,body);assert.equal(r.state.phase,'move');const expected=r;
 execFileSync(process.env.QA_RESTART_PYTHON,[process.env.QA_RESTART_HELPER],{stdio:'pipe',timeout:40000});
 await wait(async()=>await page.locator('.game-status-overlay').isVisible());await wait(async()=>await gp.locator('.game-status-overlay').isVisible());
 r=await state(h);assert.equal(r.id,expected.id);assert.equal(r.status,'paused');assert.equal(r.state.preset,process.env.QA_PRESET||'custom');assert.deepEqual(r.state,expected.state);assert.equal(r.network.invite,expected.network.invite);assert.equal((await state(g)).me.player_id,'player-1');assert.notEqual(r.instance_id,expected.instance_id);
 await post(h,`/api/rooms/${r.id}/commands`,body);assert.deepEqual((await state(h)).state,expected.state);
 await page.getByRole('button',{name:'返回首页',exact:true}).click();await page.getByRole('region',{name:'已保存的对局'}).waitFor();await page.screenshot({path:out+'/saved-home.png'});
 await page.getByRole('button',{name:/继续当前游戏/}).click();await page.getByRole('button',{name:'小熊号',exact:true}).click();await page.getByRole('button',{name:'确认飞行',exact:true}).click();await wait(async()=>(await state(h)).state.players[0].pieces[0].progress===0);
 // A real output-only Lingxi request; SFX and microphones remain off.
 const solo=await context();let t=await post(solo,'/api/rooms',{names:['小朋友','家长'],props:true,mode:'quick',board:'cross',waiting:false});
 const speech=await solo.request.post(`/api/rooms/${t.id}/announcement`,{data:{game_id:t.state.id,key:t.state.id+':'+t.state.turn}});assert.equal(speech.status(),200,await speech.text());const audio=await speech.json();assert.equal(audio.text,'轮到小朋友掷骰子啦！');assert.ok(Buffer.from(audio.audio,'base64').length>2400);assert.equal(audio.sample_rate,24000);
 await post(solo,`/api/rooms/${t.id}/discard`,{});
 await post(h,`/api/rooms/${r.id}/discard`,{});for(const p of[page,gp])assert.equal(await p.evaluate(()=>window.__mic),0);assert.deepEqual(errors,[]);
 const report={passed:true,rule_preset:process.env.QA_PRESET||'custom',public_service_restart:true,same_live_browser_recovered:true,original_host_guest_seats:true,saved_dice_props_and_legal_moves:true,duplicate_command_not_reexecuted:true,resumed_plane_took_off:true,lingxi_output_only_tts:{real_upstream:true,sample_rate:audio.sample_rate,pcm_bytes:Buffer.from(audio.audio,'base64').length},microphone_requests:0,console_errors:errors};await writeFile(out+'/results.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));
}finally{await browser.close();}
