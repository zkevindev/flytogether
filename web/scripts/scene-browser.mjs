import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL to the application under test');
const output = process.env.QA_OUTPUT_DIR || 'test-results/scene';
await mkdir(output, { recursive: true });
const browser=await chromium.launch({channel:process.env.QA_BROWSER_CHANNEL || 'chrome',headless:true,args:[...(process.platform==='darwin'?['--use-angle=metal']:[]),'--autoplay-policy=no-user-gesture-required']});
const page=await browser.newPage({baseURL:process.env.QA_BASE_URL,viewport:{width:1280,height:1100}});
try {
const errors=[]; page.on('pageerror',e=>errors.push(e.message));
await page.clock.install(); await page.clock.pauseAt(new Date(Date.now()+500));
await page.addInitScript(()=>{
 localStorage.setItem('fly-auto-voice','off');
 const seen=new WeakSet();window.__gpu={contexts:[],losses:0};
 const original=HTMLCanvasElement.prototype.getContext;
 HTMLCanvasElement.prototype.getContext=function(...args){
  const gl=original.apply(this,args);
  if(gl && ['webgl','webgl2'].includes(args[0]) && !seen.has(gl)) {
   seen.add(gl);const record={gl,resources:{}};window.__gpu.contexts.push(record);
   for(const name of ['Buffer','Texture','Program','Framebuffer','Renderbuffer']){
    const active=new Set();record.resources[name]=active;
    const create=gl['create'+name].bind(gl),del=gl['delete'+name].bind(gl);
    gl['create'+name]=(...args)=>{const value=create(...args);if(value)active.add(value);return value;};
    gl['delete'+name]=(value)=>{active.delete(value);return del(value);};
   }
   this.addEventListener('webglcontextlost',()=>{window.__gpu.losses++;for(const active of Object.values(record.resources))active.clear();});
  }
  return gl;
 };
});
const stats=()=>page.evaluate(()=>({allocated:window.__gpu.contexts.length,live:window.__gpu.contexts.filter(r=>!r.gl.isContextLost()).length,losses:window.__gpu.losses,resources:window.__gpu.contexts.filter(r=>!r.gl.isContextLost()).map(r=>Object.fromEntries(Object.entries(r.resources).map(([k,v])=>[k,v.size])))}));
await page.goto(process.env.QA_BASE_URL,{waitUntil:'networkidle'});await page.clock.runFor(50);
const boards=(await (await page.request.get('/api/config')).json()).boards;
const opts={names:['小朋友','爸爸','妈妈','爷爷'],board:'cross',mode:'classic',ai:false,demo:false,props:true,waiting:false};
let room;
const restart=async extra=>{
 const session=await (await page.request.get('/api/session')).json();
 const res=await page.request.post('/api/rooms/'+session.room.id+'/matches',{data:{...opts,...extra}});assert.equal(res.status(),200);room=await res.json();
 await page.clock.runFor(80);
 await page.waitForFunction(id=>document.querySelector('.flight-board')?.dataset.gameId===id,room.state.id,{polling:50});
 await page.clock.runFor(50);
};
await restart(); await page.clock.runFor(100);
await page.locator('.flight-host').screenshot({path:join(output, 'board-desktop.png')});
const markers=await page.locator('.plane-marker').evaluateAll(nodes=>nodes.map(node=>({text:node.textContent,title:node.title,rect:node.getBoundingClientRect().toJSON()})));
assert.equal(markers.length,16);for(const m of markers) assert.match(m.title,/[1-4]号/);
console.log('baseline',JSON.stringify(await stats())); const baseline=await stats();
for(let i=0;i<30;i++){
 await restart({board:boards[i%boards.length].id});
 assert.ok(await page.locator('.roll-button').isEnabled(), 'roll button is ready');
 await page.locator('.roll-button').click({force:true});
 const dialog=page.getByRole('dialog',{name:'3D 掷骰结果'});await dialog.waitFor({state:'visible'});
 assert.equal(await page.locator('.dice-cube').count(),1);
 if(i===0) { await page.clock.runFor(1000); await dialog.screenshot({path:join(output, 'dice-css3d.png'),animations:'disabled'}); }
 await page.getByRole('button',{name:'继续飞行',exact:true}).click({force:true});
 await page.clock.runFor(500);
 if((i+1)%10===0) console.log('after throws',i+1,JSON.stringify(await stats()));
}
await restart();await page.clock.runFor(100);
const after=await stats();assert.equal(after.allocated,baseline.allocated,'throws must not allocate more WebGL contexts');assert.equal(after.losses,baseline.losses,'no unsolicited context loss');assert.deepEqual(after.resources,baseline.resources,'same board must not accumulate GPU resources');
await page.setViewportSize({width:390,height:844});await page.clock.runFor(100);
await page.locator('.flight-board').screenshot({path:join(output, 'board-mobile.png')});
const inside=await page.locator('.flight-host').evaluate(el=>{const box=el.getBoundingClientRect();return [...el.querySelectorAll('.plane-marker b')].every(n=>{const r=n.getBoundingClientRect();return r.left>=box.left && r.right<=box.right && r.top>=box.top && r.bottom<=box.bottom;});});assert.ok(inside,'every plane number remains inside mobile viewport');
// A real GPU context loss event, not a mocked React error.
const beforeRoom=(await (await page.request.get('/api/session')).json()).room;
await page.evaluate(()=>{ window.__lossExtension = window.__gpu.contexts.at(-1).gl.getExtension('WEBGL_lose_context'); window.__lossExtension.loseContext(); });
await page.getByText('3D 画面暂时中断',{exact:false}).waitFor({state:'visible'});
assert.equal(await page.locator('.scene-recovery .board-svg').count(),1);
await page.locator('.scene-recovery').screenshot({path:join(output, 'context-fallback.png')});
// Keep playing in the fallback, then restore automatically with the standard extension.
await page.locator('.scene-recovery .board-dice').click({force:true});await page.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor({state:'visible'});
await page.getByRole('button',{name:'继续飞行',exact:true}).click({force:true});await page.clock.runFor(100);
const progressed=(await (await page.request.get('/api/session')).json()).room;assert.equal(progressed.state.id,beforeRoom.state.id);assert.ok(progressed.state.version>beforeRoom.state.version);
await page.evaluate(()=>window.__lossExtension.restoreContext());
await page.locator('.flight-host').waitFor({state:'visible'});await page.clock.runFor(100);
assert.equal(await page.locator('.scene-recovery').count(),0);
const restored=(await (await page.request.get('/api/session')).json()).room;assert.equal(restored.state.id,progressed.state.id);assert.equal(restored.state.version,progressed.state.version);
console.log('restored',JSON.stringify(await stats()));
// Re-loss and manual renderer replacement should also preserve game state.
await page.evaluate(()=>{ window.__lossExtension = window.__gpu.contexts.at(-1).gl.getExtension('WEBGL_lose_context'); window.__lossExtension.loseContext(); });
await page.getByRole('button',{name:'恢复 3D 棋盘'}).waitFor({state:'visible'});
await page.getByRole('button',{name:'恢复 3D 棋盘'}).click({force:true});await page.clock.runFor(100);
await page.locator('.flight-host').waitFor({state:'visible'});
assert.equal((await stats()).live,1);assert.equal((await (await page.request.get('/api/session')).json()).room.state.id,progressed.state.id);
assert.deepEqual(errors,[]);
const noGPU=await browser.newPage({baseURL:process.env.QA_BASE_URL});
await noGPU.clock.install(); await noGPU.clock.pauseAt(new Date(Date.now()+500));
await noGPU.addInitScript(()=>{
 localStorage.setItem('fly-auto-voice','off');
 const original=HTMLCanvasElement.prototype.getContext;
 HTMLCanvasElement.prototype.getContext=function(type,...args){return ['webgl','webgl2'].includes(type)?null:original.call(this,type,...args);};
});
noGPU.on('pageerror',e=>errors.push(e.message));
await noGPU.goto(process.env.QA_BASE_URL,{waitUntil:'networkidle'});
await noGPU.getByText('当前浏览器暂时无法显示 3D',{exact:false}).waitFor({state:'visible'});
const noGPUSession=await (await noGPU.request.get('/api/session')).json();
const noGPUMatch=await noGPU.request.post('/api/rooms/'+noGPUSession.room.id+'/matches',{data:opts});assert.equal(noGPUMatch.status(),200);
const noGPURoom=await noGPUMatch.json();
await noGPU.waitForFunction(id=>document.querySelector('.flight-board')?.dataset.gameId===id,noGPURoom.state.id,{polling:50});
await noGPU.locator('.roll-button').click({force:true});
await noGPU.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor({state:'visible'});
assert.equal(await noGPU.locator('.dice-cube').count(),1);
await noGPU.getByRole('button',{name:'继续飞行',exact:true}).click({force:true});
await noGPU.clock.runFor(500);
assert.ok((await (await noGPU.request.get('/api/session')).json()).room.state.version>noGPURoom.state.version);
assert.deepEqual(errors,[]);await noGPU.close();
console.log('PASS: 30 throws, board switches, mobile labels, fallback gameplay, automatic/manual restore, unavailable WebGL');
} finally { await browser.close(); }
