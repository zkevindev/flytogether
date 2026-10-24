import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL with working Lingxi credentials on the server. This test calls real output-only TTS.');
const out=process.env.QA_OUTPUT_DIR||'test-results/companion-tts';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
try {
 const c=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width:1360,height:1000}});await c.addInitScript(()=>{localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-companion',JSON.stringify({style:'coach',frequency:'normal'}));window.__tts={mic:0,starts:0};navigator.mediaDevices.getUserMedia=async()=>{window.__tts.mic++;throw Error('Output-only test must not capture microphone')};const start=AudioBufferSourceNode.prototype.start;AudioBufferSourceNode.prototype.start=function(...a){window.__tts.starts++;return start.apply(this,a);};});
 const p=await c.newPage(),errors=[];p.setDefaultTimeout(25000);p.on('pageerror',e=>errors.push(e.message));
 const created=await c.request.post('/api/rooms',{data:{names:['孩子','家长'],preset:'gentle',board:'cross',mode:'quick',props:true,waiting:false}});assert.equal(created.status(),201);const room=await created.json();
 await p.goto('/play',{waitUntil:'networkidle'});const response=p.waitForResponse(r=>r.url().endsWith('/announcement')&&r.request().postDataJSON()?.companion?.style==='coach');
 await p.getByRole('checkbox',{name:'关闭麦克风时也播报回合',exact:true}).check();const raw=await response;assert.equal(raw.status(),200,await raw.text());const coach=await raw.json();assert.ok(coach.text.includes('五点或六点'));assert.ok(Buffer.from(coach.audio,'base64').length>2400);assert.equal(coach.sample_rate,24000);
 const unlock=p.getByRole('button',{name:'点击启用播报声音',exact:true});await p.waitForFunction(()=>window.__tts.starts>0||document.body.innerText.includes('点击启用播报声音'));if(await unlock.isVisible())await unlock.click();await p.waitForFunction(()=>window.__tts.starts>0);
 await p.getByRole('button',{name:'总静音',exact:true}).click();
 const quiet=await c.request.post(`/api/rooms/${room.id}/announcement`,{data:{game_id:room.state.id,key:room.state.id+':1',companion:{style:'coach',frequency:'quiet'}}});assert.equal(quiet.status(),204);
 const short=await c.request.post(`/api/rooms/${room.id}/announcement`,{data:{game_id:room.state.id,key:room.state.id+':1',companion:{style:'concise',frequency:'normal'}}});assert.equal(short.status(),200,await short.text());const concise=await short.json();assert.equal(concise.text,'轮到孩子掷骰子。');assert.ok(Buffer.from(concise.audio,'base64').length>2400);
 const unchanged=(await(await c.request.get('/api/session')).json()).room.state;assert.deepEqual(unchanged,room.state);assert.equal(await p.evaluate(()=>window.__tts.mic),0);assert.deepEqual(errors,[]);
 await c.request.post(`/api/rooms/${room.id}/discard`,{data:{}});
 const report={passed:true,real_lingxi_output_only:true,coach:{text:coach.text,pcm_bytes:Buffer.from(coach.audio,'base64').length},concise:{text:concise.text,pcm_bytes:Buffer.from(concise.audio,'base64').length},quiet_status:204,real_browser_pcm_played:true,game_unchanged:true,microphone_requests:0,console_errors:errors};await writeFile(out+'/results.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));
}finally{await browser.close();}
