import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const base = process.env.QA_BASE_URL, out = process.env.QA_OUTPUT_DIR || 'test-results/network-bot';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const errors = [], contexts = [], results = [];
const transportRetry = async (request, retryAllowed = true) => {
  for (let attempt=0; attempt<3; attempt++) {
    try {return await request();}
    catch (error) {
      if (!retryAllowed || attempt===2 || !/ETIMEDOUT|ECONNRESET|EAI_AGAIN|socket hang up|Timeout|timed out/.test(String(error))) throw new Error('Browser QA transport failed: ' + (retryAllowed ? 'bounded same-request retries exhausted' : 'request was not retried'));
      await new Promise(resolve=>setTimeout(resolve,250));
    }
  }
};
const session = async c => { const response=await transportRetry(()=>c.request.get('/api/session',{timeout:12000}));assert.equal(response.status(),200);return (await response.json()).room; };
const wait = async (predicate, message, timeout = 20000) => {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { if (await predicate()) return; await new Promise(r => setTimeout(r, 150)); }
  throw new Error(message);
};
const context = async (mobile = false) => {
  const c = await browser.newContext({ baseURL: base, viewport: mobile ? { width: 390, height: 844 } : { width: 1360, height: 1000 } }); contexts.push(c);c.setDefaultTimeout(15000);c.setDefaultNavigationTimeout(20000);
  await c.addInitScript(() => {
    localStorage.setItem('fly-tutorial-v1', 'skipped'); localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-host-voice', 'off');
    window.__botMic = 0; navigator.mediaDevices.getUserMedia = async () => { window.__botMic++; throw new Error('Click-only QA must not capture microphone'); };
  });
  return c;
};
const page = async c => { const p = await c.newPage(); p.on('pageerror', e => errors.push(e.message)); p.on('console', e => { if(e.type() === 'error' && !e.text().includes('403') && !e.text().includes('400')) errors.push(e.text()); }); return p; };
const post = async (c, path, data) => { const r = await transportRetry(()=>c.request.post(path, { data, timeout:12000 }),!!data.command_id); const body = await r.json(); assert.equal(r.status(), 200, JSON.stringify(body)); return body; };
const command = async (c, action, extra = {}) => { const r = await session(c); return post(c, `/api/rooms/${r.id}/commands`, { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, ...extra }); };
const closeDice = async p => { const button = p.getByRole('button', { name: '继续飞行', exact: true }); if (await button.isVisible()) await button.click({ timeout: 2000 }).catch(() => {}); };
try {
  const h = await context(), g = await context(true), outsider = await context(); let hp = await page(h), gp = await page(g);
  await hp.goto(base, { waitUntil: 'networkidle' }); await hp.getByRole('button', { name: '创建游戏', exact: true }).click();
  const setup = hp.getByRole('dialog', { name: '创建家庭房间' });
  await setup.getByRole('checkbox', { name: /多人联网邀请房间/ }).check();
  const toggle = setup.getByRole('checkbox', { name: /添加机器人陪玩/ }); assert.equal(await toggle.isEnabled(), true); await toggle.check();
  await setup.getByRole('button', { name: '3 位', exact: true }).click(); await setup.getByRole('button', { name: /挑战/ }).click();
  await setup.getByRole('textbox', { name: '第 1 位飞行员昵称' }).fill('房主');
  await setup.getByText('2 位真人 + 1 位机器人', { exact: false }).waitFor();
  await setup.getByText('灵矽语音助手不占席位', { exact: false }).waitFor();
  await setup.evaluate(el => {el.scrollTop = el.scrollHeight - el.clientHeight;});
  await hp.screenshot({ path: join(out, 'mixed-setup.png') });
  await setup.getByRole('button', { name: '创建邀请房间', exact: true }).click(); await hp.getByRole('region', { name: '联网准备区' }).waitFor();
  let r = await session(h); const id = r.id, botID = r.state.players[2].id;
  assert.equal(r.state.difficulty, 'hard'); assert.equal(r.network.seats[2].robot, true); assert.equal(r.network.seats[2].ready, true); assert.equal(r.network.seats[2].occupied, true); assert.equal(r.network.seats[2].online, true);
  assert.equal(await hp.getByRole('button', { name: '开始游戏', exact: true }).isDisabled(), true);
  const invite = new URL('/#join=' + id + '.' + r.network.invite, base).href;
  await gp.goto(invite, { waitUntil: 'networkidle' }); await gp.getByRole('textbox', { name: '你的昵称' }).fill('朋友'); await gp.getByRole('button', { name: '加入房间', exact: true }).click();
  await gp.getByRole('button', { name: '我准备好了', exact: true }).click();
  await wait(async () => !(await hp.getByRole('button', { name: '开始游戏', exact: true }).isDisabled()), 'bot prevented starting');
  await hp.getByRole('region', { name: '联网准备区' }).getByText('自动准备 · 自动行动', { exact: true }).waitFor();
  await hp.screenshot({ path: join(out, 'mixed-lobby-desktop.png') }); await gp.screenshot({ path: join(out, 'mixed-lobby-mobile.png') });
  const extra = await outsider.request.post(`/api/rooms/${id}/join`, { data: { invite: r.network.invite, name: '第三位真人' } }); assert.equal((await extra.json()).code, 'ROOM_FULL');
  await hp.getByRole('button', { name: '开始游戏', exact: true }).click();
  // Advance the two real seats only until the robot takes the turn, without faking dice or board state.
  for (let i = 0; i < 100; i++) {
    r = await session(h); if (r.state.current === 2) break;
    const actor = r.state.current === 0 ? h : g;
    if (r.state.phase === 'roll') await command(actor, 'roll'); else await command(actor, 'move', { piece_id: r.state.legal_moves[0].piece_id });
  }
  r = await session(h); assert.equal(r.state.current, 2);
  const forbidden = await h.request.post(`/api/rooms/${id}/commands`, { data: { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action: 'roll' } }); assert.equal((await forbidden.json()).code, 'NOT_YOUR_TURN');
  const deniedVoice = await h.request.get(`/ws/voice?room_id=${id}`);
  const voiceConfigured = (await (await h.request.get('/api/config')).json()).voice_configured;
  assert.equal(deniedVoice.status(), voiceConfigured ? 403 : 503, 'off-turn voice must be denied; unconfigured upstream is unavailable before authorization');
  // Pause is allowed even during the bot's turn. Observe longer than two AI ticks.
  await command(h, 'pause'); const paused = await session(h); await new Promise(r => setTimeout(r, 2700)); assert.equal((await session(h)).state.version, paused.state.version);
  const baseline = (await session(h)).state.players[2].stats;
  await command(h, 'resume'); await wait(async () => { const s = (await session(h)).state; return s.players[2].stats.rolls > baseline.rolls; }, 'robot never rolled', 20000);
  for (const p of [hp, gp]) await p.reload({ waitUntil: 'networkidle' });
  await hp.getByRole('region', { name: '联网准备区' }).getByText('自动准备 · 自动行动', { exact: true }).waitFor();
  assert.equal((await session(g)).me.player_id, 'player-1');
  results.push({ case: 'two humans + hard bot', passed: true, checks: ['UI permits bot in network mode', 'clear seat composition', 'bot automatically ready with no socket', 'two real cookies', 'bot seat cannot be claimed', 'human command/voice cannot control bot', 'pause stops AI ticks', 'resume automatically rolls'] });
  console.log(JSON.stringify(results.at(-1)));
  await hp.close(); await gp.close();
  // Natural complete game using the real server AI scheduler, ordinary random dice and legal moves.
  let steps = 0;
  for (; steps < 1800; steps++) {
    r = await session(h); if (r.state.phase === 'finished') break;
    if (r.state.current === 2) { await new Promise(r => setTimeout(r, 200)); continue; }
    const actor = r.state.current === 0 ? h : g, s = r.state;
    if (s.phase === 'roll') {
      const player = s.players[s.current];
      if (!s.active_prop && player.props.launch > 0 && player.pieces.every(p => p.progress < 0)) await command(actor, 'prop', { prop_id: 'launch', piece_id: player.pieces[0].id });
      else await command(actor, 'roll');
    } else await command(actor, 'move', { piece_id: [...s.legal_moves].sort((a, b) => Number(b.finish) - Number(a.finish) || b.to - a.to)[0].piece_id });
  }
  assert.equal(r.state.phase, 'finished'); assert.ok(r.state.players[2].stats.rolls > 0); assert.ok(r.state.players[2].stats.props_used > 0);
  hp = await page(h); gp = await page(g); await hp.goto(new URL('/play', base).href, { waitUntil: 'networkidle' }); await gp.goto(new URL('/play', base).href, { waitUntil: 'networkidle' });
  await hp.getByRole('dialog', { name: '这次飞行，圆满抵达！' }).waitFor(); await gp.getByRole('dialog', { name: '这次飞行，圆满抵达！' }).waitFor();
  await hp.screenshot({ path: join(out, 'mixed-result.png') }); const oldID = r.state.id;
  await hp.getByRole('button', { name: '同配置再玩一局', exact: true }).click(); await wait(async () => (await session(h)).status === 'waiting', 'replay did not return to lobby');
  const replay = await session(h); assert.equal(replay.id, id); assert.notEqual(replay.state.id, oldID); assert.equal(replay.state.difficulty, 'hard'); assert.equal(replay.state.players[2].ai, true); assert.equal(replay.network.seats[2].ready, true); assert.equal(replay.network.seats[1].ready, false); assert.equal((await session(g)).me.player_id, 'player-1');
  for (const player of replay.state.players) assert.deepEqual(player.stats, { rolls: 0, takeoffs: 0, arrivals: 0, captures: 0, props_used: 0 });
  const waitingVersion = replay.state.version; await new Promise(r => setTimeout(r, 1600)); assert.equal((await session(h)).state.version, waitingVersion, 'bot moved in lobby');
  results.push({ case: 'natural mixed win + replay', passed: true, scheduler_poll_steps: steps, winner: r.state.players.find(p => p.id === r.state.winner).name, bot_stats: r.state.players[2].stats, preserved_bot_and_seats: true });
  console.log(JSON.stringify(results.at(-1)));
  for (const p of [hp, gp]) await p.close();
  // The 2-seat and 4-seat bounds, plus easy/normal difficulty and a disabled-bot toggle.
  for (const count of [2, 4]) {
    const difficulty = count === 2 ? 'easy' : 'normal';
    const names = Array.from({ length: count }, (_, i) => i === count - 1 ? '灵矽机器人' : '飞行员' + i);
    const created = await h.request.post('/api/rooms/new', { data: { names, network: true, ai: true, difficulty, board: 'cross', mode: 'quick', props: true } }); assert.equal(created.status(), 201); r = await created.json();
    const players = [h], pages = [];
    for (let i = 1; i < count - 1; i++) { const c = await context(); players.push(c); await post(c, `/api/rooms/${r.id}/join`, { invite: r.network.invite, name: '队友' + i }); }
    for (let i = 0; i < players.length; i++) { const p = await page(players[i]); pages.push(p); await p.goto(new URL('/play', base).href, { waitUntil: 'networkidle' }); if (i) await p.getByRole('button', { name: '我准备好了', exact: true }).click(); }
    await wait(async () => (await session(h)).network.seats.every(s => s.ready && s.online), 'bound room not ready'); await command(h, 'start');
    for (let i = 0; i < 100; i++) { r = await session(h); if (r.state.current === count - 1) break; const actor = players[r.state.current]; if (r.state.phase === 'roll') await command(actor, 'roll'); else await command(actor, 'move', { piece_id: r.state.legal_moves[0].piece_id }); }
    await wait(async () => (await session(h)).state.players[count - 1].stats.rolls > 0, 'bound bot did not roll'); r = await session(h);
    if (difficulty === 'easy') assert.equal(r.state.players[count - 1].stats.props_used, 0);
    else assert.ok(r.state.players[count - 1].stats.props_used > 0);
    await command(h, 'end'); for (const p of pages) { await wait(async () => new URL(p.url()).pathname === '/', 'end left a player on board'); assert.equal(await p.evaluate(() => window.__botMic), 0); await p.close(); }
    results.push({ case: `${count - 1} human(s) + ${difficulty} bot`, passed: true, human_cookies: players.length, bot_rolls: r.state.players[count - 1].stats.rolls }); console.log(JSON.stringify(results.at(-1)));
  }
  assert.deepEqual(errors, []); await writeFile(join(out, 'results.json'), JSON.stringify({ results, console_errors: errors, microphone_requests: 0, injection: false }, null, 2));
  console.log('PASS: mixed invitation rooms, natural AI scheduler, full game, authority, replay and seat bounds');
} finally { await browser.close(); }
