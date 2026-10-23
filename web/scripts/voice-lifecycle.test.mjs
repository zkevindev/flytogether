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
 constructor(){this.currentTime=0;this.destination={};this.audioWorklet={addModule:async()=>{}};contexts.push(this);}
 async resume(){} async close(){this.closed=true;}
 createMediaStreamSource(){return new NodeMock();}
 createBuffer(_channels,length,rate){return {duration:length/rate,getChannelData:()=>new Float32Array(length)};}
 createBufferSource(){const node=new NodeMock();node.start=()=>playback.push(node);node.stop=()=>{node.stopped=true;};return node;}
}
class WorkletMock extends NodeMock {constructor(){super();this.port={onmessage:null};}}
class SocketMock {
 static OPEN=1;
 constructor(){this.readyState=1;sockets.push(this);}
 send(){} close(){this.readyState=3;}
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
  const {client, ws} = await connected();
  ws.emit({type: 'wake_state', wake_enabled: true, awake: false});
  ws.emit({type: 'response.created', response: {id: 'late'}});
  ws.emit(audio('late'));
  assert.equal(playback.length, 0);
  ws.emit({type: 'host_cue'});
  ws.emit({type: 'response.created', response: {id: 'host'}});
  ws.emit(audio('host'));
  assert.equal(playback.length, 1);
  client.disconnect();
});
test('socket failure after hello releases resources and offers manual reconnect',async()=>{
 const {client,ws,states,errors}=await connected();ws.onerror();assert.equal(track.stopped,true);assert.equal(states.at(-1),'disconnected');assert.match(errors[0],/重新开启/);client.disconnect();
});
