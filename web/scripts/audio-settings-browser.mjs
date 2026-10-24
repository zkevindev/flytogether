import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL to a test server with voice enabled; upstream voice is controlled in this regression.');
const out=process.env.QA_OUTPUT_DIR||'test-results/audio';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream',...(process.platform==='darwin'?['--use-angle=metal']:[])]});
try {
 const context=await browser.newContext({baseURL:process.env.QA_BASE_URL,permissions:['microphone'],viewport:{width:1360,height:1000}});
 const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>{
  localStorage.setItem('fly-tutorial-v1','skipped');
  window.__audio={tracks:[],mic:0,starts:0,stops:0};
  const gum=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
  navigator.mediaDevices.getUserMedia=async c=>{window.__audio.mic++;const s=await gum(c);window.__audio.tracks.push(...s.getTracks());return s;};
  const start=AudioBufferSourceNode.prototype.start,stop=AudioBufferSourceNode.prototype.stop;
  AudioBufferSourceNode.prototype.start=function(...a){window.__audio.starts++;return start.apply(this,a);};
  AudioBufferSourceNode.prototype.stop=function(...a){window.__audio.stops++;return stop.apply(this,a);};
 });
 let connections=0,announcements=0,latest;
 await page.routeWebSocket('**/ws/voice?*',s=>{connections++;latest=s;s.onMessage(()=>{});s.send(JSON.stringify({type:'hello'}));s.send(JSON.stringify({type:'wake_state',wake_enabled:false,awake:true}));});
 await page.route('**/announcement',async route=>{
  announcements++;const body=route.request().postDataJSON();
  const pcm=Buffer.alloc(24000*2*4);for(let i=0;i<pcm.length/2;i++)pcm.writeInt16LE(Math.round(500*Math.sin(i/24)),i*2);
  await route.fulfill({json:{key:body.key,text:'轮到小朋友掷骰子啦！',audio:pcm.toString('base64'),sample_rate:24000}});
 });
 await page.goto('/play',{waitUntil:'networkidle'});
 let room=(await(await page.request.get('/api/session')).json()).room;
 const opts={names:['小朋友','家长'],props:true,board:'cross',mode:'quick',waiting:false};
 let res=await page.request.post(`/api/rooms/${room.id}/matches`,{data:opts});assert.equal(res.status(),200);room=await res.json();
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor();assert.equal(connections,1);
 // Muting ends real fake-device media tracks and cancels queued realtime PCM.
 latest.send(JSON.stringify({type:'response.created',response:{id:'audio-test'}}));latest.send(JSON.stringify({type:'response.output_audio.delta',response_id:'audio-test',delta:Buffer.alloc(24000*2*4).toString('base64')}));await page.waitForFunction(()=>window.__audio.starts>0);
 await page.getByRole('button',{name:'总静音',exact:true}).click();
 await page.waitForFunction(()=>window.__audio.tracks.every(t=>t.readyState==='ended')&&window.__audio.stops>0);
 assert.equal(await page.evaluate(()=>localStorage.getItem('fly-auto-voice')),null,'master mute changed microphone preference');
 await page.reload({waitUntil:'networkidle'});assert.equal(connections,1,'muted refresh reconnected microphone');assert.equal(await page.evaluate(()=>window.__audio.mic),0);
 await page.getByRole('button',{name:'恢复声音',exact:true}).click();await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor();assert.equal(connections,2);
 // Disable SFX, then turn off mic: output-only narration must still play.
 await page.getByRole('button',{name:'声音设置',exact:true}).click();let modal=page.getByRole('dialog',{name:'声音设置',exact:true});
 await modal.getByRole('checkbox',{name:'游戏音效（掷骰、起飞、撞机、抵达）',exact:true}).uncheck();
 await modal.getByRole('checkbox',{name:'自动连接语音，直接说话',exact:true}).uncheck();
 await modal.getByRole('button',{name:'完成设置',exact:true}).click();
 await page.waitForFunction(()=>window.__audio.tracks.every(t=>t.readyState==='ended'));
 await page.getByText('灵矽正在播报回合',{exact:true}).waitFor();assert.ok(announcements>0);const beforeStop=await page.evaluate(()=>window.__audio.stops);
 await page.getByRole('button',{name:'总静音',exact:true}).click();await page.waitForFunction(n=>window.__audio.stops>n,beforeStop);
 await page.getByRole('button',{name:'恢复声音',exact:true}).click();assert.equal(connections,2,'restore ignored mic-off preference');
 await page.getByRole('button',{name:'声音设置',exact:true}).click();modal=page.getByRole('dialog',{name:'声音设置',exact:true});
 assert.equal(await modal.getByRole('checkbox',{name:'游戏音效（掷骰、起飞、撞机、抵达）',exact:true}).isChecked(),false);
 assert.equal(await modal.getByRole('checkbox',{name:'自动连接语音，直接说话',exact:true}).isChecked(),false);
 assert.equal(await modal.getByRole('checkbox',{name:'关闭麦克风时也播报回合',exact:true}).isChecked(),true);
 await page.screenshot({path:out+'/audio-settings.png'});
 await modal.getByRole('checkbox',{name:'关闭麦克风时也播报回合',exact:true}).uncheck();await modal.getByRole('checkbox',{name:'游戏音效（掷骰、起飞、撞机、抵达）',exact:true}).check();await modal.getByRole('button',{name:'完成设置',exact:true}).click();
 const beforeNarration=announcements;res=await page.request.post(`/api/rooms/${room.id}/matches`,{data:opts});assert.equal(res.status(),200);
 await page.waitForTimeout(800);assert.equal(announcements,beforeNarration,'disabled narration generated speech');
 await page.reload({waitUntil:'networkidle'});assert.equal(connections,2);assert.equal(await page.getByRole('checkbox',{name:'关闭麦克风时也播报回合',exact:true}).isChecked(),false);
 assert.deepEqual(errors,[]);
 const report={passed:true,real_chrome_audio_nodes:true,microphone:'Chrome synthetic device; no physical microphone',provider:'controlled WebSocket and PCM; no upstream API call',cases:['immediate realtime PCM stop and track release','mute persists after reload','restore preserves microphone preference','TTS plays independently of disabled SFX','TTS immediately stops on mute','separate preferences persist','disabled TTS does not request synthesis'],console_errors:errors};
 await writeFile(out+'/results.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));
}finally{await browser.close();}
