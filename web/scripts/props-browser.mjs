import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {join} from 'node:path';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL');
const base=process.env.QA_BASE_URL,out=process.env.QA_OUTPUT_DIR||'test-results/props';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const errors=[],report=[];
try{
 for(const [id,name,target,expected] of [['launch','起飞卡',null,null],['boost','加速卡',null,null],['luck','幸运骰',null,null],['bomb','炸弹','爸爸的小熊号',-1],['shield','护盾','小朋友的小熊号',12],['boomerang','回旋镖','爸爸的小熊号',13],['rescue','救援卡','小朋友的小熊号',0],['escort','团队护航',null,12]]){
  const mobile=id==='escort',flat=['boomerang','rescue','escort','shield'].includes(id);
  const c=await browser.newContext({baseURL:base,viewport:mobile?{width:390,height:844}:{width:1360,height:1000}});
  await c.addInitScript(({flat,mobile})=>{localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-experience',JSON.stringify({board:flat?'flat':'auto',motion:mobile?'reduced':'full'}));window.__qa={mic:0,contexts:0,losses:0,tones:[]};const original=HTMLCanvasElement.prototype.getContext;HTMLCanvasElement.prototype.getContext=function(type,...rest){if(type==='webgl'||type==='webgl2'){window.__qa.contexts++;this.addEventListener('webglcontextlost',()=>window.__qa.losses++);}return original.call(this,type,...rest);};const tone=AudioContext.prototype.createOscillator;AudioContext.prototype.createOscillator=function(...args){const osc=tone.apply(this,args);window.__qa.tones.push(true);return osc;};navigator.mediaDevices.getUserMedia=async()=>{window.__qa.mic++;throw Error('Click-only prop QA');};},{flat,mobile});
  const created=await c.request.post('/api/rooms',{data:{names:['小朋友','爸爸'],board:'cross',mode:'classic',props:true,demo:true,waiting:false}});assert.equal(created.status(),201);const initial=await created.json();
  const page=await c.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto('/play',{waitUntil:'networkidle'});await page.locator(flat?'.board-svg':'canvas').first().waitFor();
  const gpuBefore=await page.evaluate(()=>window.__qa.contexts);
  await page.getByRole('button',{name:/^道具口袋 ·/}).click();
  await page.locator('.prop-card').first().waitFor();assert.equal(await page.locator('.prop-card').count(),8);
  await page.getByRole('button',{name:'使用'+name,exact:true}).click();
  if(target)await page.locator('.prop-targets').getByRole('button',{name:new RegExp('^'+target)}).click();
  if(id==='luck'){await page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();await page.getByRole('button',{name:'继续飞行',exact:true}).click();}
  await page.locator(`[data-prop-fx="${id}"]`).waitFor();
  const room=(await(await c.request.get('/api/session')).json()).room;const s=room.state;
  assert.equal(s.players[0].props[id],0);assert.equal(s.players[0].stats.props_used,1);
  if(id==='luck'){assert.equal(s.players[0].stats.rolls,1);assert.equal(s.dice_rolls.length,2);assert.equal(s.dice,Math.max(...s.dice_rolls));}
  else assert.equal(s.players[0].stats.rolls,0);
  if(expected!==null){const owner=id==='bomb'||id==='boomerang'?1:0;assert.equal(s.players[owner].pieces[0].progress,expected);}
  if(id==='escort')assert.ok(s.players[0].pieces.slice(0,2).every(p=>p.shield));
  if(id==='shield')assert.ok(s.players[0].pieces[0].shield);
  if(id!=='luck'){await page.waitForTimeout(450);await page.screenshot({path:join(out,id+'.png')});}
  if(mobile)assert.equal(await page.locator('.prop-fx .fx-glyph').evaluate(el=>getComputedStyle(el).animationName),'none');
  await page.locator('.prop-fx').waitFor({state:'detached',timeout:7000});
  const qa=await page.evaluate(()=>window.__qa);assert.equal(qa.mic,0);assert.equal(qa.losses,0);assert.ok(qa.tones.length>0,'effects have actual oscillator cues');assert.equal(qa.contexts,gpuBefore,'prop effect must not create a WebGL renderer');
  await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('.prop-fx').count(),0,'reload must not replay old effects');
  report.push({id,flat,mobile,actual_dice:id==='luck'?s.dice_rolls:undefined,effects:s.history.filter(e=>e.prop_id===id).at(-1)?.effects,webgl_losses:qa.losses});await c.close();
 }
 const humans=[];
 for(let i=0;i<2;i++){const c=await browser.newContext({baseURL:base});await c.addInitScript(()=>{localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-experience',JSON.stringify({board:'flat',motion:'reduced'}));});humans.push(c);}
 const created=await humans[0].request.post('/api/rooms',{data:{names:['房主','朋友'],network:true,props:true,mode:'classic',board:'cross'}});assert.equal(created.status(),201);let room=await created.json();assert.equal((await humans[1].request.post(`/api/rooms/${room.id}/join`,{data:{invite:room.network.invite,name:'朋友'}})).status(),200);
 const pages=await Promise.all(humans.map(c=>c.newPage()));for(const p of pages){p.on('pageerror',e=>errors.push(e.message));await p.goto('/play',{waitUntil:'networkidle'});}
 await pages[1].getByRole('button',{name:'我准备好了',exact:true}).click();await pages[0].getByRole('button',{name:'开始游戏',exact:true}).click();
 const state=async()=>(await(await humans[0].request.get('/api/session')).json()).room;
 const cmd=async(actor,action,extra={})=>{room=await state();const body={command_id:crypto.randomUUID(),game_id:room.state.id,expected_version:room.state.version,action,...extra};const res=await humans[actor].request.post(`/api/rooms/${room.id}/commands`,{data:body});assert.equal(res.status(),200);return {room:await res.json(),body};};
 const pending=new Set(['boomerang','rescue','escort']);let steps=0;
 for(;steps<600&&pending.size;steps++){
  room=await state();const g=room.state,actor=g.current;if(g.phase==='finished')throw Error('game ended before prop checks');
  if(g.phase==='move'){await cmd(actor,'move',{piece_id:g.legal_moves[0].piece_id});continue;}
  const main=p=>p.progress>0&&p.progress<=g.board.ring_length;
  const prop=[...pending].find(id=>!g.active_prop&&g.players[actor].props[id]>0&&(id==='boomerang'?g.players.some((p,i)=>i!==actor&&p.pieces.some(main)):g.players[actor].pieces.some(p=>main(p)&&(id==='rescue'||!p.shield))));
  if(prop){
   for(const p of pages)await p.reload({waitUntil:'networkidle'});
   const owner=prop==='boomerang'?1-actor:actor,target=g.players[owner].pieces.find(p=>main(p)&&(prop!=='escort'||!p.shield));
   const denied=await humans[1-actor].request.post(`/api/rooms/${room.id}/commands`,{data:{command_id:crypto.randomUUID(),game_id:g.id,expected_version:g.version,action:'prop',prop_id:prop,piece_id:prop==='escort'?undefined:target.id}});assert.equal(denied.status(),403);
   const {room:next,body}=await cmd(actor,'prop',{prop_id:prop,piece_id:prop==='escort'?undefined:target.id});
   for(const p of pages)await p.locator(`[data-prop-fx="${prop}"]`).waitFor();
   const ids=await Promise.all(pages.map(p=>p.locator('.prop-fx').getAttribute('data-event-id')));assert.equal(ids[0],ids[1]);
   const other=(await(await humans[1-actor].request.get('/api/session')).json()).room;assert.deepEqual(other.state,next.state);
   const retry=await humans[actor].request.post(`/api/rooms/${room.id}/commands`,{data:body});assert.equal(retry.status(),200);assert.deepEqual((await retry.json()).state,next.state);
   report.push({id:prop,network:true,same_effect_id:true,wrong_seat_denied:true,idempotent_retry:true});pending.delete(prop);
  }
  await cmd(actor,'roll');
 }
 assert.equal(pending.size,0,'natural network game never reached prop targets');for(const c of humans)await c.close();
 assert.deepEqual(errors,[]);await writeFile(join(out,'results.json'),JSON.stringify({passed:true,scope:'Actual Chrome UI on official demo game setup; server commands and real random dice, no state injection; mobile is viewport only; no microphone',report,errors},null,2));console.log(JSON.stringify({passed:true,cases:report.length,errors}));
}finally{await browser.close();}
