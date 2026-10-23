import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
assert.ok(process.env.QA_BASE_URL && process.env.QA_AUDIO_FILE, 'Set QA_BASE_URL and QA_AUDIO_FILE (see docs/QA-VOICE.md)');
const outputDir = process.env.QA_OUTPUT_DIR || 'test-results/voice';
await mkdir(outputDir, { recursive: true });
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream','--use-file-for-fake-audio-capture=' + resolve(process.env.QA_AUDIO_FILE),...(process.platform==='darwin'?['--use-angle=metal']:[])]});
try {
 const context=await browser.newContext({baseURL:process.env.QA_BASE_URL,permissions:['microphone'],viewport:{width:1280,height:1050}});
 const page=await context.newPage();const errors=[];const output=[];
 const log=value=>{output.push(value);console.log(JSON.stringify(value));};
 page.on('pageerror',e=>errors.push(e.message));
 await page.exposeBinding('qaEvent',(_,value)=>{if(['hello','stt','error','speech_stopped','response.done'].includes(value.type))log(value);});
 await page.addInitScript(()=>{
  window.__qa={events:[],tracks:[],sources:[]};
  const Original=WebSocket;
  window.WebSocket=class extends Original{constructor(url,...args){super(url,...args);if(String(url).includes('/ws/voice?'))this.addEventListener('message',e=>{
   if(typeof e.data!=='string')return;let m;try{m=JSON.parse(e.data);}catch{return;}
   const record={at:performance.now(),type:m.type,response_id:m.response_id||m.response?.id,text:m.text,status:m.response?.status};
   if(m.type.endsWith('audio.delta'))record.audio_bytes=Math.floor((m.delta||'').length*3/4);
   window.__qa.events.push(record);if(!m.type.endsWith('.delta'))void window.qaEvent(record);
  });}};
  const get=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
  navigator.mediaDevices.getUserMedia=async opts=>{const stream=await get(opts);window.__qa.tracks.push(...stream.getTracks());return stream;};
  const media=AudioContext.prototype.createMediaStreamSource;
  AudioContext.prototype.createMediaStreamSource=function(...args){this.__voice=true;return media.apply(this,args);};
  const create=AudioContext.prototype.createBufferSource;
  AudioContext.prototype.createBufferSource=function(...args){const source=create.apply(this,args);if(this.__voice){const start=source.start.bind(source);source.start=(...args)=>{window.__qa.sources.push({at:performance.now(),duration:source.buffer?.duration});return start(...args);};}return source;};
 });
 await page.goto(process.env.QA_BASE_URL,{waitUntil:'networkidle'});
 let room=(await(await page.request.get('/api/session')).json()).room;
 const options={room_name:'自动唤醒联调',names:['小朋友','爸爸'],board:'cross',mode:'quick',ai:false,props:true,waiting:false};
 let res=await page.request.post('/api/rooms/'+room.id+'/matches',{data:options});assert.equal(res.status(),200);room=await res.json();
 await page.getByRole('button',{name:'关闭语音与麦克风',exact:true}).waitFor({state:'visible',timeout:30000});
 log({phase:'connected_without_voice_button',version:room.state.version});
 const state=async()=>{room=(await(await page.request.get('/api/session')).json()).room;return room.state;};
 // The first utterance contains no wake word and must not arm any action.
 await new Promise(resolve=>setTimeout(resolve,14000));assert.equal((await state()).version,1);log({phase:'unwoken_ignored',version:1});
 const until=async predicate=>{const deadline=Date.now()+25000;while(Date.now()<deadline){const s=await state();if(predicate(s))return s;await new Promise(r=>setTimeout(r,200));}throw new Error('Timed out waiting for expected authoritative game state');};
 let s=await until(s=>s.active_prop==='launch');assert.equal(s.version,2);assert.equal(s.players[0].props.launch,0);log({phase:'launch_card',version:s.version});
 s=await until(s=>s.phase==='move');assert.equal(s.version,3);assert.ok(s.dice>=1&&s.dice<=6);log({phase:'real_roll',dice:s.dice,version:s.version});
 s=await until(s=>s.players[0].pieces[0].progress===0);assert.equal(s.version,4);log({phase:'voice_takeoff',progress:0,version:4});
 await page.waitForFunction(()=>window.__qa.events.filter(e=>e.type==='stt').length>=5,null,{timeout:40000});
 await page.waitForFunction(()=>{const e=window.__qa.events;const last=e.findLastIndex(x=>x.type==='stt');return e.slice(last+1).some(x=>x.type==='response.done'&&x.status==='completed');},null,{timeout:15000});
 assert.equal((await state()).version,4,'query and negative speech must not change state');
 const metrics=await page.evaluate(()=>{
  const events=window.__qa.events;return events.filter(e=>e.type==='stt').map(e=>{
   const start=events.indexOf(e);const end=events.slice(start+1).find(x=>x.type==='stt');
   const response=events.slice(start+1).find(x=>x.type==='response.created'&&(!end||x.at<end.at));
   const audio=response&&events.slice(start+1).find(x=>x.type.endsWith('audio.delta')&&x.response_id===response.response_id);
   const source=window.__qa.sources.find(x=>x.at>=e.at);
   return {text:e.text,asr_to_first_pcm_ms:audio?Math.round(audio.at-e.at):null,asr_to_playback_schedule_ms:source?Math.round(source.at-e.at):null};
  });
 });log({phase:'latency',samples:metrics});
 await page.locator('.voice-panel').screenshot({path:join(outputDir, 'auto-voice.png')});
 const before=await state();await page.getByRole('button',{name:'关闭语音与麦克风',exact:true}).click();
 await page.waitForFunction(()=>window.__qa.tracks.every(t=>t.readyState==='ended'));
 await page.getByRole('checkbox',{name:'自动连接语音，随时呼唤'}).check();
 await page.getByRole('button',{name:'关闭语音与麦克风',exact:true}).waitFor({state:'visible'});
 const after=await state();assert.equal(after.id,before.id);assert.equal(after.version,before.version);
 log({phase:'real_reconnect_preserves_game',version:after.version});
 await page.getByRole('button',{name:'关闭语音与麦克风',exact:true}).click();
 await page.waitForFunction(()=>window.__qa.tracks.every(t=>t.readyState==='ended'));
 assert.deepEqual(errors,[]);
 await writeFile(join(outputDir, 'realtime-browser-results.json'),JSON.stringify({scope:'native Chrome fake WAV microphone -> actual worklet -> Go -> real Lingxi; no autoplay override',output,metrics,errors},null,2));
 console.log('PASS: actual provider, browser auto-connect, real PCM capture path, random roll, takeoff, negative/query no-write, native reconnect');
} finally {await browser.close();}
