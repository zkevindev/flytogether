import { chromium } from 'playwright';
import assert from 'node:assert/strict';

const baseURL = process.env.QA_BASE_URL;
assert.ok(baseURL, 'Set QA_BASE_URL');
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const contexts = [];
const createContext = async viewport => {
  const context = await browser.newContext({ baseURL, viewport });
  contexts.push(context);
  await context.addInitScript(() => {
    localStorage.setItem('fly-auto-voice', 'off');
    localStorage.setItem('fly-host-voice', 'off');
    localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
  });
  return context;
};
const post = async (context, path, body, expected = 200) => {
  const response = await context.request.post(path, { data: body });
  const value = await response.json();
  assert.equal(response.status(), expected, JSON.stringify(value));
  return value;
};
const markers = async locator => locator.locator('.player-marker').evaluateAll(nodes => [...new Set(nodes.map(node => node.className.match(/player-marker-(circle|triangle|diamond|square)/)?.[1]).filter(Boolean))]);

try {
  const errors = [];
  const local = await createContext({ width: 1280, height: 960 });
  const localPage = await local.newPage();
  localPage.setDefaultTimeout(20000);
  localPage.on('pageerror', error => errors.push(error.message));
  await localPage.goto('/play', { waitUntil: 'networkidle' });
  let room = (await (await local.request.get('/api/session')).json()).room;
  room = await post(local, `/api/rooms/${room.id}/matches`, {
    names: ['小红', '小黄', '小蓝', '小绿'], board: 'cross', mode: 'classic', ai: false, props: false, waiting: false,
  });
  await localPage.reload({ waitUntil: 'networkidle' });
  await localPage.locator('.board-svg').waitFor();
  assert.deepEqual(await markers(localPage.locator('.plane-legend')), ['circle', 'triangle', 'diamond', 'square']);
  assert.deepEqual(await localPage.locator('.airport-player-label').evaluateAll(nodes => nodes.map(node => node.dataset.playerMarker)), ['circle', 'triangle', 'diamond', 'square']);
  assert.deepEqual(await localPage.locator('.player-shape-badge').evaluateAll(nodes => [...new Set(nodes.map(node => node.dataset.playerMarker))]), ['circle', 'triangle', 'diamond', 'square']);
  assert.deepEqual(await markers(localPage.locator('.crew-list')), ['circle', 'triangle', 'diamond', 'square']);
  assert.equal(await localPage.locator('.board-turnbar .player-marker').count(), 1);

  await localPage.getByRole('button', { name: '显示与操作设置' }).click();
  await localPage.getByRole('radio', { name: '自动立体' }).check();
  await localPage.getByRole('button', { name: '完成显示设置' }).click();
  await localPage.locator('.flight-host:not(.scene-hidden)').waitFor({ timeout: 20000 });
  await localPage.locator('.plane-marker i').first().waitFor();
  assert.equal(await localPage.locator('.plane-marker i').count(), 16);
  assert.deepEqual(await localPage.locator('.plane-marker').evaluateAll(nodes => [...new Set(nodes.map(node => node.dataset.playerMarker))]), ['circle', 'triangle', 'diamond', 'square']);
  const output = process.env.QA_OUTPUT_DIR || '/tmp/flytogether-player-markers-qa';
  await (await import('node:fs/promises')).mkdir(output, { recursive: true });
  await localPage.screenshot({ path: `${output}/four-player-3d.png`, fullPage: true });
  await localPage.setViewportSize({ width: 390, height: 844 });
  assert.deepEqual(await markers(localPage.locator('.plane-legend')), ['circle', 'triangle', 'diamond', 'square']);
  assert.ok(await localPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile board overflows horizontally');

  const host = await createContext({ width: 1280, height: 960 });
  const guest = await createContext({ width: 390, height: 844 });
  const watch = await createContext({ width: 1024, height: 768 });
  const inviteRoom = await post(host, '/api/rooms', { room_name: '色弱标记观赛验收', names: ['房主', '朋友'], board: 'cross', mode: 'quick', network: true, props: false }, 201);
  await post(guest, `/api/rooms/${inviteRoom.id}/join`, { invite: inviteRoom.network.invite, name: '朋友' }, 200);
  const hostPage = await host.newPage(), guestPage = await guest.newPage();
  const watchPage = await watch.newPage();
  for (const page of [hostPage, guestPage, watchPage]) { page.setDefaultTimeout(20000); page.on('pageerror', error => errors.push(error.message)); }
  await Promise.all([hostPage.goto('/play', { waitUntil: 'networkidle' }), guestPage.goto('/play', { waitUntil: 'networkidle' })]);
  const invite = await hostPage.getByRole('textbox', { name: '观赛链接', exact: true }).inputValue();
  await watchPage.goto(invite, { waitUntil: 'networkidle' });
  await watchPage.getByRole('textbox', { name: '你的昵称', exact: true }).fill('奶奶');
  await watchPage.getByRole('button', { name: '开始观赛', exact: true }).click();
  await watchPage.getByText('观赛中', { exact: true }).waitFor();
  assert.deepEqual(await markers(hostPage.locator('.plane-legend')), ['circle', 'triangle']);
  assert.deepEqual(await markers(watchPage.locator('.plane-legend')), ['circle', 'triangle']);
  assert.deepEqual(await markers(watchPage.locator('.crew-list')), ['circle', 'triangle']);
  await guestPage.getByRole('button', { name: '我准备好了', exact: true }).click();
  await hostPage.getByRole('button', { name: '开始游戏', exact: true }).click();
  await watchPage.getByText('等待开局', { exact: true }).waitFor({ state: 'detached' });
  assert.deepEqual(await markers(watchPage.locator('.board-turnbar')), ['circle']);
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ result: 'PASS', fourSeatMarkers: ['circle', 'triangle', 'diamond', 'square'], flat: true, threeD: true, spectatorMatchesPlayers: true, mobile: true, errors }));
} finally {
  await Promise.all(contexts.map(context => context.close()));
  await browser.close();
}
