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
  localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
  localStorage.setItem('fly-sound', 'off');
  localStorage.setItem('fly-master-muted', 'on');
  navigator.mediaDevices.getUserMedia = async () => { throw new Error('Click-only co-op QA must not request microphone'); };
});
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const roomSnapshot = async () => (await (await context.request.get('/api/session')).json()).room;
const waitForWatchers = async count => {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    const current = await roomSnapshot();
    if ((current.audience?.online || 0) >= count) return current;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`expected ${count} connected spectator(s), got ${(await roomSnapshot()).audience?.online || 0}`);
};
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
  assert.equal(await page.locator('html').getAttribute('data-motion'), 'reduce', 'the rescue theme must honor reduced-motion preference');
  assert.equal(await page.getByRole('button', { name: '恢复声音', exact: true }).getAttribute('aria-pressed'), 'true', 'the rescue theme must honor the saved mute preference');
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
  await guestContext.addInitScript(() => {
    localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-auto-voice', 'off');
    localStorage.setItem('fly-host-voice', 'off');
    localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
    localStorage.setItem('fly-sound', 'off');
    localStorage.setItem('fly-master-muted', 'on');
  });
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
  await spectatorContext.addInitScript(() => {
    localStorage.setItem('fly-auto-voice', 'off');
    localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
    localStorage.setItem('fly-master-muted', 'on');
  });
  const spectatorPage = await spectatorContext.newPage();
  await spectatorPage.goto(new URL(`/#watch=${inviteRoom.id}.${inviteRoom.audience.token}`, base).href, { waitUntil: 'networkidle' });
  await spectatorPage.getByRole('textbox', { name: '你的昵称' }).fill('观赛家人');
  await spectatorPage.getByRole('button', { name: '开始观赛', exact: true }).click();
  await spectatorPage.getByRole('region', { name: '星际救援任务' }).waitFor();
  await spectatorPage.getByText('建立全员联络', { exact: true }).waitFor();
  assert.equal(await spectatorPage.locator('html').getAttribute('data-motion'), 'reduce', 'the phone spectator must honor reduced motion');
  assert.equal(await spectatorPage.getByRole('button', { name: '恢复声音', exact: true }).getAttribute('aria-label'), '恢复声音', 'the phone spectator must honor saved mute preference');
  await waitForWatchers(1);
  assert.equal(await spectatorPage.locator('.play-shell.theme-star-rescue').count(), 1, 'spectator must receive the rescue theme');
  const spectatorProgress = await spectatorPage.locator('.mission-progress strong').innerText();
  assert.equal(spectatorProgress, '0/2', 'spectator task progress must match both players');

  // Earn real server-side progress with alternating seat credentials, then
  // verify the mobile player and spectator receive the same committed state.
  let lastActor, lastPayload;
  for (let i = 0; i < 80; i++) {
    room = await roomSnapshot();
    if (room.state.mission.progress > 0) break;
    const actor = room.state.current === 0 ? context : guestContext;
    const payload = {
      command_id: crypto.randomUUID(),
      game_id: room.state.id,
      expected_version: room.state.version,
      action: room.state.phase === 'roll' ? 'roll' : 'move',
      ...(room.state.phase === 'move' ? { piece_id: room.state.legal_moves[0]?.piece_id } : {}),
    };
    assert.ok(payload.action !== 'move' || payload.piece_id, 'a real legal move must be available');
    const response = await actor.request.post(`/api/rooms/${room.id}/commands`, { data: payload });
    assert.equal(response.status(), 200, await response.text());
    lastActor = actor;
    lastPayload = payload;
  }
  room = await roomSnapshot();
  assert.ok(room.state.players.some(player => player.stats.rolls >= 3), 'a pilot must satisfy the selected task using committed rolls');
  assert.equal(room.state.mission.progress, 1);
  assert.equal(room.state.mission.contributors.length, 1);
  await guestPage.getByText('1/2', { exact: true }).waitFor();
  await spectatorPage.waitForFunction(() => document.querySelector('.mission-progress strong')?.innerText.replace(/\s/g, '') === '1/2', null, { timeout: 10000 }).catch(async error => {
    const rendered = await spectatorPage.locator('.mission-progress strong').innerText().catch(() => 'missing');
    const reconnecting = await spectatorPage.locator('.connection-hint').allInnerTexts().catch(() => []);
    const watchResponse = await spectatorContext.request.get(`/api/rooms/${inviteRoom.id}/watch`);
    const watchState = watchResponse.ok() ? await watchResponse.json() : { status: watchResponse.status() };
    throw new Error(`spectator did not receive committed mission progress: ${JSON.stringify({ rendered, reconnecting, audienceOnline: room.audience?.online, watchState: { game: watchState.state?.id, seq: watchState.seq, progress: watchState.state?.mission?.progress }, room: { game: room.state.id, seq: room.seq, progress: room.state.mission.progress }, cause: error.message })}`);
  });
  const diceDialog = spectatorPage.getByRole('dialog', { name: '3D 掷骰结果' });
  const settleUntil = Date.now() + 30000;
  let idleSince = 0;
  while (Date.now() < settleUntil) {
    if (await diceDialog.isVisible().catch(() => false)) {
      await diceDialog.getByRole('button', { name: /继续飞行/ }).click();
      idleSince = 0;
    } else if (!idleSince) {
      idleSince = Date.now();
    } else if (Date.now() - idleSince >= 500) {
      break;
    }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.ok(idleSince && Date.now() - idleSince >= 500, 'spectator board presentation must settle');
  await spectatorPage.screenshot({ path: join(out, 'rescue-spectator-mobile.png'), fullPage: true });

  // The second seat remains in the server room while its phone is offline.
  // Finish its roll target, reconnect, and require the newest shared progress.
  await guestContext.setOffline(true);
  await guestPage.locator('.connection-hint').waitFor({ state: 'visible' });
  for (let i = 0; i < 80; i++) {
    room = await roomSnapshot();
    if (room.state.mission.progress === room.state.mission.target) break;
    const actor = room.state.current === 0 ? context : guestContext;
    const payload = {
      command_id: crypto.randomUUID(),
      game_id: room.state.id,
      expected_version: room.state.version,
      action: room.state.phase === 'roll' ? 'roll' : 'move',
      ...(room.state.phase === 'move' ? { piece_id: room.state.legal_moves[0]?.piece_id } : {}),
    };
    assert.ok(payload.action !== 'move' || payload.piece_id, 'a real legal move must be available while the phone is offline');
    const response = await actor.request.post(`/api/rooms/${room.id}/commands`, { data: payload });
    assert.equal(response.status(), 200, await response.text());
    lastActor = actor;
    lastPayload = payload;
  }
  room = await roomSnapshot();
  assert.equal(room.state.mission.status, 'completed', 'the second pilot must complete the shared task while the phone is offline');
  await guestContext.setOffline(false);
  await guestPage.locator('.connection-hint').waitFor({ state: 'hidden', timeout: 15000 });
  await guestPage.waitForFunction(() => document.querySelector('.mission-progress strong')?.innerText.replace(/\s/g, '') === '2/2', null, { timeout: 15000 });
  await spectatorPage.waitForFunction(() => document.querySelector('.mission-progress strong')?.innerText.replace(/\s/g, '') === '2/2', null, { timeout: 15000 });

  // Retrying the exact committed command must not count the action twice.
  const committedVersion = room.state.version;
  const retry = await lastActor.request.post(`/api/rooms/${room.id}/commands`, { data: lastPayload });
  assert.equal(retry.status(), 200, await retry.text());
  assert.equal((await roomSnapshot()).state.version, committedVersion, 'an idempotent retry must not create a second mission update');

  // Refreshing the independent phone seat during a changed mission must recover
  // the persisted/shared progress instead of replaying an old snapshot.
  await guestPage.reload({ waitUntil: 'networkidle' });
  await guestPage.getByRole('region', { name: '星际救援任务' }).waitFor();
  assert.equal(await guestPage.locator('html').getAttribute('data-motion'), 'reduce', 'the phone seat must honor reduced motion');
  assert.equal(await guestPage.getByRole('button', { name: '恢复声音', exact: true }).getAttribute('aria-pressed'), 'true', 'the phone seat must honor mute preference');
  await guestPage.getByText('2/2', { exact: true }).waitFor();
  const recoveredGuest = (await (await guestContext.request.get('/api/session')).json()).room;
  assert.equal(recoveredGuest.state.mission.progress, 2);
  assert.deepEqual(recoveredGuest.state.mission, room.state.mission);

  await spectatorContext.close();
  assert.deepEqual(errors, []);

  console.log(JSON.stringify({ passed: true, mode: 'coop', name: '星河救援', mission: room.state.mission.id, commands, players: room.state.players.length, replayPreservedThemeAndMission: true, inviteRoomMissionShared: true, spectatorThemeAndMissionShared: true, liveProgressSyncedAcrossDevices: true, offlinePhoneRecoveredCompletedMission: true, idempotentRetry: true, phoneReloadRecoveredProgress: true, reducedMotionAndMutePreserved: true, pageErrors: errors }));
} finally {
  await browser.close();
}
