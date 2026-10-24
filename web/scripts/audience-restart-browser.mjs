import { chromium } from 'playwright';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import assert from 'node:assert/strict';

assert.ok(process.env.QA_BASE_URL && process.env.QA_RESTART_SCRIPT, 'Set QA_BASE_URL and an explicitly authorized QA_RESTART_SCRIPT');
const base = process.env.QA_BASE_URL, out = process.env.QA_OUTPUT_DIR || 'test-results/audience-restart';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ channel: 'chrome', headless: true });
const errors = [];
try {
  const contexts = [], pages = [];
  for (let i = 0; i < 4; i++) {
    const c = await browser.newContext({ baseURL: base }); contexts.push(c);
    await c.addInitScript(() => { localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-host-voice', 'off'); localStorage.setItem('fly-tutorial-v1', 'skipped'); localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' })); navigator.mediaDevices.getUserMedia = async () => { throw Error('No microphone in restart QA'); }; });
  }
  const [h, g, a, b] = contexts;
  let r = await (await h.request.post('/api/rooms', { data: { network: true, names: ['重启房主', '家人'], room_name: '观赛恢复验收', props: true, mode: 'quick' } })).json();
  const id = r.id, token = r.audience.token;
  assert.equal((await g.request.post(`/api/rooms/${id}/join`, { data: { invite: r.network.invite, name: '家人' } })).status(), 200);
  for (const [i, c] of [a,b].entries()) assert.equal((await c.request.post(`/api/rooms/${id}/watch`, { data: { token, name: i ? '小姨' : '奶奶' } })).status(), 200);
  for (let i = 0; i < 4; i++) { const p = await contexts[i].newPage(); pages.push(p); p.on('pageerror', e => errors.push(e.message)); await p.goto(i < 2 ? '/play' : '/#watch=' + id + '.' + token, { waitUntil: 'networkidle' }); }
  const state = async () => (await (await h.request.get('/api/session')).json()).room;
  const cmd = async action => { r = await state(); const res = await h.request.post(`/api/rooms/${id}/commands`, { data: { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action } }); assert.equal(res.status(), 200); return res.json(); };
  await pages[1].getByRole('button', { name: '我准备好了', exact: true }).click(); await cmd('start'); await cmd('roll'); await cmd('pause');
  const message = { id: crypto.randomUUID(), kind: 'text', text: '重启后继续看这一局' };
  assert.equal((await a.request.post(`/api/rooms/${id}/chat`, { data: message })).status(), 200);
  const saved = await state();
  await promisify(execFile)(process.env.QA_RESTART_PYTHON || 'python3', [process.env.QA_RESTART_SCRIPT], { timeout: 45000, maxBuffer: 65536 });
  let ready=false;
  for(let i=0;i<150;i++){if(await h.request.get('/readyz').then(r=>r.ok()).catch(()=>false)){ready=true;break;}await new Promise(resolve=>setTimeout(resolve,150));}
  assert.ok(ready,'restarted service did not become ready');
  for (const p of pages) await p.reload({ waitUntil: 'networkidle' });
  r = await state(); assert.equal(r.status, 'paused'); assert.notEqual(r.instance_id, saved.instance_id); assert.deepEqual(r.state, saved.state); assert.equal(r.audience.count, 2);
  for (const [i, c] of [a,b].entries()) {
    const v = await (await c.request.get(`/api/rooms/${id}/watch`)).json();
    assert.equal(v.audience.me.name, i ? '小姨' : '奶奶'); assert.equal(v.network.invite, ''); assert.ok(!v.me && !v.profile_ids && !v.audience.token);
    assert.equal(v.audience.messages.filter(m => m.id === message.id).length, 1);
    await pages[i+2].getByText('比赛暂停', { exact: true }).waitFor(); assert.equal(await pages[i+2].getByRole('dialog', { name: '3D 掷骰结果' }).count(), 0);
  }
  await pages[2].screenshot({ path: join(out, 'watch-restored.png'), fullPage: true });
  assert.equal((await h.request.post(`/api/rooms/${id}/audience`, { data: { enabled: false } })).status(), 200);
  for (const p of pages.slice(2)) await p.getByText(/房主已关闭观赛/).waitFor();
  for (const c of [a,b]) assert.equal((await c.request.get(`/api/rooms/${id}/watch`)).status(), 401);
  assert.equal((await state()).state.version, saved.state.version); assert.deepEqual(errors, []);
  const result = { passed: true, actual_service_restart: true, spectators: 2, players: 2, same_board_dice_stats: true, same_roles: true, messages_persist_once: true, no_old_roll_animation: true, subsequent_revocation: true, errors };
  await writeFile(join(out, 'results.json'), JSON.stringify(result, null, 2)); console.log(JSON.stringify(result));
} finally { await browser.close(); }
