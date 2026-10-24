import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {resolve,join} from 'node:path';
import {mkdir,writeFile} from 'node:fs/promises';
assert.ok(process.env.QA_BASE_URL&&process.env.QA_AUDIO_FILE,'Set QA_BASE_URL and QA_AUDIO_FILE to the lucky fixture');
const base=process.env.QA_BASE_URL,out=process.env.QA_OUTPUT_DIR||'test-results/network-voice';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream','--use-file-for-fake-audio-capture='+resolve(process.env.QA_AUDIO_FILE),...(process.platform==='darwin'?['--use-angle=metal']:[])]});
try{
 const h=await browser.newContext({baseURL:base,permissions:['microphone']}),g=await browser.newContext({baseURL:base});
 for(const [c,enabled]of [[h,true],[g,false]])await c.addInitScript(enabled=>{localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-auto-voice',enabled?'on':'off');window.__voice=[];const WS=WebSocket;window.WebSocket=class extends WS{constructor(url,...args){super(url,...args);if(String(url).includes('/ws/voice?'))this.addEventListener('message',e=>{if(typeof e.data==='string'){const data=JSON.parse(e.data);if(data.type==='stt'||data.type==='hello'||data.type==='error')window.__voice.push({type:data.type,text:data.text});}});}};},enabled);
 const created=await h.request.post('/api/rooms',{data:{names:['房主','空位'],network:true,board:'cross',mode:'quick',props:true}});assert.equal(created.status(),201);let r=await created.json();
 const joined=await g.request.post(`/api/rooms/${r.id}/join`,{data:{invite:r.network.invite,name:'朋友'}});assert.equal(joined.status(),200);
 const hp=await h.newPage(),gp=await g.newPage();await hp.goto(new URL('/play',base).href,{waitUntil:'networkidle'});await gp.goto(new URL('/play',base).href,{waitUntil:'networkidle'});
 await gp.getByRole('button',{name:'我准备好了',exact:true}).click();await hp.getByRole('button',{name:'开始游戏',exact:true}).click();
 await hp.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({timeout:20000});
 const denied=await g.request.get(`/ws/voice?room_id=${r.id}`);assert.equal(denied.status(),403);assert.equal((await denied.json()).code,'NOT_YOUR_TURN');
 const until=Date.now()+30000;while(Date.now()<until){r=(await(await h.request.get('/api/session')).json()).room;if(r.state.players[0].props.luck===0)break;await new Promise(resolve=>setTimeout(resolve,150));}
 assert.equal(r.state.players[0].props.luck,0);assert.equal(r.state.players[0].stats.rolls,1);assert.equal(r.state.dice_rolls.length,2);assert.equal(r.state.dice,Math.max(...r.state.dice_rolls));
 await gp.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();const guest=(await(await g.request.get('/api/session')).json()).room;assert.equal(guest.state.id,r.state.id);assert.equal(guest.state.version,r.state.version);assert.equal(guest.me.player_id,'player-1');
 const events=await hp.evaluate(()=>window.__voice);assert.ok(events.some(e=>e.type==='stt'&&/幸运/.test(e.text)));assert.equal(events.filter(e=>e.type==='error').length,0);
 await hp.screenshot({path:join(out,'network-voice-dice.png')});const result={passed:true,scope:'Synthetic WAV through real Chrome microphone, Go membership-bound voice and actual Lingxi provider; no physical multi-user microphone test',dice:r.state.dice_rolls,host_rolls:r.state.players[0].stats.rolls,guest_cannot_open_voice_on_host_turn:true,both_clients_share_actual_result:true,events};await writeFile(join(out,'results.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
}finally{await browser.close();}
