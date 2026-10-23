import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL');
const output=process.env.QA_OUTPUT_DIR||'test-results/fun';await mkdir(output,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const errors=[];
const options={names:['小朋友','爸爸'],board:'cross',mode:'classic',ai:false,props:true,demo:true,waiting:false};
const contexts=[];
const setup=async()=>{
 const context=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width:1280,height:1000}});contexts.push(context);
 const created=await context.request.post('/api/rooms',{data:options});assert.equal(created.status(),201);let room=await created.json();
 const state=async()=>{room=(await(await context.request.get('/api/session')).json()).room;return room.state;};
 const command=async(action,extra={})=>{await state();const response=await context.request.post('/api/rooms/'+room.id+'/commands',{data:{command_id:crypto.randomUUID(),game_id:room.state.id,expected_version:room.state.version,action,...extra}});assert.equal(response.status(),200);room=await response.json();return room.state;};
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>{
  localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');
  window.__qa={microphone:0,playbacks:0,frequencies:[]};
  navigator.mediaDevices.getUserMedia=async()=>{window.__qa.microphone++;throw new Error('This test never needs microphone capture');};
  const create=AudioContext.prototype.createBufferSource;
  AudioContext.prototype.createBufferSource=function(...args){const node=create.apply(this,args),start=node.start.bind(node);node.start=(...args)=>{window.__qa.playbacks++;return start(...args);};return node;};
  const set=AudioParam.prototype.setValueAtTime;
  AudioParam.prototype.setValueAtTime=function(value,...args){window.__qa.frequencies.push(value);return set.call(this,value,...args);};
 });
 return {context,page,state,command,room:()=>room};
};
const pcmWav=pcm=>{const header=Buffer.alloc(44);header.write('RIFF');header.writeUInt32LE(36+pcm.length,4);header.write('WAVEfmt ',8);header.writeUInt32LE(16,16);header.writeUInt16LE(1,20);header.writeUInt16LE(1,22);header.writeUInt32LE(24000,24);header.writeUInt32LE(48000,28);header.writeUInt16LE(2,32);header.writeUInt16LE(16,34);header.write('data',36);header.writeUInt32LE(pcm.length,40);return Buffer.concat([header,pcm]);};
try{
 const t=await setup();await t.page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 let announcements=0;t.page.on('request',req=>{if(req.url().endsWith('/announcement'))announcements++;});
 const speechResponse=t.page.waitForResponse(res=>res.url().endsWith('/announcement')&&res.status()===200,{timeout:20000});
 await t.page.getByRole('checkbox',{name:'关闭麦克风时也播报回合'}).check();
 const first=await(await speechResponse).json();const pcm=Buffer.from(first.audio,'base64');
 assert.ok(pcm.length>10000&&pcm.some(value=>value!==0));assert.match(first.text,/轮到小朋友掷骰子/);
 await t.page.waitForFunction(()=>window.__qa.playbacks>0);assert.equal(await t.page.evaluate(()=>window.__qa.microphone),0);
 await writeFile(join(output,'turn-announcement.wav'),pcmWav(pcm));
 const baseline=await t.state();assert.equal(baseline.version,1,'TTS does not execute game tools');
 let s=await t.command('roll');
 if(s.phase==='move')s=await t.command('move',{piece_id:s.legal_moves[0].piece_id});
 await t.page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor({state:'visible'});
 assert.equal(announcements,1,'next-turn narration waits for the dice popup');
 const nextResponse=t.page.waitForResponse(res=>res.url().endsWith('/announcement')&&res.status()===200,{timeout:20000});
 await t.page.getByRole('button',{name:'继续飞行',exact:true}).click();
 const next=await(await nextResponse).json();assert.notEqual(next.key,first.key);assert.match(next.text,new RegExp(s.players[s.current].name));
 assert.equal(await t.page.evaluate(()=>window.__qa.microphone),0);
 await t.page.locator('.voice-panel').screenshot({path:join(output,'independent-narration.png')});
 console.log(JSON.stringify({result:'PASS',case:'real Lingxi TTS without microphone',pcm_bytes:pcm.length,first:first.text,next:next.text,game_version_unchanged_by_tts:true}));
 await t.context.close();

 for(const kind of ['capture','finish']){
  const item=await setup();let picked;let steps=0;
  for(;steps<1400;steps++){
   const state=await item.state();
   if(state.phase==='finished')throw new Error('preparation reached an unexpected completed game');
   if(state.phase==='roll'){await item.command('roll');continue;}
   picked=state.legal_moves.find(m=>kind==='capture'?m.captured.length>0:m.finish);
   if(picked)break;
   const move=[...state.legal_moves].sort((a,b)=>b.to-a.to)[0];assert.ok(move);await item.command('move',{piece_id:move.piece_id});
  }
  assert.ok(picked,`could not prepare a natural ${kind} move`);
  await item.page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
  const before=await item.state();
  await item.page.getByRole('button',{name:picked.label,exact:true}).click();
  if(kind==='capture'){
   await item.page.getByText('被撞回机场',{exact:true}).waitFor({state:'visible'});
   const after=await item.state();for(const id of picked.captured)assert.equal(after.players.flatMap(p=>p.pieces).find(p=>p.id===id).progress,-1);
   assert.ok(await item.page.evaluate(()=>window.__qa.frequencies.includes(620)),'descending crash engine sound was scheduled');
   await item.page.locator('.board-stage').screenshot({path:join(output,'crash-feedback.png')});
  }else{
   await item.page.getByRole('status',{name:'抵达终点撒花庆祝'}).waitFor({state:'visible'});
   assert.equal(await item.page.locator('.confetti-rain i').count(),40);
   const after=await item.state();assert.equal(after.players.flatMap(p=>p.pieces).find(p=>p.id===picked.piece_id).progress,before.board.finish);
   await item.page.locator('.board-stage').screenshot({path:join(output,'finish-confetti.png')});
   // A prepared classic match celebrates a single finish without declaring a winner.
   assert.equal(after.phase,'roll');
   await item.command('pause');await item.page.locator('.celebration').waitFor({state:'hidden'});
  }
  assert.equal(await item.page.evaluate(()=>window.__qa.microphone),0);
  console.log(JSON.stringify({result:'PASS',case:kind,preparation_commands:steps,actual_move:picked.summary}));
  await item.context.close();
 }
 assert.deepEqual(errors,[]);
}finally{await browser.close();}
