import { chromium } from 'playwright';
import assert from 'node:assert/strict';

const baseURL = process.env.QA_BASE_URL;
assert.ok(baseURL, 'Set QA_BASE_URL');
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const context = await browser.newContext({ baseURL, viewport: { width: 1280, height: 900 } });
await context.addInitScript(() => {
  localStorage.setItem('fly-auto-voice', 'off');
  localStorage.setItem('fly-host-voice', 'off');
  localStorage.setItem('fly-tutorial-v1', 'skipped');
});

try {
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/play', { waitUntil: 'networkidle' });
  let room = (await (await context.request.get('/api/session')).json()).room;
  room = await (await context.request.post(`/api/rooms/${room.id}/matches`, { data: {
    names: ['甲', '乙'], board: 'cross', mode: 'quick', ai: false, props: true, waiting: false,
  } })).json();
  await page.reload({ waitUntil: 'networkidle' });
  const toggle = page.getByRole('checkbox', { name: '只问不操作（语音不执行棋局操作）' });
  await toggle.check();
  assert.equal(await page.locator('.roll-button').isEnabled(), true, 'read-only preference must not disable click actions');
  await page.reload({ waitUntil: 'networkidle' });
  assert.equal(await toggle.isChecked(), true, 'seat 0 preference should survive refresh');

  const command = async action => {
    const state = room.state;
    const response = await context.request.post(`/api/rooms/${room.id}/commands`, { data: {
      command_id: crypto.randomUUID(), game_id: state.id, expected_version: state.version,
      action, ...(action === 'move' ? { piece_id: state.legal_moves[0].piece_id } : {}),
    } });
    const result = await response.json();
    assert.equal(response.status(), 200, JSON.stringify(result));
    return result;
  };
  const switchTo = async target => {
    for (let i = 0; room.state.current !== target && i < 20; i++) {
      room = await command(room.state.phase === 'roll' ? 'roll' : 'move');
    }
    assert.equal(room.state.current, target, `could not reach seat ${target}`);
    await page.reload({ waitUntil: 'networkidle' });
  };
  await switchTo(1);
  assert.equal(await page.getByRole('checkbox', { name: '只问不操作（语音不执行棋局操作）' }).isChecked(), false, 'seat 1 must keep its own default');
  await page.getByRole('checkbox', { name: '只问不操作（语音不执行棋局操作）' }).check();
  await switchTo(0);
  assert.equal(await page.getByRole('checkbox', { name: '只问不操作（语音不执行棋局操作）' }).isChecked(), true, 'seat 0 preference must survive seat switches');
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ result: 'PASS', persistedAcrossRefresh: true, independentSeats: true, twoHumanSeats: true, pageErrors: errors }));
} finally {
  await context.close();
  await browser.close();
}
