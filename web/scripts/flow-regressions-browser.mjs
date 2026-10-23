import { chromium } from 'playwright';
import assert from 'node:assert/strict';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL');
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream',...(process.platform==='darwin'?['--use-angle=metal']:[])]});
try{
 const page=await browser.newPage({baseURL:process.env.QA_BASE_URL,permissions:['microphone'],viewport:{width:1440,height:1120}});page.setDefaultTimeout(15000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>{
  window.__qa={mic:0,tracks:[]};const get=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
  navigator.mediaDevices.getUserMedia=async options=>{window.__qa.mic++;const stream=await get(options);window.__qa.tracks.push(...stream.getTracks());return stream;};
 });
 let connections=0,announcements=0;
 await page.routeWebSocket('**/ws/voice?*',socket=>{connections++;socket.send(JSON.stringify({type:'hello'}));socket.send(JSON.stringify({type:'wake_state',wake_enabled:false,awake:true}));});
 await page.route('**/api/rooms/*/announcement',route=>{announcements++;return route.fulfill({status:503,json:{message:'controlled narration unavailable'}});});
 await page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 let room=(await(await page.request.get('/api/session')).json()).room;
 const options={names:['小朋友','爸爸'],board:'cross',mode:'quick',ai:false,props:true,waiting:false};
 await page.request.post('/api/rooms/'+room.id+'/matches',{data:options});
 const guide=page.getByRole('dialog',{name:'新手飞行指南'});await guide.waitFor({state:'visible'});await page.waitForTimeout(400);
 assert.equal(await page.evaluate(()=>window.__qa.mic),0);assert.equal(connections,0);assert.equal(announcements,0);
 await page.reload({waitUntil:'networkidle'});await guide.waitFor({state:'visible'});assert.equal(connections,0);assert.equal(announcements,0);assert.equal(await page.evaluate(()=>window.__qa.mic),0);
 await guide.getByRole('button',{name:'跳过引导',exact:true}).click();await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({state:'visible'});assert.equal(connections,1);
 await page.getByRole('button',{name:'新手指南',exact:true}).click();await guide.waitFor({state:'visible'});
 await page.waitForFunction(()=>window.__qa.tracks.every(t=>t.readyState==='ended'));const count=await page.evaluate(()=>window.__qa.mic);await page.waitForTimeout(400);assert.equal(await page.evaluate(()=>window.__qa.mic),count);assert.equal(announcements,0);
 await guide.getByRole('button',{name:'下一步',exact:true}).click();await guide.getByRole('button',{name:'下一步',exact:true}).click();await guide.getByRole('button',{name:'开始玩',exact:true}).click();
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).waitFor({state:'visible'});assert.equal(connections,2);
 await page.getByRole('button',{name:'关闭对讲麦克风',exact:true}).click();await page.getByRole('button',{name:'新手指南',exact:true}).click();await guide.waitFor({state:'visible'});
 const announcementsBefore=announcements;await page.waitForTimeout(500);assert.equal(announcements,announcementsBefore);
 await guide.getByRole('button',{name:'跳过引导',exact:true}).click();await page.waitForTimeout(500);assert.equal(connections,2,'closing the guide respects microphone-off preference');
 console.log('PASS: first guide/reload/reopen stays silent without microphone or narration; skip/complete connects once, manual off persists');

 let rejectEnd=true;await page.route('**/api/rooms/*/commands',route=>{if(rejectEnd&&route.request().postDataJSON().action==='end')return route.fulfill({status:503,json:{message:'controlled end failure'}});return route.continue();});
 const end=async()=>{await page.getByRole('button',{name:'结束游戏',exact:true}).click();await page.getByRole('dialog',{name:'结束这一局？'}).getByRole('button',{name:'结束游戏',exact:true}).click();};
 await end();await page.getByText('controlled end failure',{exact:true}).waitFor({state:'visible'});assert.equal(new URL(page.url()).pathname,'/play');assert.equal((await(await page.request.get('/api/session')).json()).room.status,'playing');
 rejectEnd=false;await end();await page.waitForURL(new URL('/',process.env.QA_BASE_URL).href);assert.equal((await(await page.request.get('/api/session')).json()).room.status,'ended');assert.equal(await page.getByRole('button',{name:/继续当前游戏/}).count(),0);
 await page.reload({waitUntil:'networkidle'});assert.equal(new URL(page.url()).pathname,'/');assert.equal(await page.locator('.flight-host').count(),0);
 console.log('PASS: end failure stays on board; successful end returns home, survives refresh and removes ended-game continue');

 room=(await(await page.request.get('/api/session')).json()).room;
 await page.goto(new URL('/play',process.env.QA_BASE_URL).href,{waitUntil:'networkidle'});
 const fresh=await page.request.post('/api/rooms/'+room.id+'/matches',{data:{...options,demo:true}});assert.equal(fresh.status(),200);
 const before=(await(await page.request.get('/api/session')).json()).room.state;
 await page.getByRole('button',{name:/道具口袋/}).click();await page.getByRole('button',{name:'使用幸运骰',exact:true}).click();
 const dice=page.getByRole('dialog',{name:'3D 掷骰结果'});await dice.waitFor({state:'visible'});assert.equal(await dice.locator('.dice-cube').count(),2);
 const after=(await(await page.request.get('/api/session')).json()).room.state;const actual=await dice.locator('.dice-cube').evaluateAll(nodes=>nodes.map(n=>Number(n.dataset.value)));
 assert.deepEqual(actual,after.dice_rolls);assert.equal(after.dice,Math.max(...actual));assert.equal(after.players[0].props.luck,0);assert.equal(after.version,before.version+2);
 console.log(JSON.stringify({result:'PASS',case:'one click uses lucky dice and actually throws two dice',dice:actual,version_before:before.version,version_after:after.version}));
 assert.deepEqual(errors,[]);
}finally{await browser.close();}
