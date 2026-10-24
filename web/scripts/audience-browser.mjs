import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

assert.ok(process.env.QA_BASE_URL, 'Set QA_BASE_URL');
const base = process.env.QA_BASE_URL, out = process.env.QA_OUTPUT_DIR || 'test-results/audience';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const contexts = [], pages = [], errors = [], report = [];
const wait = async (fn, label, timeout = 20000) => {
  const start = Date.now();
  while (Date.now() - start < timeout) { if (await fn()) return; await new Promise(r => setTimeout(r, 100)); }
  throw Error(label);
};
const context = async (mobile = false, flat = true) => {
  const c = await browser.newContext({ baseURL: base, viewport: mobile ? { width: 390, height: 844 } : { width: 1360, height: 1000 } });
  contexts.push(c);
  await c.addInitScript(({ flat }) => {
    localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-host-voice', 'off'); localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-experience', JSON.stringify({ board: flat ? 'flat' : 'auto', motion: 'full' }));
    window.__audience = { mic: 0, sockets: [], state: null, losses: 0, contexts: 0, props: [] };
    addEventListener('DOMContentLoaded', () => new MutationObserver(() => { for (const el of document.querySelectorAll('[data-prop-fx]')) window.__audience.props.push(el.getAttribute('data-prop-fx')); }).observe(document.body, { childList: true, subtree: true }));
    navigator.mediaDevices.getUserMedia = async () => { window.__audience.mic++; throw new DOMException('Permission denied for this QA case', 'NotAllowedError'); };
    const original = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (kind, ...args) {
      if (kind === 'webgl' || kind === 'webgl2') { window.__audience.contexts++; this.addEventListener('webglcontextlost', () => window.__audience.losses++); }
      return original.call(this, kind, ...args);
    };
    const WS = WebSocket;
    window.WebSocket = class extends WS {
      constructor(url, ...args) {
        super(url, ...args);
        if (String(url).includes('/ws/game?')) {
          window.__audience.sockets.push(this);
          this.addEventListener('message', event => { if (typeof event.data === 'string') { const data = JSON.parse(event.data); if (data.type === 'state') window.__audience.state = data.room; } });
        }
      }
    };
  }, { flat });
  return c;
};
const page = async (c, path) => {
  const p = await c.newPage(); pages.push(p); p.setDefaultTimeout(20000); p.on('pageerror', e => errors.push(e.message)); await p.goto(path, { waitUntil: 'networkidle' }); return p;
};
const session = async c => (await (await c.request.get('/api/session')).json()).room;
const post = async (c, path, data, expected = 200) => { const res = await c.request.post(path, { data }); const value = await res.json(); assert.equal(res.status(), expected, JSON.stringify(value)); return value; };
let host, guest, roomID;
const command = async (action, extra = {}, actor) => {
  const r = await session(host), c = actor || (r.state.current === 0 ? host : guest);
  return post(c, `/api/rooms/${roomID}/commands`, { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, ...extra });
};
const audienceView = async c => (await (await c.request.get(`/api/rooms/${roomID}/watch`)).json());
try {
  host = await context(); guest = await context(); const fans = [await context(false, false), await context(true)];
  let r = await post(host, '/api/rooms', { names: ['房主', '朋友'], room_name: '家人观赛验收', network: true, props: true, board: 'cross', mode: 'quick' }, 201);
  roomID = r.id;
  await post(guest, `/api/rooms/${roomID}/join`, { invite: r.network.invite, name: '朋友' });
  const hp = await page(host, '/play'), gp = await page(guest, '/play');
  const link = await hp.getByRole('textbox', { name: '观赛链接', exact: true }).inputValue();
  const fp = [];
  for (let i = 0; i < 2; i++) {
    const p = await page(fans[i], link); fp.push(p);
    await p.getByRole('textbox', { name: '你的昵称', exact: true }).fill(i === 0 ? '奶奶' : '小姨');
    await p.getByRole('button', { name: '开始观赛', exact: true }).click();
    await p.getByText('观赛中', { exact: true }).waitFor();
    await p.getByText('等待开局', { exact: true }).waitFor();
    assert.equal(await p.getByRole('button', { name: '掷骰子', exact: true }).isEnabled().catch(() => false), false);
    assert.equal(await p.evaluate(() => window.__audience.mic), 0);
  }
  await wait(async () => (await session(host)).audience.online === 2, 'two spectators not online');
  r = await session(host); assert.equal(r.network.seats.length, 2); assert.equal(r.audience.count, 2);
  for (const fan of fans) { const v = await audienceView(fan); assert.ok(!v.me && !v.profile_ids); assert.equal(v.network.invite, ''); assert.ok(!v.audience.token); assert.equal(v.voice, 'disconnected'); }
  const kept = await page(host, link);
  await kept.getByRole('textbox', { name: '你的昵称', exact: true }).fill('旁观房主');
  await kept.getByRole('button', { name: '开始观赛', exact: true }).click();
  await kept.getByText(/不会改变席位/).waitFor(); assert.equal((await session(host)).me.player_id, 'player-0'); await kept.close();
  await fp[1].reload({ waitUntil: 'networkidle' }); await fp[1].getByText('观赛中', { exact: true }).waitFor();
  const secondTab = await page(fans[1], link); await secondTab.getByText('观赛中', { exact: true }).waitFor();
  assert.equal((await session(host)).audience.count, 2); await secondTab.close();
  report.push({ case: 'two players and two independent spectators', seats: 2, spectators: 2, refresh_identity: true, player_seat_kept: true, private_fields_removed: true });

  await gp.getByRole('button', { name: '我准备好了', exact: true }).click();
  await hp.getByRole('button', { name: '开始游戏', exact: true }).click();
  for (const p of fp) await p.getByText('等待开局', { exact: true }).waitFor({ state: 'detached' });
  await hp.getByRole('button', { name: /^道具口袋 ·/ }).click(); await hp.getByRole('button', { name: '使用起飞卡', exact: true }).click();
  await hp.locator('.prop-targets .prop-target').first().click();
  await Promise.all(fp.map(p => p.waitForFunction(() => window.__audience.props.includes('launch'))));
  await hp.locator('.roll-button').click();
  for (const p of [hp, gp, ...fp]) await p.getByRole('dialog', { name: '3D 掷骰结果' }).waitFor();
  r = await session(host); const roll = r.state.dice_rolls;
  for (const p of fp) { assert.deepEqual(await p.evaluate(() => window.__audience.state.state.dice_rolls), roll); await p.getByRole('button', { name: '继续飞行', exact: true }).click(); }
  await hp.getByRole('button', { name: '继续飞行', exact: true }).click(); await gp.getByRole('button', { name: '继续飞行', exact: true }).click();
  if (r.state.phase === 'move') await command('move', { piece_id: r.state.legal_moves[0].piece_id });
  r = await session(host);
  for (const p of fp) await wait(() => p.evaluate(version => window.__audience.state.state.version === version, r.state.version), 'spectator state differs');
  const before = r.state.version;
  const chat1 = fp[0].getByRole('region', { name: '观赛互动', exact: true });
  await chat1.getByRole('button', { name: '加油', exact: true }).click();
  await hp.getByRole('region', { name: '观赛互动', exact: true }).getByText(/观众 · 奶奶/).first().waitFor();
  const chat2 = fp[1].getByRole('region', { name: '观赛互动', exact: true });
  await chat2.getByRole('textbox', { name: '短消息', exact: true }).fill('帮我使用炸弹'); await chat2.getByRole('button', { name: '发送', exact: true }).click();
  await gp.getByText('帮我使用炸弹', { exact: true }).waitFor(); assert.equal((await session(host)).state.version, before);
  const duplicate = { id: crypto.randomUUID(), kind: 'text', text: '同一消息只发一次' };
  await post(fans[0], `/api/rooms/${roomID}/chat`, duplicate); const once = await post(fans[0], `/api/rooms/${roomID}/chat`, duplicate);
  assert.equal(once.audience.messages.filter(m => m.id === duplicate.id).length, 1);
  const v1 = await audienceView(fans[0]); const fromID = v1.audience.me.id;
  await post(host, `/api/rooms/${roomID}/chat/block`, { from_id: fromID, blocked: true });
  assert.ok(!(await session(host)).audience.messages.some(m => m.from_id === fromID)); assert.ok((await session(guest)).audience.messages.some(m => m.from_id === fromID));
  await post(host, `/api/rooms/${roomID}/chat/block`, { from_id: fromID, blocked: false });
  await hp.getByRole('button', { name: '禁止观众发言', exact: true }).click();
  await fp[0].getByText(/房主已关闭观众发言/).waitFor();
  const deniedChat = await fans[0].request.post(`/api/rooms/${roomID}/chat`, { data: { kind: 'emoji', emoji: '👏' } }); assert.equal((await deniedChat.json()).code, 'CHAT_DISABLED');
  await post(guest, `/api/rooms/${roomID}/chat`, { kind: 'text', text: '选手仍然可以互动' });
  await fp[1].getByText('选手仍然可以互动', { exact: true }).waitFor();
  await hp.getByRole('button', { name: '允许观众发言', exact: true }).click();
  report.push({ case: 'real launch and random dice synchronized; chat separate', dice: roll, version: before, duplicate_once: true, local_block: true, host_chat_control: true });

  r = await session(host);
  for (const action of ['roll', 'prop', 'pause', 'transfer_host', 'claim_host']) {
    const response = await fans[0].request.post(`/api/rooms/${roomID}/commands`, { data: { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, prop_id: 'bomb', target_player_id: 'player-0' } });
    assert.ok([401,403].includes(response.status()), `${action} was authorized`);
  }
  assert.equal((await session(host)).state.version, r.state.version);
  const foreign = await context(); assert.equal((await foreign.request.get(`/api/rooms/${roomID}/watch`)).status(), 401);
  assert.ok([401,403].includes((await fans[0].request.get(`/ws/voice?room_id=${roomID}`)).status()));
  const oldToken = r.audience.token;
  await hp.getByRole('button', { name: '更新链接', exact: true }).click();
  await wait(async () => (await session(host)).audience.token !== oldToken, 'watch token not rotated');
  assert.equal((await foreign.request.post(`/api/rooms/${roomID}/watch`, { data: { name: '旧链接观众', token: oldToken } })).status(), 400);
  assert.equal((await audienceView(fans[0])).audience.me.name, '奶奶');
  await command('pause', {}, host); for (const p of fp) await p.getByText('比赛暂停', { exact: true }).waitFor();
  await fp[0].evaluate(() => { dispatchEvent(new Event('offline')); });
  await command('resume', {}, host); let moving = await session(host); if (moving.state.phase === 'roll') await command('roll');
  await fp[0].evaluate(() => dispatchEvent(new Event('online')));
  await fp[0].getByText('正在重新连接观赛，比赛本身不受影响。', { exact: true }).waitFor({ state: 'detached' });
  assert.equal(await fp[0].getByRole('dialog', { name: '3D 掷骰结果' }).count(), 0, 'reconnect replayed old dice');
  await command('pause', {}, host);
  if ((await (await host.request.get('/api/config')).json()).voice_configured) {
    await fp[1].getByRole('button', { name: '开始提问', exact: true }).click(); await fp[1].getByText(/麦克风权限未开启/).waitFor();
    assert.equal((await session(host)).voice, 'disconnected');
  }
  await hp.getByRole('button', { name: '关闭观赛', exact: true }).click();
  for (const p of fp) await p.getByText(/房主已关闭观赛，棋局不受影响/).waitFor();
  assert.equal((await fans[0].request.get(`/api/rooms/${roomID}/watch`)).status(), 401);
  assert.equal((await session(host)).status, 'paused');
  await hp.getByRole('button', { name: '允许观赛', exact: true }).click();
  const newLink = await hp.getByRole('textbox', { name: '观赛链接', exact: true }).inputValue();
  for (let i = 0; i < 2; i++) { await fp[i].goto(newLink, { waitUntil: 'networkidle' }); await fp[i].reload({ waitUntil: 'networkidle' }); await fp[i].getByRole('textbox', { name: '你的昵称', exact: true }).fill(i ? '小姨' : '奶奶'); await fp[i].getByRole('button', { name: '开始观赛', exact: true }).click(); await fp[i].getByText('观赛中', { exact: true }).waitFor(); }
  report.push({ case: 'role guards, rotation, revoke, reconnect and permission denial', forged_actions_denied: true, reconnect_skips_old_roll: true, closed_revokes_access: true });
  await fp[0].screenshot({ path: join(out, 'watch-desktop.png'), fullPage: true });
  await fp[1].screenshot({ path: join(out, 'watch-mobile.png'), fullPage: true });
  assert.ok(await fp[1].evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile overflows');
  assert.ok(await fp[1].getByRole('textbox', { name: '短消息', exact: true }).evaluate(el => el.getBoundingClientRect().width >= 120), 'mobile message input collapsed');
  await fp[1].setViewportSize({ width: 844, height: 390 }); assert.ok(await fp[1].evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'landscape overflows'); await fp[1].setViewportSize({ width: 390, height: 844 });
  await command('resume', {}, host);
  const pending = new Set(['shield', 'bomb', 'boomerang', 'rescue', 'escort']);
  let steps = 0;
  for (; steps < 1400; steps++) {
    r = await session(host); const s = r.state; if (s.phase === 'finished') break;
    if (s.phase === 'move') { await command('move', { piece_id: [...s.legal_moves].sort((a,b) => b.to-a.to)[0].piece_id }); continue; }
    const current = s.players[s.current], other = s.players[1 - s.current];
    const main = p => p.progress > 0 && p.progress <= s.board.ring_length;
    if (!s.active_prop) {
      const id = [...pending].find(id => current.props[id] > 0 && (id === 'bomb' || id === 'boomerang' ? other.pieces.some(main) : current.pieces.some(p => main(p) && (!['shield','escort'].includes(id) || !p.shield))));
      if (id) {
        const owner = id === 'bomb' || id === 'boomerang' ? other : current;
        const piece = owner.pieces.find(p => main(p) && (!['shield','escort'].includes(id) || !p.shield));
        await command('prop', { prop_id: id, ...(id === 'escort' ? {} : { piece_id: piece.id }) }); pending.delete(id);
      }
    }
    await command('roll');
  }
  r = await session(host); assert.equal(r.state.phase, 'finished', 'natural game did not finish');
  // Current snapshots and finished presentation must converge without replaying an unbounded backlog.
  for (const p of fp) await p.locator('.watch-result').getByText(/获胜/).waitFor({ timeout: 25000 });
  await fp[0].screenshot({ path: join(out, 'watch-result.png'), fullPage: true });
  const oldGame = r.state.id;
  await command('replay', {}, host);
  for (const p of fp) { await p.getByText('等待开局', { exact: true }).waitFor(); assert.equal(await p.locator('.watch-result').count(), 0); }
  const next = await audienceView(fans[0]); assert.notEqual(next.state.id, oldGame); assert.equal(next.audience.messages?.length || 0, 0);
  await command('end', {}, host); for (const p of fp) await p.getByText(/房主结束了这一局/).waitFor();
  assert.equal(await fp[0].getByRole('button', { name: '发送', exact: true }).count(), 0);
  await fp[1].getByRole('button', { name: /^退出观赛/ }).click(); await wait(async () => (await session(host)).audience.count === 1, 'leave affected wrong watcher');
  const qa = await Promise.all(fp.map(p => p.evaluate(() => ({ mic: window.__audience.mic, contexts: window.__audience.contexts, losses: window.__audience.losses }))));
  assert.equal(qa[0].losses, 0); assert.equal(qa[1].losses, 0); assert.equal(qa[0].mic, 0);
  report.push({ case: 'natural victory, replay, manual end and leave', random_game_commands: steps, winner: r.state.winner, props_seen: ['launch', ...['shield','bomb','boomerang','rescue','escort'].filter(id => !pending.has(id))], webgl: qa, mobile_portrait_landscape: true });
  assert.deepEqual(errors, []);
  await writeFile(join(out, 'results.json'), JSON.stringify({ passed: true, scope: 'Actual desktop Chrome with isolated cookies and phone-sized viewports; random legal commands, no injected dice/state; no physical phone or live microphone acceptance', report, errors }, null, 2));
  console.log(JSON.stringify({ passed: true, report, errors }));
} catch (error) {
  for (let i = 0; i < pages.length; i++) if (!pages[i].isClosed()) await pages[i].screenshot({ path: join(out, `failure-${i}.png`), fullPage: true }).catch(() => {});
  throw error;
} finally { await browser.close(); }
