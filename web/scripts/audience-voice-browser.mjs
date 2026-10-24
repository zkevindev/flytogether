import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

assert.ok(process.env.QA_BASE_URL && process.env.QA_AUDIENCE_AUDIO_DIR, 'Set QA_BASE_URL and QA_AUDIENCE_AUDIO_DIR');
const base = process.env.QA_BASE_URL, out = process.env.QA_OUTPUT_DIR || 'test-results/audience-voice';
await mkdir(out, { recursive: true });
const browsers = [], errors = [];
const launch = async name => {
  const b = await chromium.launch({ channel: 'chrome', headless: true, args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream', '--autoplay-policy=no-user-gesture-required', '--use-file-for-fake-audio-capture=' + resolve(process.env.QA_AUDIENCE_AUDIO_DIR, name, 'browser-input.wav'), ...(process.platform === 'darwin' ? ['--use-angle=metal'] : [])] });
  browsers.push(b);
  const c = await b.newContext({ baseURL: base, permissions: ['microphone'], viewport: { width: 1360, height: 1000 } });
  await c.addInitScript(() => {
    localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-host-voice', 'off'); localStorage.setItem('fly-tutorial-v1', 'skipped'); localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' }));
    window.__voice = { events: [], tracks: [], binary: 0, audio: 0, scheduled: 0, nonSilent: 0, active: 0, interrupts: 0, endpoints: [], room: null };
    const media = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async (...args) => { const stream = await media(...args); window.__voice.tracks.push(...stream.getTracks()); return stream; };
    const original = AudioContext.prototype.createBufferSource;
    AudioContext.prototype.createBufferSource = function (...args) {
      const source = original.apply(this, args), start = source.start.bind(source);
      source.start = (...args) => {
        if (source.buffer) { window.__voice.scheduled++; if (source.buffer.getChannelData(0).some(x => Math.abs(x) > .01)) window.__voice.nonSilent++; window.__voice.active++; source.addEventListener('ended', () => window.__voice.active--, { once: true }); }
        return start(...args);
      }; return source;
    };
    const WS = WebSocket;
    window.WebSocket = class extends WS {
      constructor(url, ...args) {
        super(url, ...args);
        const voice = /\/ws\/(?:audience-voice|voice)\?/.test(String(url));
        if (voice) window.__voice.endpoints.push(new URL(url).pathname);
        this.addEventListener('message', event => {
          if (typeof event.data !== 'string') return;
          const e = JSON.parse(event.data);
          if (e.type === 'state') window.__voice.room = e.room;
          if (!voice) return;
          if (e.type.endsWith('audio.delta')) window.__voice.audio++;
          if (e.type === 'speech_started' && window.__voice.active > 0) window.__voice.interrupts++;
          if (['hello', 'stt', 'game_action', 'speech_started', 'error', 'response.created', 'response.done', 'response.audio_transcript.done', 'response.output_audio_transcript.done'].includes(e.type)) window.__voice.events.push({ type: e.type, text: e.text || e.transcript, response: e.response?.id || e.response_id, action: e.action, version: e.room?.state.version, at: Date.now(), current: window.__voice.room?.state.players[window.__voice.room.state.current].name });
        });
      }
      send(data) { if (data instanceof ArrayBuffer) window.__voice.binary++; return super.send(data); }
    };
  });
  return c;
};
const view = async p => p.evaluate(() => ({ events: window.__voice.events, binary: window.__voice.binary, audio: window.__voice.audio, scheduled: window.__voice.scheduled, nonSilent: window.__voice.nonSilent, interrupts: window.__voice.interrupts, endpoints: window.__voice.endpoints, tracks: window.__voice.tracks.map(t => t.readyState) }));
const page = async c => { const p = await c.newPage(); p.on('pageerror', e => errors.push(e.message)); return p; };
try {
  const h = await launch('host'), a = await launch('a'), b = await launch('b'), g = await browsers[0].newContext({ baseURL: base });
  const created = await h.request.post('/api/rooms', { data: { names: ['房主', '朋友'], room_name: '独立观赛语音验收', network: true, props: true, board: 'cross', mode: 'quick' } });
  assert.equal(created.status(), 201); let r = await created.json();
  assert.equal((await g.request.post(`/api/rooms/${r.id}/join`, { data: { invite: r.network.invite, name: '朋友' } })).status(), 200);
  const hp = await page(h), gp = await page(g), ap = await page(a), bp = await page(b);
  await hp.goto('/play', { waitUntil: 'networkidle' });
  // This page keeps the second player online; it never opens their microphone.
  await gp.addInitScript(() => { localStorage.setItem('fly-auto-voice', 'off'); localStorage.setItem('fly-host-voice', 'off'); localStorage.setItem('fly-tutorial-v1', 'skipped'); localStorage.setItem('fly-experience', JSON.stringify({ board: 'flat', motion: 'reduced' })); });
  await gp.goto('/play', { waitUntil: 'networkidle' });
  const link = await hp.getByRole('textbox', { name: '观赛链接', exact: true }).inputValue();
  for (const [p, name] of [[ap, '奶奶'], [bp, '小姨']]) { await p.goto(link, { waitUntil: 'networkidle' }); await p.getByRole('textbox', { name: '你的昵称', exact: true }).fill(name); await p.getByRole('button', { name: '开始观赛', exact: true }).click(); await p.getByText('观赛中', { exact: true }).waitFor(); }
  await gp.getByRole('button', { name: '我准备好了', exact: true }).click(); await hp.getByRole('button', { name: '开始游戏', exact: true }).click();
  await hp.getByRole('button', { name: /^道具口袋 ·/ }).click(); await hp.getByRole('button', { name: '使用起飞卡', exact: true }).click(); await hp.locator('.prop-targets .prop-target').first().click();
  // The card occupies the current pre-roll prop slot. Make one ordinary legal
  // move, then advance naturally until this player's next prop slot is free.
  const command = async (action, extra = {}) => { const s = (await (await h.request.get('/api/session')).json()).room; const c = s.state.current === 0 ? h : g; const res = await c.request.post(`/api/rooms/${r.id}/commands`, { data: { command_id: crypto.randomUUID(), game_id: s.state.id, expected_version: s.state.version, action, ...extra } }); assert.equal(res.status(), 200); return res.json(); };
  for (let i = 0; i < 100; i++) { const s = (await (await h.request.get('/api/session')).json()).room.state; if (s.current === 0 && s.phase === 'roll' && !s.active_prop) break; if (s.phase === 'roll') await command('roll'); else await command('move', { piece_id: s.legal_moves[0].piece_id }); }
  r = (await (await h.request.get('/api/session')).json()).room;
  const rollsBefore = r.state.players[0].stats.rolls;
  assert.equal(r.state.current, 0); assert.equal(r.state.phase, 'roll');
  // Reload discards setup animations, preserving the actual state and seats.
  await hp.reload({ waitUntil: 'networkidle' });
  await hp.getByRole('checkbox', { name: '自动连接语音，直接说话', exact: true }).check();
  await hp.getByRole('button', { name: '关闭对讲麦克风', exact: true }).waitFor({ timeout: 25000 });
  for (const p of [ap, bp]) { await p.getByRole('button', { name: '开始提问', exact: true }).click(); await p.getByText('正在听你说', { exact: true }).waitFor({ timeout: 25000 }); }
  // A successful throw can immediately pass the turn and close the player's
  // microphone before a separate voice receipt arrives. The committed state
  // and real ASR are the authority for this check.
  const until = Date.now() + 40000;
  while (Date.now() < until) { r = (await (await h.request.get('/api/session')).json()).room; if (r.state.players[0].props.luck === 0) break; await new Promise(resolve => setTimeout(resolve, 150)); }
  r = (await (await h.request.get('/api/session')).json()).room;
  assert.equal(r.state.players[0].stats.rolls, rollsBefore + 1); assert.equal(r.state.players[0].props.luck, 0); assert.equal(r.state.dice_rolls.length, 2);
  const stableVersion = r.state.version;
  for (const p of [ap, bp]) await p.waitForFunction(() => window.__voice.events.some(e => e.type.endsWith('audio_transcript.done')), null, { timeout: 35000 });
  for (const p of [ap, bp]) {
    const v = await view(p), first = v.events.find(e => e.type === 'stt'), reply = v.events.find(e => e.type.endsWith('audio_transcript.done'));
    assert.ok(first && reply && first.current && reply.text.includes(first.current), JSON.stringify({ first, reply }));
    assert.ok(v.binary > 0 && v.audio > 0 && v.nonSilent > 0, 'no real AudioWorklet upload/playback');
    assert.deepEqual(v.endpoints, ['/ws/audience-voice']);
  }
  // The audio fixture interrupts a rule explanation with a new board question,
  // then asks for a forbidden write. No transcripts are injected by this script.
  await ap.waitForFunction(() => window.__voice.events.some(e => e.type === 'stt' && /炸弹/.test(e.text)), null, { timeout: 90000 });
  await ap.waitForFunction(() => { const e = window.__voice.events, i = e.findLastIndex(e => e.type === 'stt' && /炸弹/.test(e.text)); return i >= 0 && e.slice(i + 1).some(e => e.type.endsWith('audio_transcript.done')); }, null, { timeout: 30000 });
  await bp.waitForFunction(() => window.__voice.events.filter(e => e.type.endsWith('audio_transcript.done')).length >= 3, null, { timeout: 30000 });
  const av = await view(ap), bv = await view(bp), hv = await view(hp);
  assert.ok(av.interrupts >= 1, 'synthetic speech did not actually interrupt playing audio');
  assert.ok(!av.events.some(e => e.text?.includes('橘子')) && !bv.events.some(e => e.text?.includes('蓝莓')), 'private dialogue crossed sessions');
  assert.equal(av.events.filter(e => e.type === 'game_action').length, 0); assert.equal(bv.events.filter(e => e.type === 'game_action').length, 0);
  for (const v of [av, bv, hv]) assert.equal(v.events.filter(e => e.type === 'error').length, 0);
  const bombIndex = av.events.findLastIndex(e => e.type === 'stt' && /炸弹/.test(e.text)), refusal = av.events.slice(bombIndex + 1).find(e => e.type.endsWith('audio_transcript.done')).text;
  assert.match(refusal, /不能|无法|只.*(?:观赛|讲解|回答)|不可以|没有.*权限/);
  const latest = (await (await h.request.get('/api/session')).json()).room; assert.equal(latest.state.version, stableVersion); assert.equal(latest.state.players[0].stats.rolls, rollsBefore + 1); assert.equal(latest.state.players[0].props.bomb, 1);
  await ap.screenshot({ path: join(out, 'watch-private-voice.png'), fullPage: true });
  const result = { passed: true, scope: 'Three separate Chrome processes, real AudioWorklet/Go/Lingxi Realtime, synthetic macOS Tingting WAV; PCM scheduled and non-silent in browser, not a human hearing/physical microphone claim', game_id: latest.state.id, version: latest.state.version, dice: latest.state.dice_rolls, a: av, b: bv, player: hv, errors };
  // Revocation must release the spectator media without touching the player.
  await h.request.post(`/api/rooms/${latest.id}/audience`, { data: { enabled: false } });
  for (const p of [ap, bp]) await p.waitForFunction(() => window.__voice.tracks.length > 0 && window.__voice.tracks.every(t => t.readyState === 'ended'));
  assert.equal((await (await h.request.get('/api/session')).json()).room.state.version, stableVersion);
  await writeFile(join(out, 'results.json'), JSON.stringify(result, null, 2)); console.log(JSON.stringify({ passed: true, version: latest.state.version, dice: latest.state.dice_rolls, private_sessions: 2, actual_interruptions: av.interrupts, errors }));
} catch (error) {
  for (let i = 0; i < browsers.length; i++) { let j = 0; for (const c of browsers[i].contexts()) for (const p of c.pages()) { await writeFile(join(out, `failure-${i}-${j}-voice.json`), JSON.stringify(await view(p).catch(() => ({})), null, 2)); await p.screenshot({ path: join(out, `failure-${i}-${j++}.png`), fullPage: true }).catch(() => {}); } }
  throw error;
} finally { for (const b of browsers) await b.close(); }
