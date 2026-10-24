import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const output=process.env.QA_OUTPUT_DIR||'test-results/onboarding';await mkdir(output,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const contexts=[],errors=[],results=[];
const options={names:['小朋友','爸爸'],board:'cross',mode:'classic',ai:false,props:true,demo:true,waiting:false};
const setup=async({seen=true,noGPU=false}={})=>{
 const context=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width:1440,height:1120}});contexts.push(context);
 await context.addInitScript(({seen,noGPU})=>{
  localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');
  if(seen)localStorage.setItem('fly-tutorial-v1','completed');
  navigator.mediaDevices.getUserMedia=async()=>{throw new Error('No microphone needed for onboarding QA');};
  if(noGPU){const get=HTMLCanvasElement.prototype.getContext;HTMLCanvasElement.prototype.getContext=function(type,...args){return ['webgl','webgl2'].includes(type)?null:get.call(this,type,...args);};}
 },{seen,noGPU});
 const page=await context.newPage();page.setDefaultTimeout(15000);page.setDefaultNavigationTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
 let room;
 const state=async()=>{room=(await(await context.request.get('/api/session')).json()).room;return room.state;};
 const create=async extra=>{const response=await context.request.post('/api/rooms',{data:{...options,...extra}});assert.equal(response.status(),201);room=await response.json();return room.state;};
 const restart=async()=>{const response=await context.request.post('/api/rooms/'+room.id+'/matches',{data:options});assert.equal(response.status(),200);room=await response.json();return room.state;};
 const command=async(action,extra={})=>{await state();const response=await context.request.post('/api/rooms/'+room.id+'/commands',{data:{command_id:crypto.randomUUID(),game_id:room.state.id,expected_version:room.state.version,action,...extra}});assert.equal(response.status(),200);room=await response.json();return room.state;};
 return {context,page,state,create,restart,command};
};
const log=result=>{results.push(result);console.log(JSON.stringify(result));};
try{
 const t=await setup({seen:false});await t.page.goto(process.env.QA_BASE_URL,{waitUntil:'networkidle'});
 assert.equal(await t.page.getByRole('dialog',{name:'新手飞行指南'}).count(),0,'home does not force a tutorial');
 await t.page.getByRole('button',{name:'创建游戏',exact:true}).click();const form=t.page.getByRole('dialog',{name:'创建家庭房间'});
 await form.getByRole('checkbox',{name:/添加机器人陪玩/}).uncheck();await form.getByRole('button',{name:'2 位',exact:true}).click();
 await form.getByRole('button',{name:'创建并开始游戏',exact:true}).click();
 console.log('QA: room created, waiting for first guide');
 const guide=t.page.getByRole('dialog',{name:'新手飞行指南'});await guide.waitFor({state:'visible'});const initial=await t.state();
 assert.ok(initial.version>=1);await guide.getByRole('button',{name:'下一步',exact:true}).click();
 await guide.getByRole('heading',{name:'先看落点，再决定怎么飞'}).waitFor({state:'visible'});
 console.log('QA: tutorial step two visible');
 await guide.screenshot({path:join(output,'tutorial.png'),timeout:10000});
 console.log('QA: tutorial screenshot saved');
 await guide.getByRole('button',{name:'上一步',exact:true}).click();await guide.getByRole('button',{name:'下一步',exact:true}).click();
 await guide.getByRole('button',{name:'下一步',exact:true}).click();await guide.getByRole('button',{name:'开始玩',exact:true}).click();
 assert.equal((await t.state()).version,initial.version);assert.equal(await t.page.evaluate(()=>localStorage.getItem('fly-tutorial-v1')),'completed');
 await t.page.reload({waitUntil:'networkidle'});assert.equal(await t.page.getByRole('dialog',{name:'新手飞行指南'}).count(),0);
 await t.page.getByRole('button',{name:'新手指南',exact:true}).click();await guide.waitFor({state:'visible'});
 await t.page.keyboard.press('Escape');await guide.waitFor({state:'hidden'});assert.equal((await t.state()).version,initial.version);
 log({result:'PASS',case:'first-play guide, steps/back/complete, refresh preference, manual reopen and Escape; guide never writes game'});
 await t.page.getByRole('button',{name:/道具口袋/}).click();await t.page.getByRole('button',{name:'使用起飞卡',exact:true}).click();
 await t.page.locator('.roll-button').click();await t.page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor({state:'visible'});
 assert.equal(await t.page.getByRole('region',{name:'飞行落点预览'}).count(),0);
 await t.page.getByRole('button',{name:'继续飞行',exact:true}).click();
 const before=await t.state();await t.page.getByRole('button',{name:'小熊号',exact:true}).click();
 const preview=t.page.getByRole('region',{name:'飞行落点预览'});await preview.waitFor({state:'visible'});
 assert.equal(await preview.getAttribute('data-to'),'0');assert.equal((await t.state()).version,before.version);
 await t.page.locator('.landing-marker:not([hidden])').waitFor({state:'visible'});
 await t.page.getByRole('button',{name:'兔子号',exact:true}).click();assert.equal(await preview.getAttribute('data-piece-id'),before.players[0].pieces[1].id);
 await preview.getByRole('button',{name:'再想想',exact:true}).click();await preview.waitFor({state:'hidden'});assert.equal((await t.state()).version,before.version);
 await t.page.getByRole('button',{name:'小熊号',exact:true}).click();
 let moves=0;t.page.on('request',r=>{if(r.url().endsWith('/commands')&&r.postDataJSON()?.action==='move')moves++;});
 await preview.getByRole('button',{name:'确认飞行',exact:true}).evaluate(b=>{b.click();b.click();});
 await t.page.waitForFunction(()=>!document.querySelector('.roll-button')?.disabled);
 const after=await t.state();assert.equal(after.version,before.version+1);assert.equal(after.players[0].pieces[0].progress,0);assert.equal(moves,1);
 await preview.waitFor({state:'hidden'});
 log({result:'PASS',case:'real random launch: preview/switch/cancel no write, confirm twice sends one move',dice:before.dice,version_before:before.version,version_after:after.version});
 await t.context.close();

 // Prepare special outcomes exclusively via real server dice/actions; no position injection.
 for(const kind of ['jump','fly','capture','finish','blocked']){
  const item=await setup();await item.create();let found,steps=0;
  for(;steps<1800;steps++){
   let s=await item.state();if(s.phase==='finished'){await item.restart();continue;}
   if(s.phase==='roll'){
    const player=s.players[s.current];
    const shieldTarget=kind==='blocked'&&player.props.shield>0&&!s.active_prop&&player.pieces.find(p=>p.progress>0&&p.progress<=s.board.ring_length&&!p.shield);
    if(shieldTarget)await item.command('prop',{prop_id:'shield',piece_id:shieldTarget.id});
    await item.command('roll');continue;
   }
   found=s.legal_moves.find(m=>kind==='capture'?m.captured.length>0:kind==='blocked'?(m.blocked||[]).length>0:kind==='finish'?m.finish:m[kind]);
   if(found)break;
   const player=s.players[s.current];
   const pick=[...s.legal_moves].sort((a,b)=>Number(!!player.pieces.find(p=>p.id===a.piece_id)?.shield)-Number(!!player.pieces.find(p=>p.id===b.piece_id)?.shield)||b.to-a.to)[0];
   await item.command('move',{piece_id:pick.piece_id});
  }
  assert.ok(found,'could not naturally prepare '+kind);
  await item.page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
  await item.page.getByRole('button',{name:found.label,exact:true}).click();const box=item.page.getByRole('region',{name:'飞行落点预览'});await box.waitFor({state:'visible'});
  const original=await item.state();assert.equal(Number(await box.getAttribute('data-to')),found.to);assert.equal(await box.locator('.preview-summary').innerText(),found.summary);
  if(kind==='blocked'){await box.getByText(/击破护盾/).waitFor({state:'visible'});assert.equal(found.captured.length,0);}
  await item.page.locator('.landing-marker:not([hidden])').waitFor({state:'visible'});
  if(kind==='jump')await item.page.screenshot({path:join(output,'preview-desktop.png'),fullPage:true});
  if(kind==='capture'){
   await item.page.setViewportSize({width:390,height:844});
   await item.page.screenshot({path:join(output,'preview-mobile.png'),fullPage:true});
   const dims=await item.page.evaluate(()=>{const b=document.querySelector('.move-preview').getBoundingClientRect();return {left:b.left,right:b.right,bottom:b.bottom,width:innerWidth,height:innerHeight,scroll:document.documentElement.scrollWidth};});
   assert.ok(dims.left>=0&&dims.right<=dims.width&&dims.bottom<=dims.height);assert.ok(dims.scroll<=dims.width);
  }
  if(kind==='jump'){
   await item.command('pause');await box.waitFor({state:'hidden'});await item.command('resume');assert.equal(await box.count(),0,'pause/resume discards a selection');
   await item.page.getByRole('button',{name:found.label,exact:true}).click();await box.waitFor({state:'visible'});
   // An action from another input path invalidates the old preview immediately.
   await item.command('move',{piece_id:found.piece_id});await box.waitFor({state:'hidden'});
   let next=await item.state();while(next.phase!=='move')next=await item.command('roll');
   await item.page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor({state:'visible'});
   await item.page.getByRole('button',{name:'继续飞行',exact:true}).click();
   await item.page.getByRole('button',{name:next.legal_moves[0].label,exact:true}).click();await box.waitFor({state:'visible'});
   const replacement=await item.restart();await box.waitFor({state:'hidden'});
   await item.page.waitForFunction(id=>document.querySelector('.flight-board')?.dataset.gameId===id,replacement.id);
   assert.notEqual(replacement.id,next.id,'a new match must discard the old selection');
  }else{
   await box.getByRole('button',{name:'确认飞行',exact:true}).click();
   await item.page.waitForFunction(v=>document.querySelector('.flight-board')?.dataset.version===String(v),original.version+1);
   const next=await item.state();assert.equal(next.players.flatMap(p=>p.pieces).find(p=>p.id===found.piece_id).progress,found.to);
   for(const id of found.captured)assert.equal(next.players.flatMap(p=>p.pieces).find(p=>p.id===id).progress,-1);
   for(const id of found.blocked||[])assert.equal(next.players.flatMap(p=>p.pieces).find(p=>p.id===id).shield||false,false);
  }
  log({result:'PASS',case:'authoritative '+kind+' preview',preparation_commands:steps,to:found.to,summary:found.summary});await item.context.close();
 }
 const flat=await setup({noGPU:true,seen:false});let s=await flat.create();
 while(s.phase!=='move')s=await flat.command('roll');
 await flat.page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 await flat.page.getByRole('button',{name:'跳过引导',exact:true}).click();
 assert.equal(await flat.page.evaluate(()=>localStorage.getItem('fly-tutorial-v1')),'skipped');
 await flat.page.getByRole('button',{name:s.legal_moves[0].label,exact:true}).click();
 await flat.page.locator('.flat-preview').waitFor({state:'visible'});const version=s.version;
 await flat.page.getByRole('button',{name:'确认飞行',exact:true}).click();
 await flat.page.waitForFunction(v=>document.querySelector('.flight-board')?.dataset.version===String(v),version+1);
 log({result:'PASS',case:'skip preference and actual 2D fallback landing preview/confirmation'});
 await flat.context.close();assert.deepEqual(errors,[]);
 await writeFile(join(output,'results.json'),JSON.stringify({results,errors},null,2)+'\n');
}finally{await browser.close();}
