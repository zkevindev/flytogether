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
 localStorage.setItem('fly-tutorial-v1','skipped');
  window.__qa={events:[],tracks:[],sources:[],diceDialogs:[]};
  const seenDice=new WeakSet();
  const observeDice=()=>{for(const dialog of document.querySelectorAll('dialog[aria-label="3D 掷骰结果"]')){if(!seenDice.has(dialog)&&dialog.querySelector('.dice-cube')){seenDice.add(dialog);window.__qa.diceDialogs.push({at:performance.now(),values:[...dialog.querySelectorAll('.dice-cube')].map(n=>Number(n.dataset.value))});}}};
  new MutationObserver(observeDice).observe(document,{childList:true,subtree:true});
  const Original=WebSocket;
  window.WebSocket=class extends Original{constructor(url,...args){super(url,...args);if(String(url).includes('/ws/voice?'))this.addEventListener('message',e=>{
   if(typeof e.data!=='string')return;let m;try{m=JSON.parse(e.data);}catch{return;}
   const record={at:performance.now(),type:m.type,response_id:m.response_id||m.response?.id,text:m.text||m.transcript||m.delta,status:m.response?.status};
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
 await page.goto(new URL('/play', process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 let room=(await(await page.request.get('/api/session')).json()).room;
 const coaching=process.env.QA_SCENARIO==='coach';
 const options={demo:coaching,room_name:'直接对话联调',names:['小朋友','爸爸'],board:'cross',mode:'quick',ai:false,props:true,waiting:false};
 let res=await page.request.post('/api/rooms/'+room.id+'/matches',{data:options});assert.equal(res.status(),200);room=await res.json();
 if(coaching){
  const roll=await page.request.post('/api/rooms/'+room.id+'/commands',{data:{command_id:crypto.randomUUID(),game_id:room.state.id,expected_version:room.state.version,action:'roll'}});assert.equal(roll.status(),200);room=await roll.json();
  const dice=page.getByRole('dialog',{name:'3D 掷骰结果'});await dice.waitFor({state:'visible'});await dice.getByRole('button',{name:'继续飞行',exact:true}).click();
 }
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({state:'visible',timeout:30000});
 log({phase:'connected_without_voice_button',version:room.state.version});
 const state=async()=>{room=(await(await page.request.get('/api/session')).json()).room;return room.state;};

 const lucky=process.env.QA_SCENARIO==='lucky';
 const until=async predicate=>{const deadline=Date.now()+25000;while(Date.now()<deadline){const s=await state();if(predicate(s))return s;await new Promise(r=>setTimeout(r,200));}throw new Error('Timed out waiting for expected authoritative game state');};
 if(coaching){
  await until(()=>room.coach&&room.state.version===2);
  const first=room.coach;
  await page.locator('.landing-marker:not([hidden])').waitFor({state:'visible'});
  assert.equal(await page.locator('.move-preview').getAttribute('data-piece-id'),first.move.piece_id);
  log({phase:'voice_coach_preview_without_move',piece:first.move.label,version:2});
  await until(()=>room.coach&&room.coach.id!==first.id);const second=room.coach;
  assert.notEqual(second.move.piece_id,first.move.piece_id);assert.equal(room.state.version,2);
  log({phase:'voice_alternative_without_move',piece:second.move.label,version:2});
  await until(()=>room.coach&&room.coach.id!==second.id);const safe=room.coach;assert.equal(room.state.version,2);
  log({phase:'voice_safe_without_move',piece:safe.move.label,version:2});
  await until(()=>room.coach&&room.coach.id!==safe.id);const previous=room.coach;assert.equal(room.state.version,2);
  log({phase:'voice_previous_without_move',piece:previous.move.label,version:2});
  await page.locator('.move-preview').screenshot({path:join(outputDir,'voice-coach-preview.png')});
  const moved=await until(s=>s.version===3);
  assert.equal(moved.players.flatMap(p=>p.pieces).find(p=>p.id===previous.move.piece_id).progress,previous.move.to);
  assert.equal(room.coach,undefined);
  log({phase:'voice_confirm_committed_once',piece:previous.move.label,to:previous.move.to,version:3});
  await page.waitForFunction(()=>window.__qa.events.filter(e=>e.type==='stt').length>=6,null,{timeout:40000});
  await page.waitForFunction(()=>{const e=window.__qa.events;const last=e.findLastIndex(x=>x.type==='stt');return e.slice(last+1).some(x=>x.type==='response.done'&&x.status==='completed');},null,{timeout:15000});
  assert.equal((await state()).version,3,'negative speech must not roll again');
 }else if(lucky){
  const s=await until(s=>s.players[0].props.luck===0&&s.dice_rolls.length===2);
  assert.equal(s.version,3);assert.equal(s.dice,Math.max(...s.dice_rolls));
  await page.waitForFunction(()=>window.__qa.diceDialogs.some(d=>d.values.length===2));
  const shown=await page.evaluate(()=>window.__qa.diceDialogs.find(d=>d.values.length===2));
  assert.deepEqual(shown.values,s.dice_rolls);
  log({phase:'voice_lucky_actually_rolled_with_popup',dice:s.dice_rolls,version:s.version,dialog_at:shown.at});
  await page.waitForFunction(()=>window.__qa.events.filter(e=>e.type==='stt').length>=2,null,{timeout:40000});
  await page.waitForFunction(()=>{const e=window.__qa.events;const last=e.findLastIndex(x=>x.type==='stt');return e.slice(last+1).some(x=>x.type==='response.done'&&x.status==='completed');},null,{timeout:15000});
  assert.equal((await state()).version,3,'negative speech after lucky dice must not throw again');
 }else{
 // The first direct query must receive a reply without changing the game.
 await new Promise(resolve=>setTimeout(resolve,14000));assert.equal((await state()).version,1);await page.waitForFunction(() => window.__qa.events.some(e=>e.type==='stt'&&e.text?.includes('轮到')), null, {timeout:5000});
 assert.equal(room.voice_settings.wake_enabled,false);log({phase:'direct_query_without_wake',version:1});

 let s=await until(s=>s.active_prop==='launch');assert.equal(s.version,2);assert.equal(s.players[0].props.launch,0);log({phase:'launch_card',version:s.version});
 s=await until(s=>s.phase==='move');assert.equal(s.version,3);assert.ok(s.dice>=1&&s.dice<=6);log({phase:'real_roll',dice:s.dice,version:s.version});
 s=await until(s=>s.players[0].pieces[0].progress===0);assert.equal(s.version,4);log({phase:'voice_takeoff',progress:0,version:4});
 await page.waitForFunction(()=>window.__qa.events.filter(e=>e.type==='stt').length>=6,null,{timeout:40000});
 await page.waitForFunction(()=>{const e=window.__qa.events;const last=e.findLastIndex(x=>x.type==='stt');return e.slice(last+1).some(x=>x.type==='response.done'&&x.status==='completed');},null,{timeout:15000});
 assert.equal((await state()).version,4,'query and negative speech must not change state');
 assert.equal(await page.getByText(/语音播放积压|本次播报过长/).count(),0,'normal provider replies should not trigger playback overflow');

  const shown=await page.evaluate(()=>window.__qa.diceDialogs);
  assert.ok(shown.some(d=>d.values.length===1),'voice roll must actually display a dice popup');
 }
 const replies=await page.evaluate(()=>window.__qa.events.filter(e=>e.type.endsWith('audio_transcript.done')||e.type==='response.output_text.done').map(e=>({at:e.at,text:e.text})));
 log({phase:'actual_spoken_replies',replies});
 if(coaching){const turns=await page.evaluate(()=>window.__qa.events.filter(e=>e.type==='stt').map(e=>e.at));const advice=replies.filter(r=>r.at>=turns[0]&&r.at<turns[4]).map(r=>r.text||'').join('');assert.ok(!/没有风险|无风险|不会被.{0,5}撞|很安全|绝对安全|保证安全/.test(advice.replace(/(?:不能|无法|不)保证安全/g,'')),'voice must not turn a limited risk estimate into a safety guarantee');}
 if(lucky){
  const dice=(await state()).dice_rolls;
  const turns=await page.evaluate(()=>window.__qa.events.filter(e=>e.type==='stt').map(e=>e.at));
  const transcript=replies.filter(r=>r.at>=turns[0]&&r.at<turns[1]).map(r=>r.text||'').join('');
  const numerals=['零','一','二','三','四','五','六'];
  assert.ok(transcript.includes('幸运骰')&&transcript.includes('取'),'lucky narration must state both real dice and the chosen value');
  for(const value of dice)assert.ok(transcript.includes(String(value))||transcript.includes(numerals[value]),'spoken lucky result must contain actual dice values');
 }
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
 const before=await state();await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).click();
 await page.waitForFunction(()=>window.__qa.tracks.every(t=>t.readyState==='ended'));
 await page.getByRole('checkbox',{name:'自动连接语音，直接说话'}).check();
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({state:'visible'});
 const after=await state();assert.equal(after.id,before.id);assert.equal(after.version,before.version);
 log({phase:'real_reconnect_preserves_game',version:after.version});
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).click();
 await page.waitForFunction(()=>window.__qa.tracks.every(t=>t.readyState==='ended'));
 assert.deepEqual(errors,[]);
 await writeFile(join(outputDir, 'realtime-browser-results.json'),JSON.stringify({scope:'native Chrome fake WAV microphone -> actual worklet -> Go -> real Lingxi; no autoplay override',output,metrics,errors},null,2));
 console.log(coaching?'PASS: actual provider coach, visible preview, next/safe/previous with no game writes, one confirmed move, negative and reconnect':lucky?'PASS: actual provider accepts exact lucky-dice utterance, commits two real dice, shows popup, negative no-write and reconnect':'PASS: actual provider, browser auto-connect, real PCM capture path, visible random roll, takeoff, negative/query no-write, native reconnect');
} finally {await browser.close();}
