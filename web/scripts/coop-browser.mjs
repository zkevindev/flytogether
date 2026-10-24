import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';

assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const base = process.env.QA_BASE_URL;
const out = process.env.QA_OUTPUT_DIR || 'test-results/coop';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const context = await browser.newContext({ baseURL: base, viewport: { width: 1360, height: 980 } });
context.setDefaultTimeout(10000);
await context.addInitScript(() => {
  localStorage.setItem('fly-tutorial-v1', 'skipped');
  localStorage.setItem('fly-auto-voice', 'off');
  localStorage.setItem('fly-host-voice', 'off');
  navigator.mediaDevices.getUserMedia = async () => { throw new Error('Click-only co-op QA must not request microphone'); };
});
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const roomSnapshot = async () => (await (await context.request.get('/api/session')).json()).room;
const command = async (room, action, fields = {}) => {
  const response = await context.request.post(`/api/rooms/${room.id}/commands`, { data: { command_id: crypto.randomUUID(), game_id: room.state.id, expected_version: room.state.version, action, ...fields } });
  const body = await response.json();
  assert.equal(response.status(), 200, JSON.stringify(body));
  return body;
};

try {
  await page.goto(base, { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: '创建游戏', exact: true }).click();
  await page.getByRole('button', { name: '2 位', exact: true }).click();
  await page.getByRole('checkbox', { name: /添加机器人陪玩/ }).uncheck();
  await page.getByRole('button', { name: /星河救援 · 合作挑战/ }).click();
  await page.locator('#mission-card').selectOption('launch-formation');
  await page.getByRole('button', { name: '创建并开始游戏', exact: true }).click();
  await page.getByText('本局规则 · 星河救援').waitFor();
  await page.getByRole('region', { name: '星际救援任务' }).waitFor();
  await page.screenshot({ path: join(out, 'rescue-board-desktop.png'), fullPage: true });

  let room = await roomSnapshot();
  assert.equal(room.state.mode, 'coop');
  assert.equal(room.state.rules.id, 'coop');
  assert.equal(room.state.players.length, 2);
  assert.equal(room.state.theme_id, 'star-rescue');
  assert.equal(room.state.mission.id, 'launch-formation');
  assert.equal(room.state.mission.status, 'active');
  assert.ok(room.state.players.every(player => player.pieces.length === 2));
  await page.getByText(/救援进度/).first().waitFor();

  // Play through real random dice and legal moves; no board or dice injection.
  let commands = 0;
  for (; commands < 2200; commands++) {
    room = await roomSnapshot();
    if (room.state.phase === 'finished') break;
    if (room.state.phase === 'roll') {
      await command(room, 'roll');
    } else {
      const move = [...room.state.legal_moves].sort((a, b) => b.to - a.to)[0];
      assert.ok(move, 'move phase must offer a legal move');
      await command(room, 'move', { piece_id: move.piece_id });
    }
  }
  room = await roomSnapshot();
  assert.equal(room.state.phase, 'finished', `co-op game did not finish after ${commands} commands`);
  assert.equal(room.state.winner, 'team');
  assert.equal(room.state.mission.status, 'completed', 'all real player takeoffs must complete the selected mission');
  assert.equal(room.state.mission.progress, 2);
  assert.ok(room.state.players.every(player => player.pieces.some(piece => piece.progress === room.state.board.finish)));
  await page.reload({ waitUntil: 'networkidle' });
  assert.equal((await roomSnapshot()).state.mission.status, 'completed', 'refresh must preserve committed mission completion');
  await page.getByRole('dialog', { name: '全员抵达，救援成功！' }).waitFor();
  await page.getByText('🚀 全队完成星河救援').waitFor();
  await page.getByText('制作家庭回顾卡').click();
  await page.getByRole('img', { name: '全队共同完成星河救援的家庭飞行棋回顾卡' }).waitFor();
  const history = await (await context.request.get('/api/family')).json();
  const savedCoop = history.matches.find(match => match.mode === 'coop');
  assert.ok(savedCoop, 'natural cooperative finish must be saved to family history');
  assert.ok(savedCoop.players.every(player => player.won), 'family history must credit every co-op player');
  assert.deepEqual(errors, []);

  await page.getByRole('button', { name: '同配置再玩一局', exact: true }).click();
  await page.getByRole('region', { name: /游戏棋盘|棋盘/ }).waitFor().catch(() => {});
  await page.waitForTimeout(250);
  room = await roomSnapshot();
  assert.equal(room.state.mode, 'coop', 'replay must preserve the selected mode');
  assert.equal(room.state.theme_id, 'star-rescue', 'replay must preserve the rescue theme');
  assert.equal(room.state.mission.id, 'launch-formation', 'replay must preserve the chosen card');
  assert.equal(room.state.mission.progress, 0, 'replay starts a fresh mission');
  assert.equal(room.state.mission.status, 'active');
  assert.equal(room.state.phase, 'roll');
  assert.ok(room.state.players.every(player => player.pieces.length === 2));

  const inviteResponse = await context.request.post('/api/rooms/new', { data: { names: ['房主', '朋友'], room_name: '星河救援联网验收', network: true, mode: 'coop', theme_id: 'star-rescue', mission_id: 'signal-network', props: false, board: 'cross' } });
  assert.equal(inviteResponse.status(), 201, await inviteResponse.text());
  const inviteRoom = await inviteResponse.json();
  const guestContext = await browser.newContext({ baseURL: base, viewport: { width: 390, height: 844 } });
  guestContext.setDefaultTimeout(10000);
  const guestPage = await guestContext.newPage();
  await guestPage.goto(new URL(`/#join=${inviteRoom.id}.${inviteRoom.network.invite}`, base).href, { waitUntil: 'networkidle' });
  await guestPage.getByRole('textbox', { name: '你的昵称' }).fill('朋友');
  await guestPage.getByRole('button', { name: '加入房间', exact: true }).click();
  await guestPage.getByRole('region', { name: '联网准备区' }).waitFor();
  await guestPage.getByRole('button', { name: '我准备好了', exact: true }).click();
  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('region', { name: '联网准备区' }).waitFor();
  await page.getByRole('button', { name: '开始游戏', exact: true }).click();
  room = await roomSnapshot();
  assert.equal(room.state.mode, 'coop', 'invite room must retain co-op mode');
  assert.equal(room.state.theme_id, 'star-rescue');
  assert.equal(room.state.mission.id, 'signal-network');
  assert.equal(room.state.players.length, 2);
  assert.ok(room.state.players.every(player => player.pieces.length === 2));
  const guestRoom = (await (await guestContext.request.get('/api/session')).json()).room;
  assert.equal(guestRoom.state.mode, 'coop');
  assert.deepEqual(guestRoom.state.mission, room.state.mission, 'all invitees must receive the same task state');
  assert.ok(await guestPage.getByRole('region', { name: '星际救援任务' }).isVisible(), 'invitee must see the rescue mission card');

  assert.ok(inviteRoom.audience?.token, 'co-op invite room should issue an independent spectator link');
  const spectatorContext = await browser.newContext({ baseURL: base, viewport: { width: 390, height: 844 } });
  spectatorContext.setDefaultTimeout(10000);
  await spectatorContext.addInitScript(() => { localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-tutorial-v1', 'skipped'); });
  const spectatorPage = await spectatorContext.newPage();
  await spectatorPage.goto(new URL(`/#watch=${inviteRoom.id}.${inviteRoom.audience.token}`, base).href, { waitUntil: 'networkidle' });
  await spectatorPage.getByRole('textbox', { name: '你的昵称' }).fill('观赛家人');
  await spectatorPage.getByRole('button', { name: '开始观赛', exact: true }).click();
  await spectatorPage.getByRole('region', { name: '星际救援任务' }).waitFor();
  await spectatorPage.getByText('建立全员联络', { exact: true }).waitFor();
  assert.equal(await spectatorPage.locator('.play-shell.theme-star-rescue').count(), 1, 'spectator must receive the rescue theme');
  const spectatorProgress = await spectatorPage.locator('.mission-progress strong').innerText();
  assert.equal(spectatorProgress, '0/2', 'spectator task progress must match both players');
  await spectatorPage.screenshot({ path: join(out, 'rescue-spectator-mobile.png'), fullPage: true });
  await spectatorContext.close();
  assert.deepEqual(errors, []);

  console.log(JSON.stringify({ passed: true, mode: 'coop', name: '星河救援', mission: room.state.mission.id, commands, players: room.state.players.length, replayPreservedThemeAndMission: true, inviteRoomMissionShared: true, spectatorThemeAndMissionShared: true, pageErrors: errors }));
} finally {
  await browser.close();
}
