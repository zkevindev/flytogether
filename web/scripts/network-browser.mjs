import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {join} from 'node:path';
assert.ok(process.env.QA_BASE_URL,'Set QA_BASE_URL');
const base=process.env.QA_BASE_URL,out=process.env.QA_OUTPUT_DIR||'test-results/network';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:process.platform==='darwin'?['--use-angle=metal']:[]});
const errors=[],contexts=[];const results=[];
const context=async(mobile=false)=>{const c=await browser.newContext({baseURL:base,viewport:mobile?{width:390,height:844}:{width:1360,height:1000}});contexts.push(c);await c.addInitScript(()=>{window.__networkMic=0;navigator.mediaDevices.getUserMedia=async()=>{window.__networkMic++;throw new Error('Click-only multiplayer QA must not request microphone');};localStorage.setItem('fly-tutorial-v1','skipped');localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');});return c;};
const page=async c=>{const p=await c.newPage();p.on('pageerror',e=>errors.push(e.message));return p;};
const session=async c=>(await(await c.request.get('/api/session')).json()).room;
const post=async(c,path,data)=>{const r=await c.request.post(path,{data});const v=await r.json();assert.equal(r.status(),200,JSON.stringify(v));return v;};
const command=async(c,action,extra={})=>{const r=await session(c);return post(c,`/api/rooms/${r.id}/commands`,{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action,...extra});};
const wait=async(fn,message)=>{for(let i=0;i<100;i++){if(await fn())return;await new Promise(r=>setTimeout(r,100));}throw Error(message);};
try {
 const h=await context(),g=await context(true),x=await context();let hp=await page(h),gp=await page(g);
 await hp.goto(base,{waitUntil:'networkidle'});await hp.getByRole('button',{name:'创建游戏',exact:true}).click();
 await hp.getByRole('checkbox',{name:/多人联网邀请房间/}).check();await hp.getByRole('checkbox',{name:/添加机器人陪玩/}).uncheck();await hp.getByRole('button',{name:'2 位',exact:true}).click();
 await hp.getByRole('textbox',{name:'第 1 位飞行员昵称'}).fill('房主');
 await hp.getByRole('button',{name:'创建邀请房间',exact:true}).click();
 await hp.getByRole('region',{name:'联网准备区'}).waitFor();let r=await session(h);assert.ok(r.network&&r.me.host);const roomID=r.id;
 assert.equal(r.status,'waiting');assert.ok(await hp.getByRole('button',{name:'开始游戏',exact:true}).isDisabled());
 const invite=new URL('/#join='+r.id+'.'+r.network.invite,base).href;
 const noAuth=await x.request.post(`/api/rooms/${roomID}/commands`,{data:{command_id:crypto.randomUUID(),game_id:r.state.id,expected_version:r.state.version,action:'roll'}});assert.equal(noAuth.status(),401);
 await gp.goto(invite,{waitUntil:'networkidle'});await gp.getByRole('textbox',{name:'你的昵称'}).fill('朋友');await gp.getByRole('button',{name:'加入房间',exact:true}).click();
 await gp.getByRole('region',{name:'联网准备区'}).waitFor();await gp.getByRole('button',{name:'我准备好了',exact:true}).click();
 await wait(async()=>!(await hp.getByRole('button',{name:'开始游戏',exact:true}).isDisabled()),'host start never enabled');
 await hp.screenshot({path:join(out,'lobby-desktop.png')});await hp.setViewportSize({width:390,height:844});assert.ok(await hp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'host mobile overflow');await hp.setViewportSize({width:1360,height:1000});await gp.screenshot({path:join(out,'lobby-mobile.png')});
 const denied=await g.request.post(`/api/rooms/${roomID}/commands`,{data:{command_id:crypto.randomUUID(),game_id:(await session(g)).state.id,expected_version:(await session(g)).state.version,action:'start'}});assert.equal((await denied.json()).code,'HOST_ONLY');
 await hp.getByRole('button',{name:'开始游戏',exact:true}).click();
 await wait(async()=>(await session(g)).status==='playing','start sync');
 await wait(async()=>await gp.getByRole('button',{name:'掷骰子',exact:true}).isDisabled(),'guest may roll other seat');
 const stale=await session(g);const illegal=await g.request.post(`/api/rooms/${roomID}/commands`,{data:{command_id:crypto.randomUUID(),game_id:stale.state.id,expected_version:stale.state.version,action:'roll'}});assert.equal((await illegal.json()).code,'NOT_YOUR_TURN');
 const before=await session(h),body={command_id:crypto.randomUUID(),game_id:before.state.id,expected_version:before.state.version,action:'roll'};
 await post(h,`/api/rooms/${roomID}/commands`,body);await post(h,`/api/rooms/${roomID}/commands`,body);
 assert.equal((await session(h)).state.players[0].stats.rolls,1);
 await hp.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();await gp.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();
 await hp.getByRole('button',{name:'继续飞行',exact:true}).click();await gp.getByRole('button',{name:'继续飞行',exact:true}).click();
 await gp.close();await wait(async()=>!(await session(h)).network.seats[1].online,'offline not visible');
 gp=await page(g);await gp.goto(new URL('/play',base).href,{waitUntil:'networkidle'});await gp.getByRole('region',{name:'联网准备区'}).waitFor();
 assert.equal((await session(g)).me.player_id,'player-1');await wait(async()=>(await session(h)).network.seats[1].online,'rejoin did not mark online');
 assert.equal((await session(g)).state.id,before.state.id);
 // Actual network loss: prevent commands until a fresh authoritative snapshot has arrived.
 await g.setOffline(true);await wait(async()=>await gp.getByText('正在同步棋局，连接恢复后才可操作。').isVisible(),'missing reconnect gate');
 await g.setOffline(false);await wait(async()=>!(await gp.getByText('正在同步棋局，连接恢复后才可操作。').isVisible()),'did not resync');
 results.push({case:'two isolated browsers',passed:true,checked:['invitation','ready','host-only start','seat-only roll','duplicate command counted once','both 3D popups','close/reopen original seat','offline/reconnect snapshot gate']});
 await hp.close();await gp.close();
 // Advance only through public random-roll and legal-move commands; no injected board/dice.
 let steps=0;
 for(;steps<1400;steps++){
  r=await session(h);const s=r.state;if(s.phase==='finished')break;
  if(s.phase==='roll'&&s.players[s.current].pieces.some(p=>p.progress>=s.board.finish-6))break;
  const actor=s.current===0?h:g;
  if(s.phase==='roll')await command(actor,'roll');else await command(actor,'move',{piece_id:[...s.legal_moves].sort((a,b)=>b.to-a.to)[0].piece_id});
 }
 hp=await page(h);gp=await page(g);await hp.goto(new URL('/play',base).href,{waitUntil:'networkidle'});await gp.goto(new URL('/play',base).href,{waitUntil:'networkidle'});
 for(;steps<1600;steps++){
  r=await session(h);if(r.state.phase==='finished')break;
  const actor=r.state.current===0?h:g;
  if(r.state.phase==='roll') {
   await command(actor,'roll');
   for(const p of [hp,gp]){await p.getByRole('dialog',{name:'3D 掷骰结果'}).waitFor();assert.equal(await p.getByRole('dialog',{name:'这次飞行，圆满抵达！'}).count(),0);await p.getByRole('button',{name:'继续飞行',exact:true}).click();}
   await new Promise(r=>setTimeout(r,1000));
  } else {await command(actor,'move',{piece_id:[...r.state.legal_moves].sort((a,b)=>b.to-a.to)[0].piece_id});await new Promise(r=>setTimeout(r,1000));}
 }
 r=await session(h);assert.equal(r.state.phase,'finished');assert.ok(r.state.players.reduce((a,p)=>a+p.stats.rolls,0)>12);
 await hp.getByRole('dialog',{name:'这次飞行，圆满抵达！'}).waitFor({timeout:15000});await gp.getByRole('dialog',{name:'这次飞行，圆满抵达！'}).waitFor({timeout:15000});
 await hp.screenshot({path:join(out,'settlement-desktop.png')});await gp.screenshot({path:join(out,'settlement-mobile.png')});
 const stats=r.state.players.map(p=>p.stats);await hp.reload({waitUntil:'networkidle'});await hp.getByRole('dialog',{name:'这次飞行，圆满抵达！'}).waitFor();assert.deepEqual((await session(h)).state.players.map(p=>p.stats),stats);
 await hp.getByRole('button',{name:'同配置再玩一局',exact:true}).click();await hp.getByRole('region',{name:'联网准备区'}).waitFor();
 await wait(async()=>(await session(g)).status==='waiting','replay sync');const replay=await session(h);assert.notEqual(replay.state.id,r.state.id);assert.equal(replay.id,roomID);assert.equal(replay.state.players[1].name,'朋友');assert.equal(replay.network.seats[1].ready,false);assert.equal(replay.network.seats[0].ready,true);for(const p of replay.state.players)assert.deepEqual(p.stats,{rolls:0,takeoffs:0,arrivals:0,captures:0,props_used:0});
 results.push({case:'natural victory, result, reload and replay',passed:true,random_game_commands:steps,stats,retained_room_and_seats:true});
 await gp.getByRole('button',{name:'我准备好了',exact:true}).waitFor();await gp.getByRole('button',{name:'退出房间',exact:true}).click();await wait(async()=>!(await session(h)).network.seats[1].occupied,'leave did not release seat');
 // Four real isolated sessions, distinct seats and personalized sockets.
 await hp.close();await gp.close();const created=await h.request.post('/api/rooms/new',{data:{names:['房主','第二位','第三位','第四位'],network:true,ai:false,board:'long',mode:'classic',props:true}});assert.equal(created.status(),201);r=await created.json();const four=[h,await context(),await context(),await context()],pages=[];
 for(let i=0;i<4;i++){
  if(i)await post(four[i],`/api/rooms/${r.id}/join`,{invite:r.network.invite,name:'队友'+i});
  const p=await page(four[i]);await p.goto(new URL('/play',base).href,{waitUntil:'networkidle'});pages.push(p);await p.getByRole('region',{name:'联网准备区'}).waitFor();if(i)await p.getByRole('button',{name:'我准备好了',exact:true}).click();
 }
 await wait(async()=>(await session(h)).network.seats.every(s=>s.online&&s.ready),'four players not ready');await command(h,'start');
 for(let i=0;i<4;i++){assert.equal((await session(four[i])).me.player_id,'player-'+i);assert.equal((await session(four[i])).state.players.length,4);}
 await command(h,'end');for(const p of pages)await wait(async()=>new URL(p.url()).pathname==='/','end did not return a client home');
 results.push({case:'four isolated browser seats',passed:true,checked:['four ready players','classic long board','personalized identities','host end returns everyone home']});
 for(const p of pages) await p.close();
 const threeCreate=await h.request.post('/api/rooms/new',{data:{names:['房主','第二位','第三位'],network:true,ai:false,board:'circle',mode:'quick',props:false}});assert.equal(threeCreate.status(),201);r=await threeCreate.json();
 for(let i=0;i<3;i++){if(i)await post(four[i],`/api/rooms/${r.id}/join`,{invite:r.network.invite,name:'伙伴'+i});const p=await page(four[i]);await p.goto(new URL('/play',base).href,{waitUntil:'networkidle'});await p.getByRole('region',{name:'联网准备区'}).waitFor();if(i)await p.getByRole('button',{name:'我准备好了',exact:true}).click();}
 await wait(async()=>(await session(h)).network.seats.every(s=>s.online&&s.ready),'three players not ready');await command(h,'start');assert.equal((await session(h)).state.players.length,3);
 results.push({case:'three isolated browser seats',passed:true,checked:['circle board','no props','ready and start']});
 for(const c of contexts)for(const p of c.pages())assert.equal(await p.evaluate(()=>window.__networkMic),0);assert.deepEqual(errors,[]);await writeFile(join(out,'results.json'),JSON.stringify({results,console_errors:errors,credentials_recorded:false},null,2));console.log(JSON.stringify({passed:true,results,console_errors:errors}));
}finally{await browser.close();}
