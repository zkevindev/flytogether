import { chromium } from 'playwright';
import assert from 'node:assert/strict';

assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const base = process.env.QA_BASE_URL;
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const context = await browser.newContext({ baseURL: base, viewport: { width: 1360, height: 900 } });
context.setDefaultTimeout(10000);
await context.addInitScript(() => {
  localStorage.setItem('fly-tutorial-v1', 'skipped');
  localStorage.setItem('fly-auto-voice', 'off');
  localStorage.setItem('fly-host-voice', 'off');
  navigator.mediaDevices.getUserMedia = async () => { throw new Error('Lifecycle simulation must not request a microphone'); };
});
const errors = [];
const page = await context.newPage();
page.on('pageerror', error => errors.push(error.message));
const snapshot = async () => (await (await context.request.get('/api/session')).json()).room;
const command = async (room, action) => {
  const response = await context.request.post(`/api/rooms/${room.id}/commands`, {
    data: { command_id: crypto.randomUUID(), game_id: room.state.id, expected_version: room.state.version, action },
  });
  const body = await response.json();
  assert.equal(response.status(), 200, JSON.stringify(body));
  return body;
};

try {
  const created = await context.request.post('/api/rooms', { data: { names: ['后台测试甲', '后台测试乙'], board: 'cross', mode: 'quick', props: false, waiting: false } });
  assert.equal(created.status(), 201, await created.text());
  const roomID = (await created.json()).id;
  await page.goto(new URL('/play', base).href, { waitUntil: 'networkidle' });
  await page.getByText('本局规则').waitFor();
  let room = await snapshot();
  assert.equal(room.id, roomID);

  // Simulate the Page Visibility API signal; this does not suspend the browser process or OS app.
  await page.evaluate(() => {
    let state = 'hidden';
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => state === 'hidden' });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  assert.deepEqual(await page.evaluate(() => ({ hidden: document.hidden, visibility: document.visibilityState })), { hidden: true, visibility: 'hidden' });
  const rolled = await command(room, 'roll');
  assert.ok(rolled.state.version > room.state.version);
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.getByRole('dialog', { name: '3D 掷骰结果' }).waitFor();
  room = await snapshot();
  assert.equal(room.state.version, rolled.state.version, 'the returning page must show the authoritative hidden-period move');
  await page.getByRole('button', { name: '继续飞行', exact: true }).click();

  // Simulate a connectivity gap and return; the page must lock writes until its socket is back.
  await context.setOffline(true);
  await page.getByText('正在同步棋局，连接恢复后才可操作。').waitFor();
  await context.setOffline(false);
  await page.getByText('正在同步棋局，连接恢复后才可操作。').waitFor({ state: 'hidden' });
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ passed: true, visibility: 'Page Visibility API hidden → visible event simulation', backgroundGameAction: 'authoritative roll restored after foreground', connectivity: 'offline → online resync gate', pageErrors: errors, realOSBackgroundAndCarrierHandoffTested: false }));
} finally {
  await browser.close();
}
