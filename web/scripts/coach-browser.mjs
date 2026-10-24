import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL');
const output=process.env.QA_OUTPUT_DIR||'test-results/coach';await mkdir(output,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
try {
 const context=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width:1440,height:1120}});
 await context.addInitScript(()=>{localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');window.__mic=0;navigator.mediaDevices.getUserMedia=async()=>{window.__mic++;throw new Error('Click coach needs no microphone');};});
 const page=await context.newPage();page.setDefaultTimeout(12000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const room=async()=>(await(await page.request.get('/api/session')).json()).room;
 const command=async(action,extra={})=>{const r=await room();return page.request.post('/api/rooms/'+r.id+'/commands',{data:{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action,...extra}});};
 const preview=page.getByRole('region',{name:'飞行落点预览'});
 const clickCommand=async(run)=>{const response=page.waitForResponse(r=>r.url().endsWith('/commands')&&r.request().method()==='POST');await run();const res=await response;assert.equal(res.status(),200);await res.finished();return res.json();};
 const prepare=async(extra={},launch=false)=>{
  const r=await room();const res=await page.request.post('/api/rooms/'+r.id+'/matches',{data:{names:['小朋友','爸爸'],board:'cross',mode:'quick',ai:false,props:true,demo:true,waiting:false,...extra}});assert.equal(res.status(),200);
  if(launch)assert.equal((await command('prop',{prop_id:'launch'})).status(),200);
  assert.equal((await command('roll')).status(),200);
  const dice=page.getByRole('dialog',{name:'3D 掷骰结果'});await dice.waitFor({state:'visible'});await dice.getByRole('button',{name:'继续飞行',exact:true}).click();
  await page.getByRole('button',{name:'帮我选',exact:true}).waitFor({state:'visible'});await page.waitForFunction(()=>!document.querySelector('.coach-shortcut')?.disabled);
 };
 await page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});await prepare();
 const baseline=JSON.stringify((await room()).state);
 await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});
 let first=(await room()).coach;assert.ok(first);assert.equal(JSON.stringify((await room()).state),baseline);
 assert.equal(await preview.getAttribute('data-piece-id'),first.move.piece_id);await page.locator('.landing-marker:not([hidden])').waitFor({state:'visible'});
 await clickCommand(()=>preview.getByRole('button',{name:'另一架呢',exact:true}).click());let second=(await room()).coach;assert.notEqual(second.move.piece_id,first.move.piece_id);assert.equal(JSON.stringify((await room()).state),baseline);
 await clickCommand(()=>preview.getByRole('button',{name:'刚才那个',exact:true}).click());assert.equal((await room()).coach.move.piece_id,first.move.piece_id);
 await clickCommand(()=>preview.getByRole('button',{name:'安全一点',exact:true}).click());assert.equal(JSON.stringify((await room()).state),baseline);
 await preview.getByRole('button',{name:'确认飞行',exact:true}).waitFor({state:'visible'});await page.waitForFunction(()=>!document.querySelector('.move-preview .primary-button')?.disabled);
 await preview.screenshot({path:join(output,'coach-desktop.png')});
 // Same room, actual second browser tab: proposal changes synchronize.
 const tab=await context.newPage();await tab.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 const prior=(await room()).coach;await tab.getByRole('region',{name:'飞行落点预览'}).getByRole('button',{name:'另一架呢',exact:true}).click();
 const current=(await room()).coach;await page.waitForFunction(id=>document.querySelector('.move-preview')?.dataset.pieceId===id,current.move.piece_id);
 const stale=await command('confirm_coach',{coach_id:prior.id});assert.notEqual(stale.status(),200);assert.equal(JSON.stringify((await room()).state),baseline);
 await tab.close();await page.reload({waitUntil:'networkidle'});await preview.waitFor({state:'visible'});assert.equal((await room()).coach.id,current.id);
 // Choosing a plane manually cancels the server suggestion and keeps a local preview.
 await clickCommand(()=>page.getByRole('button',{name:current.move.label,exact:true}).click());await page.waitForFunction(()=>!document.querySelector('.coach-detail'));
 assert.equal((await room()).coach,undefined);assert.equal(JSON.stringify((await room()).state),baseline);
 await preview.getByRole('button',{name:'再想想',exact:true}).click();assert.equal(await preview.count(),0);
 await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});const selected=(await room()).coach;
 await preview.getByRole('button',{name:'确认飞行',exact:true}).click({clickCount:2});
 await page.waitForFunction(()=>!document.querySelector('.move-preview'));const moved=await room();assert.equal(moved.state.version,selected.version+1);assert.equal(moved.coach,undefined);
 assert.equal(moved.state.players.flatMap(p=>p.pieces).find(p=>p.id===selected.move.piece_id).progress,selected.move.to);
 console.log('PASS: advice/next/previous/safe leave game unchanged; shared-tab synchronization, stale confirmation, manual override, one actual move');
 // Pause/resume and new match invalidate the old proposal.
 await prepare();await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});const paused=(await room()).coach;
 assert.equal((await command('pause')).status(),200);await preview.waitFor({state:'detached'});assert.equal((await room()).coach,undefined);
 assert.equal((await command('resume')).status(),200);assert.notEqual((await command('confirm_coach',{coach_id:paused.id})).status(),200);
 await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});const replaced=(await room()).coach;
 await prepare();assert.equal((await room()).coach,undefined);assert.notEqual((await command('confirm_coach',{coach_id:replaced.id})).status(),200);
 await page.setViewportSize({width:390,height:844});await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>document.documentElement.clientWidth),false,'mobile page overflow');
 await page.waitForFunction(()=>!document.querySelector('.move-preview .primary-button')?.disabled);
 await page.screenshot({path:join(output,'coach-mobile.png'),fullPage:true});
 await clickCommand(()=>preview.getByRole('button',{name:'再想想',exact:true}).click());await preview.waitFor({state:'detached'});assert.equal((await room()).coach,undefined);
 await prepare({names:['小朋友','爸爸','妈妈','家人'],mode:'classic'},true);
 await page.getByRole('button',{name:'帮我选',exact:true}).click();await preview.waitFor({state:'visible'});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>document.documentElement.clientWidth),false,'four-plane mobile controls overflow');
 assert.equal(await page.evaluate(()=>window.__mic),0);assert.deepEqual(errors,[]);
 await writeFile(join(output,'results.json'),JSON.stringify({result:'PASS',scope:'actual Chrome + Go commands, natural random dice, no injected state, no microphone; click advice, comparison, stale/cancel/confirm, two tabs and mobile',errors},null,2));
 console.log('PASS: pause/resume, new game, cancel and mobile coach; no page errors or microphone requests');
} finally {await browser.close();}
