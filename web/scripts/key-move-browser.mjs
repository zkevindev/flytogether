import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';

const baseURL = process.env.QA_BASE_URL;
assert.ok(baseURL, 'Set QA_BASE_URL');
const browser = await chromium.launch({ channel: 'chrome', headless: true });
const contexts = [];
const makeContext = async viewport => {
  const context = await browser.newContext({ baseURL, viewport });
  contexts.push(context);
  await context.addInitScript(() => {
    localStorage.setItem('fly-auto-voice', 'off');
    localStorage.setItem('fly-host-voice', 'off');
    localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
    window.__qaMic = 0;
    navigator.mediaDevices.getUserMedia = async () => { window.__qaMic++; throw new DOMException('not requested', 'NotAllowedError'); };
  });
  return context;
};
const post = async (context, path, body, status = 200) => {
  const response = await context.request.post(path, { data: body });
  const value = await response.json();
  assert.equal(response.status(), status, JSON.stringify(value));
  return value;
};
const command = async (context, room, action, extra = {}) => post(context, `/api/rooms/${room.id}/commands`, {
  command_id: crypto.randomUUID(), game_id: room.state.id, expected_version: room.state.version, action, ...extra,
});

try {
  const host = await makeContext({ width: 1280, height: 960 });
  const guest = await makeContext({ width: 390, height: 844 });
  const hostRoom = await post(host, '/api/rooms', {
    room_name: '棋步说明验收', names: ['小朋友', '爸爸'], board: 'cross', mode: 'quick', network: true, props: true,
  }, 201);
  await post(guest, `/api/rooms/${hostRoom.id}/join`, { invite: hostRoom.network.invite, name: '爸爸' }, 200);

  const hostPage = await host.newPage();
  const guestPage = await guest.newPage();
  const errors = [];
  for (const page of [hostPage, guestPage]) { page.on('pageerror', error => errors.push(error.message)); page.setDefaultTimeout(20000); }
  await Promise.all([hostPage.goto('/play', { waitUntil: 'networkidle' }), guestPage.goto('/play', { waitUntil: 'networkidle' })]);
  const invite = await hostPage.getByRole('textbox', { name: '观赛链接', exact: true }).inputValue();
  const watcher = await makeContext({ width: 1024, height: 768 });
  const watchPage = await watcher.newPage();
  watchPage.setDefaultTimeout(20000);
  watchPage.on('pageerror', error => errors.push(error.message));
  await watchPage.goto(invite, { waitUntil: 'networkidle' });
  await watchPage.getByRole('textbox', { name: '你的昵称', exact: true }).fill('奶奶');
  await watchPage.getByRole('button', { name: '开始观赛', exact: true }).click();
  await watchPage.getByText('观赛中', { exact: true }).waitFor();

  let room = (await (await host.request.get('/api/session')).json()).room;
  await guestPage.getByRole('button', { name: '我准备好了', exact: true }).click();
  await hostPage.getByRole('button', { name: '开始游戏', exact: true }).click();
  await watchPage.getByText('等待开局', { exact: true }).waitFor({ state: 'detached' });

  await hostPage.getByRole('button', { name: /^道具口袋 ·/ }).click();
  await hostPage.getByRole('button', { name: '使用起飞卡', exact: true }).click();
  await hostPage.locator('.prop-targets .prop-target').first().click();
  const hostLine = hostPage.locator('.key-move-narration p');
  const watchLine = watchPage.locator('.key-move-narration p');
  await hostLine.filter({ hasText: '使用起飞卡' }).waitFor();
  await watchLine.filter({ hasText: '使用起飞卡' }).waitFor();
  const sharedText = await hostLine.innerText();
  assert.equal(await watchLine.innerText(), sharedText, 'players and spectators see the same authoritative sentence');
  assert.match(sharedText, /小朋友.*小熊号/);
  assert.doesNotMatch(sharedText, /player-\d|p\d-\d|[a-f0-9]{32}/i, 'internal identifiers are not rendered');
  assert.equal(await hostPage.evaluate(() => window.__qaMic), 0);
  assert.equal(await watchPage.evaluate(() => window.__qaMic), 0);
  const output = process.env.QA_OUTPUT_DIR || '/tmp/flytogether-key-move-qa';
  await mkdir(output, { recursive: true });
  await Promise.all([
    hostPage.screenshot({ path: `${output}/player.png`, fullPage: true }),
    watchPage.screenshot({ path: `${output}/spectator.png`, fullPage: true }),
  ]);

  room = (await (await host.request.get('/api/session')).json()).room;
  await command(host, room, 'end');
  room = (await (await host.request.get('/api/session')).json()).room;
  await command(host, room, 'replay');
  await hostLine.waitFor({ state: 'detached' });
  await watchLine.waitFor({ state: 'detached' });
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ result: 'PASS', sharedText, oldNarrationClearedOnReplay: true, microphoneRequests: 0, errors }));
} finally {
  await Promise.all(contexts.map(context => context.close()));
  await browser.close();
}
