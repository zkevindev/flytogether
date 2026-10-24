import { chromium } from 'playwright';
import assert from 'node:assert/strict';

const baseURL = process.env.QA_BASE_URL;
assert.ok(baseURL, 'Set QA_BASE_URL');
const browser = await chromium.launch({ channel: 'chrome', headless: true });

try {
  const context = await browser.newContext({ baseURL, viewport: { width: 390, height: 844 } });
  await context.addInitScript(() => {
    localStorage.setItem('fly-tutorial-v1', 'completed');
    localStorage.setItem('fly-auto-voice', 'off');
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(new URL('/play', baseURL).href, { waitUntil: 'networkidle' });

  let session = await (await page.request.get('/api/session')).json();
  const room = session.room;
  assert.ok(room?.id, 'a local room is available');
  const response = await page.request.post(`/api/rooms/${room.id}/matches`, {
    data: { names: ['小朋友', '爸爸'], board: 'cross', mode: 'quick', ai: false, props: false, waiting: false },
  });
  assert.equal(response.status(), 200);
  await page.reload({ waitUntil: 'networkidle' });

  const prompt = page.locator('.pass-device-prompt');
  await prompt.waitFor({ state: 'visible' });
  assert.match(await prompt.innerText(), /把手机交给小朋友/);
  assert.match(await prompt.innerText(), /现在掷骰子/);
  assert.equal(await page.locator('.pass-device-prompt').count(), 1);

  let passedToDad = false;
  for (let turn = 0; turn < 24 && !passedToDad; turn++) {
    session = await (await page.request.get('/api/session')).json();
    const state = session.room.state;
    const text = await prompt.innerText();
    assert.match(text, new RegExp(state.players[state.current].name));
    assert.match(text, state.phase === 'move' ? /现在选一架飞机/ : /现在掷骰子/);

    if (state.phase === 'roll') {
      await page.locator('.board-turnbar .roll-button').click();
      const dice = page.getByRole('dialog', { name: '3D 掷骰结果' });
      await dice.waitFor({ state: 'visible' });
      const diceBox = await dice.boundingBox();
      assert.ok(diceBox);
      const dialogIsTopmost = await page.evaluate(box => {
        const target = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
        return !!target?.closest('dialog[open]');
      }, diceBox);
      assert.ok(dialogIsTopmost, 'the native dice dialog must stay above the handoff prompt');
      await dice.getByRole('button', { name: '继续飞行' }).click();
    }

    session = await (await page.request.get('/api/session')).json();
    const afterRoll = session.room.state;
    if (afterRoll.phase === 'move') {
      await page.waitForFunction(() => document.querySelector('.pass-device-prompt')?.textContent?.includes('现在选一架飞机'));
      assert.match(await prompt.innerText(), /把手机交给小朋友/);
      const move = afterRoll.legal_moves[0];
      await page.getByRole('button', { name: move.label, exact: true }).click();
      const preview = page.getByRole('region', { name: '飞行落点预览' });
      await preview.waitFor({ state: 'visible' });
      await preview.getByRole('button', { name: '确认飞行' }).click();
      await page.waitForTimeout(500);
    }

    session = await (await page.request.get('/api/session')).json();
    passedToDad = session.room.state.current === 1;
    if (passedToDad) {
      await page.waitForFunction(() => document.querySelector('.pass-device-prompt')?.textContent?.includes('把手机交给爸爸'));
      assert.match(await prompt.innerText(), /现在掷骰子/);
    }
  }

  assert.ok(passedToDad, 'the prompt advances to the next real player');

  const network = await page.request.post('/api/rooms/new', { data: {
    room_name: '轮转提示验收', names: ['房主', '灵矽机器人'], board: 'cross', mode: 'quick',
    difficulty: 'easy', ai: true, network: true, props: false, waiting: true,
  } });
  assert.equal(network.status(), 201);
  const networkRoom = await network.json();
  await page.reload({ waitUntil: 'networkidle' });
  await page.locator('.lobby-panel').waitFor({ state: 'visible' });
  await page.waitForFunction(() => !document.body.textContent?.includes('正在同步棋局'));
  await page.request.post(`/api/rooms/${networkRoom.id}/ready`, { data: { ready: true } });
  const started = await page.request.post(`/api/rooms/${networkRoom.id}/commands`, { data: {
    command_id: crypto.randomUUID(), game_id: networkRoom.state.id, expected_version: networkRoom.state.version, action: 'start',
  } });
  assert.equal(started.status(), 200, await started.text());
  await page.reload({ waitUntil: 'networkidle' });
  await page.getByText('邀请房间', { exact: false }).waitFor({ state: 'visible' });
  assert.equal(await prompt.count(), 0, 'network players should not see a pass-device prompt');
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ result: 'PASS', turnsUntilHandoff: session.room.state.turn, networkPromptCount: await prompt.count(), errors }));
} finally {
  await browser.close();
}
