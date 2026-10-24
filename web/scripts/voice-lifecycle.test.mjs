import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const dir = await mkdtemp(join(tmpdir(), 'fly-voice-test-'));
execFileSync(fileURLToPath(new URL('../node_modules/.bin/tsc', import.meta.url)), ['--ignoreConfig','--target','ES2022','--module','ESNext','--moduleResolution','bundler','--skipLibCheck','--outDir',dir, 'src/lib/voice.ts','src/lib/types.ts'], {cwd:fileURLToPath(new URL('..', import.meta.url))});
for (const file of ['types', 'voice']) {
 const outputText = await readFile(join(dir, `${file}.js`), 'utf8');
 await writeFile(join(dir, `${file}.mjs`), outputText.replace("'./types'", "'./types.mjs'"));
}
const { VoiceClient } = await import(pathToFileURL(join(dir,'voice.mjs')));
await rm(dir, {recursive:true});
const tick = () => new Promise(resolve => setImmediate(resolve));
let sockets, contexts, playback, track;
class NodeMock { connect() {} disconnect() {this.disconnected=true;} }
class AudioMock {
 constructor(){this.currentTime=0;this.state='running';this.destination={};this.audioWorklet={addModule:async()=>{}};contexts.push(this);}
 async resume(){} async close(){this.closed=true;}
 createMediaStreamSource(){return new NodeMock();}
 createBuffer(_channels,length,rate){return {duration:length/rate,getChannelData:()=>new Float32Array(length)};}
 createBufferSource(){const node=new NodeMock();node.start=at=>{node.startAt=at;playback.push(node);};node.stop=()=>{node.stopped=true;};return node;}
}
class WorkletMock extends NodeMock {constructor(){super();this.port={onmessage:null};}}
class SocketMock {
 static OPEN=1;
 constructor(){this.readyState=1;sockets.push(this);}
 send(value){(this.sent||=[]).push(JSON.parse(value));} close(){this.readyState=3;}
 emit(message){this.onmessage?.({data:JSON.stringify(message)});}
}
function setup(getUserMedia){
 sockets=[];contexts=[];playback=[];track={stop(){this.stopped=true;}};
 globalThis.window={isSecureContext:true};globalThis.location={protocol:'https:',host:'game.test'};
 globalThis.AudioContext=AudioMock;globalThis.AudioWorkletNode=WorkletMock;globalThis.WebSocket=SocketMock;
 Object.defineProperty(globalThis,'navigator',{configurable:true,value:{mediaDevices:{getUserMedia:getUserMedia||(async()=>({getTracks:()=>[track]}))}}});
 const states=[],texts=[],errors=[];
 return {client:new VoiceClient('room',(s)=>states.push(s),(text,who)=>texts.push({text,who}),(e)=>errors.push(e)),states,texts,errors};
}
async function connected(){const t=setup();const p=t.client.connect();await tick();sockets[0].emit({type:'hello'});await p;return {...t,ws:sockets[0]};}
const audio=id=>({type:'response.output_audio.delta',response_id:id,delta:'AAAAAA=='});

test('disconnect while microphone permission is pending stops late stream and never opens a socket',async()=>{
 let release;const pending=new Promise(resolve=>{release=resolve;});const t=setup(()=>pending);
 const p=t.client.connect();await tick();t.client.disconnect();release({getTracks:()=>[track]});await p;
 assert.equal(track.stopped,true);assert.equal(sockets.length,0);assert.equal(contexts[0].closed,true);assert.equal(t.states.at(-1),'disconnected');
});
test('close before hello rejects immediately and releases microphone',async()=>{
 const t=setup();const p=t.client.connect();const rejected=assert.rejects(p,/提前断开/);await tick();sockets[0].onclose();await rejected;
 assert.equal(track.stopped,true);assert.equal(contexts[0].closed,true);
});
test('user disconnect cancels an unfinished handshake without a hanging timeout',async()=>{
 const t=setup();const p=t.client.connect();await tick();t.client.disconnect();await p;
 assert.equal(track.stopped,true);assert.equal(sockets[0].readyState,3);
});
test('stop discards late audio and final captions; next accepted utterance can speak',async()=>{
 const {client,ws,texts}=await connected();ws.emit({type:'response.created',response:{id:'old'}});ws.emit(audio('old'));assert.equal(playback.length,1);
 client.stopPlayback();assert.equal(playback[0].stopped,true);ws.emit(audio('old'));ws.emit({type:'response.output_audio_transcript.done',response_id:'old',transcript:'迟到的回复'});
 assert.equal(playback.length,1);assert.equal(texts.length,0);
 ws.emit({type:'stt',text:'下一句话'});ws.emit({type:'response.created',response:{id:'new'}});ws.emit(audio('old'));ws.emit(audio('new'));
 assert.equal(playback.length,2);client.disconnect();
});
test('standby suppresses final captions and interruption detaches old playback callbacks',async()=>{
 const {client,ws,texts,states}=await connected();ws.emit({type:'response.created',response:{id:'old'}});ws.emit(audio('old'));
 client.standby();ws.emit({type:'response.output_audio_transcript.done',response_id:'old',transcript:'不应显示'});assert.equal(texts.length,0);
 assert.equal(playback[0].onended,null);assert.equal(playback[0].disconnected,true);assert.equal(states.at(-1),'standby');client.disconnect();
});
test('host line still plays while waiting for the wake word', async () => {
  const {client, ws, texts} = await connected();
  ws.emit({type: 'wake_state', wake_enabled: true, awake: false});
  ws.emit({type: 'response.created', response: {id: 'late'}});
  ws.emit(audio('late'));
  assert.equal(playback.length, 0);
  ws.emit({type: 'host_cue', text: '骰子给你'});
  assert.equal(texts.at(-1).text, '骰子给你');
  assert.equal(texts.at(-1).who, 'linx');
  ws.emit({type: 'response.created', response: {id: 'host'}});
  ws.emit(audio('host'));
  assert.equal(playback.length, 1);
  client.disconnect();
});
test('socket failure after hello releases resources and offers manual reconnect',async()=>{
 const {client,ws,states,errors}=await connected();ws.onerror();assert.equal(track.stopped,true);assert.equal(states.at(-1),'disconnected');assert.match(errors[0],/重新开启/);client.disconnect();
});

test('automatic connect requests the microphone even while audio autoplay is suspended, and cancel releases it',async()=>{
 const original=AudioMock.prototype.resume;
 AudioMock.prototype.resume=()=>new Promise(()=>{});
 try {
  let requested=false;const t=setup(async()=>{requested=true;return {getTracks:()=>[track]};});
  const pending=t.client.connect();await tick();
  assert.equal(requested,true);assert.equal(sockets.length,0);
  t.client.disconnect();await pending;
  assert.equal(track.stopped,true);assert.equal(contexts[0].closed,true);
 }finally{AudioMock.prototype.resume=original;}
});
test('blocked audio autoplay times out clearly and releases the acquired microphone',async t=>{
 const original=AudioMock.prototype.resume;
 AudioMock.prototype.resume=()=>new Promise(()=>{});
 t.mock.timers.enable({apis:['setTimeout']});
 try {
  const state=setup();const rejected=assert.rejects(state.client.connect(),/需要一次点击/);await tick();
  t.mock.timers.tick(2600);await rejected;
  assert.equal(track.stopped,true);assert.equal(contexts[0].closed,true);assert.equal(sockets.length,0);
 }finally{AudioMock.prototype.resume=original;t.mock.timers.reset();}
});
test('denied microphone permission does not get stuck behind an unresolved audio unlock',async()=>{
 const original=AudioMock.prototype.resume;
 AudioMock.prototype.resume=()=>new Promise(()=>{});
 try {
  const t=setup(async()=>{throw new DOMException('Permission denied','NotAllowedError');});
  await assert.rejects(t.client.connect(),/麦克风权限未开启/);
  assert.equal(contexts[0].closed,true);assert.equal(sockets.length,0);
 }finally{AudioMock.prototype.resume=original;}
});


test('a fast 24-second reply stays connected and only schedules a short audio horizon',async()=>{
 const {client,ws,states,errors}=await connected();const ctx=contexts[0];
 ws.emit({type:'response.created',response:{id:'long'}});
 const delta=Buffer.alloc(24000*2).toString('base64');
 for(let i=0;i<24;i++)ws.emit({...audio('long'),delta});
 ws.emit({type:'response.done',response:{id:'long',status:'completed'}});
 assert.equal(ws.readyState,1);assert.deepEqual(errors,[]);assert.ok(playback.length<=5);
 assert.equal(states.at(-1),'speaking');
 for(let i=0;i<300;i++){
  ctx.currentTime+=.1;
  const ended=[...playback].filter(node=>!node.ended&&!node.stopped&&node.onended&&node.startAt+node.buffer.duration<=ctx.currentTime);
  for(const source of ended){source.ended=true;source.onended?.();}
 }
 assert.ok(playback.length>=240,'all audio eventually drains, not discarded at twelve seconds');
 assert.equal(states.at(-1),'ready');assert.equal(ws.readyState,1);client.disconnect();
});
test('suspended audio keeps bounded pending PCM, then resumes without a reconnect',async()=>{
 const {client,ws,errors}=await connected();const ctx=contexts[0];ctx.state='suspended';
 ws.emit({type:'response.created',response:{id:'paused'}});
 ws.emit({...audio('paused'),delta:Buffer.alloc(24000*2*16).toString('base64')});
 assert.equal(playback.length,0);assert.deepEqual(errors,[]);assert.equal(ws.readyState,1);
 ctx.currentTime=30;ctx.state='running';ctx.onstatechange();
 assert.ok(playback.length>0&&playback.length<=5);client.stopPlayback();
 assert.ok(playback.every(node=>node.stopped));client.disconnect();
});
test('overflow cancels only the current response and the next direct utterance still plays',async()=>{
 const {client,ws,errors}=await connected();ws.emit({type:'response.created',response:{id:'overflow'}});
 const delta=Buffer.alloc(24000*2*10).toString('base64');
 for(let i=0;i<10;i++)ws.emit({...audio('overflow'),delta});
 assert.equal(ws.readyState,1);assert.equal(track.stopped,undefined);assert.equal(errors.length,1);
 assert.match(errors[0],/语音仍连接/);assert.equal(ws.sent.at(-1).type,'abort');
 const count=playback.length;ws.emit(audio('overflow'));assert.equal(playback.length,count);
 ws.emit({type:'stt',text:'现在轮到谁'});ws.emit({type:'response.created',response:{id:'next'}});ws.emit(audio('next'));
 assert.equal(playback.length,count+1);client.disconnect();
});
test('interruption drops unscheduled PCM as well as late chunks of the old reply',async()=>{
 const {client,ws}=await connected();const ctx=contexts[0];
 ws.emit({type:'response.created',response:{id:'old-long'}});ws.emit({...audio('old-long'),delta:Buffer.alloc(24000*2*20).toString('base64')});
 const oldCount=playback.length;ws.emit({type:'speech_started'});ctx.currentTime=40;
 ws.emit({type:'stt',text:'掷骰子'});ws.emit({type:'response.created',response:{id:'new'}});ws.emit(audio('old-long'));ws.emit(audio('new'));
 assert.ok(playback.slice(0,oldCount).every(node=>node.stopped));assert.equal(playback.length,oldCount+1);client.disconnect();
});


test('changing preferences cancels a created host response without muting the next requested reply',async()=>{
 const {client,ws}=await connected();ws.emit({type:'host_cue',text:'旧主持'});ws.emit({type:'response.created',response:{id:'host'}});ws.emit(audio('host'));
 client.setCompanion({style:'coach',frequency:'quiet'});assert.equal(ws.sent.at(-1).type,'companion_preferences');assert.equal(sockets.length,1);
 ws.emit({type:'host_cancel'});assert.equal(playback[0].stopped,true);ws.emit(audio('host'));assert.equal(playback.length,1);
 ws.emit({type:'stt',text:'现在轮到谁'});ws.emit({type:'response.created',response:{id:'answer'}});ws.emit(audio('answer'));assert.equal(playback.length,2);client.disconnect();
});
