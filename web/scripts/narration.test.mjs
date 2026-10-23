import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Narrator, announcementKey } from '../src/lib/narration.ts';

test('only roll turns and winners receive unique announcement keys',()=>{
 assert.equal(announcementKey({id:'a',turn:1,phase:'roll'}),'a:1');
 assert.equal(announcementKey({id:'a',turn:1,phase:'move'}),'');
 assert.equal(announcementKey({id:'a',turn:2,phase:'roll'}),'a:2');
 assert.equal(announcementKey({id:'a',turn:2,phase:'finished'}),'a:winner');
});
test('a late audio unlock cannot play stale narration or stop a newer turn',async()=>{
 let release;const pending=new Promise(resolve=>{release=resolve;});const sources=[];
 class Audio {
  state='running';destination={};
  resume(){return pending;}
  createBuffer(_channels,size,rate){return {getChannelData:()=>new Float32Array(size),duration:size/rate};}
  createBufferSource(){const node={connect(){},disconnect(){},start(){sources.push(this);},stop(){this.stopped=true;}};return node;}
  async close(){this.state='closed';}
 }
 globalThis.AudioContext=Audio;
 const narrator=new Narrator();const old=narrator.playPCM('AQACAAMA',24000,()=>{});
 narrator.stop();
 const current=narrator.playPCM('AQACAAMA',24000,()=>{});
 release();assert.equal(await old,false);assert.equal(await current,true);assert.equal(sources.length,1);
 narrator.stop();assert.equal(sources[0].stopped,true);narrator.dispose();
});
