import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {join,resolve} from 'node:path';
assert.ok(process.env.QA_BASE_URL&&process.env.QA_PROP_AUDIO_DIR,'Set QA_BASE_URL and QA_PROP_AUDIO_DIR');
const out=process.env.QA_OUTPUT_DIR||'test-results/props-voice';await mkdir(out,{recursive:true});const report=[];
for(const id of ['boomerang','rescue','escort']){
 const browser=await chromium.launch({channel:'chrome',headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream','--use-file-for-fake-audio-capture='+resolve(process.env.QA_PROP_AUDIO_DIR,id,'browser-input.wav'),...(process.platform==='darwin'?['--use-angle=metal']:[])]});
 try{
  const c=await browser.newContext({baseURL:process.env.QA_BASE_URL,permissions:['microphone']});
  await c.addInitScript(()=>{localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-experience',JSON.stringify({board:'flat',motion:'reduced'}));window.__propVoice={events:[],audio:0,audio_after_action:0};const WS=WebSocket;window.WebSocket=class extends WS{constructor(url,...args){super(url,...args);if(String(url).includes('/ws/voice?'))this.addEventListener('message',e=>{if(typeof e.data!=='string')return;const v=JSON.parse(e.data);if(v.type.endsWith('audio.delta')){window.__propVoice.audio++;if(window.__propVoice.events.some(e=>e.type==='game_action'))window.__propVoice.audio_after_action++;}if(['stt','game_action','error','response.done','response.audio_transcript.done','response.output_audio_transcript.done'].includes(v.type))window.__propVoice.events.push({type:v.type,text:v.text||v.transcript,action:v.action,version:v.room?.state.version,status:v.response?.status});});}};});
  const res=await c.request.post('/api/rooms',{data:{names:['小朋友','爸爸'],demo:true,props:true,waiting:false,board:'cross',mode:'classic'}});assert.equal(res.status(),201);
  const p=await c.newPage(),errors=[];p.on('pageerror',e=>errors.push(e.message));await p.goto('/play',{waitUntil:'networkidle'});await p.getByRole('checkbox',{name:'自动连接语音，直接说话',exact:true}).check();await p.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({timeout:20000});
  await p.waitForFunction(()=>window.__propVoice.events.some(e=>e.type==='game_action'),null,{timeout:32000});
  try{await p.waitForFunction(()=>{const a=window.__propVoice.events,i=a.findIndex(e=>e.type==='game_action');return i>=0&&a.slice(i+1).some(e=>e.type.endsWith('audio_transcript.done'));},null,{timeout:20000});}catch(e){console.log(JSON.stringify(await p.evaluate(()=>window.__propVoice)));throw e;}
  const s=(await(await c.request.get('/api/session')).json()).room.state,events=await p.evaluate(()=>window.__propVoice);assert.equal(s.players[0].props[id],0);assert.equal(s.players[0].stats.props_used,1);assert.equal(s.players[0].stats.rolls,0);assert.equal(s.version,2);
  if(id==='boomerang')assert.equal(s.players[1].pieces[0].progress,13);if(id==='rescue')assert.equal(s.players[0].pieces[0].progress,0);if(id==='escort')assert.ok(s.players[0].pieces.slice(0,2).every(p=>p.shield));const actionIndex=events.events.findIndex(e=>e.type==='game_action'),reply=events.events.slice(actionIndex+1).filter(e=>e.type.endsWith('audio_transcript.done')).map(e=>e.text||'').join('');assert.match(reply,{boomerang:/回旋镖.*后退.*3|回旋镖.*后退.*三/,rescue:/救援.*撤回.*起飞位/,escort:/团队护航.*护盾/}[id]);assert.ok(!reply.includes('要炸就说'));assert.ok(events.audio_after_action>0);assert.equal(events.events.filter(e=>e.type==='error').length,0);assert.deepEqual(errors,[]);
  await p.getByRole('button',{name:'关闭对讲麦克风',exact:true}).click();report.push({id,version:s.version,actual_effect:s.history.at(-1),...events});console.log(JSON.stringify({passed:true,id,events:events.events}));
 }finally{await browser.close();}
}
await writeFile(join(out,'results.json'),JSON.stringify({passed:true,scope:'Synthetic Tingting WAV through real Chrome AudioWorklet, Go and actual Lingxi; official demo setup, no injected transcripts/dice; not physical microphone acceptance',report},null,2));
