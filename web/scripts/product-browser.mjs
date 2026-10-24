import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const out=process.env.QA_OUTPUT_DIR||'test-results/product';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true});const errors=[],report=[];
try{
 for(const [name,width,height] of [['desktop',1440,1000],['mobile',390,844],['tablet',820,1180],['small',320,740]]){
  const c=await browser.newContext({baseURL:process.env.QA_BASE_URL,viewport:{width,height},reducedMotion:name==='small'?'reduce':'no-preference'});
  await c.addInitScript(()=>{window.__product={mic:0,gpu:0};navigator.mediaDevices.getUserMedia=async()=>{window.__product.mic++;throw Error('No microphone on product page');};const original=HTMLCanvasElement.prototype.getContext;HTMLCanvasElement.prototype.getContext=function(kind,...args){if(kind==='webgl'||kind==='webgl2')window.__product.gpu++;return original.call(this,kind,...args);};localStorage.setItem('fly-auto-voice','off');localStorage.setItem('fly-host-voice','off');localStorage.setItem('fly-tutorial-v1','skipped');});
  const p=await c.newPage(),api=[];p.on('pageerror',e=>errors.push(e.message));p.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/api/'))api.push(new URL(r.url()).pathname);});
  const response=await p.goto('/product',{waitUntil:'networkidle'});assert.equal(response.status(),200);
  await p.getByRole('heading',{level:1}).waitFor();assert.deepEqual(api,[],'product visit created/read a game session');
  assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'product page overflows at '+width);
  await p.getByRole('button',{name:'看护盾',exact:true}).click();await p.getByText('小熊号有了护盾，可以抵挡一次撞机或攻击。',{exact:true}).waitFor();
  await p.getByRole('button',{name:'看幸运骰',exact:true}).click();await p.getByText(/这次示意取 6 点/).waitFor();
  await p.getByRole('button',{name:/完整飞行/}).click();assert.equal(await p.locator('.product-preview-board [data-piece-id]').count(),16);
  await p.getByRole('button',{name:/轻快一局/}).click();assert.equal(await p.locator('.product-preview-board [data-piece-id]').count(),8);
  await p.getByRole('button',{name:'看起飞',exact:true}).click();
  const faq=p.locator('details').filter({has:p.getByText('必须开麦克风才能玩吗？',{exact:true})});await faq.locator('summary').click();assert.ok(await faq.evaluate(el=>el.open));
  await faq.locator('summary').focus();await p.keyboard.press('Space');assert.equal(await faq.evaluate(el=>el.open),false);await p.keyboard.press('Enter');assert.equal(await faq.evaluate(el=>el.open),true);
  const shield=p.getByRole('button',{name:'看护盾',exact:true});await shield.focus();await p.keyboard.press('Enter');assert.equal(await shield.getAttribute('aria-pressed'),'true');assert.ok(await shield.evaluate(el=>getComputedStyle(el).outlineStyle!=='none'));
  await p.getByRole('button',{name:'看起飞',exact:true}).click();
  if(name==='small')assert.equal(await p.locator('.product-preview-board').evaluate(el=>getComputedStyle(el).transform),'none');
  assert.deepEqual(await p.evaluate(()=>window.__product),{mic:0,gpu:0});assert.deepEqual(api,[]);
  await p.evaluate(()=>scrollTo(0,0));await p.screenshot({path:join(out,`product-${name}.png`),fullPage:true});
  if(name==='desktop')await p.screenshot({path:join(out,'product-hero.png')});
  if(name==='desktop'||name==='mobile'){
   await p.getByRole('button',{name:'创建游戏',exact:true}).click();await p.getByRole('dialog').first().waitFor({timeout:20000});assert.equal(new URL(p.url()).pathname,'/');
   await p.getByRole('dialog').getByRole('button',{name:'关闭',exact:true}).click();await p.getByRole('button',{name:'产品介绍',exact:true}).click();await p.getByRole('heading',{level:1}).waitFor();assert.equal(new URL(p.url()).pathname,'/product');
   await p.reload({waitUntil:'networkidle'});await p.getByRole('heading',{level:1}).waitFor();
  }
  report.push({viewport:name,width,height,interactive_preview:true,without_mic_or_webgl:true,no_game_requests:true,create_entry:name==='desktop'||name==='mobile'});await c.close();
 }
 assert.deepEqual(errors,[]);await writeFile(join(out,'results.json'),JSON.stringify({passed:true,report,errors,scope:'Actual desktop Chrome, responsive phone/tablet viewports, no physical phone claim'},null,2));console.log(JSON.stringify({passed:true,report,errors}));
}finally{await browser.close();}
