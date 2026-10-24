import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { readFile, mkdir } from 'node:fs/promises';

const baseURL = process.env.QA_BASE_URL;
assert.ok(baseURL, 'Set QA_BASE_URL');
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: process.platform === 'darwin' ? ['--use-angle=metal'] : [] });
const context = await browser.newContext({ baseURL, viewport: { width: 1280, height: 1000 }, acceptDownloads: true, permissions: ['clipboard-read', 'clipboard-write'] });
await context.addInitScript(() => {
  localStorage.setItem('fly-auto-voice', 'off');
  localStorage.setItem('fly-host-voice', 'off');
  localStorage.setItem('fly-tutorial-v1', 'skipped');
});
const output = process.env.QA_OUTPUT_DIR || '/tmp/flytogether-issue45-qa';
await mkdir(output, { recursive: true });

try {
  const page = await context.newPage();
  page.setDefaultTimeout(20000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/play', { waitUntil: 'networkidle' });
  let room = (await (await context.request.get('/api/session')).json()).room;
  room = await (await context.request.post(`/api/rooms/${room.id}/matches`, { data: {
    names: ['小朋友', '爸爸'], board: 'cross', mode: 'quick', ai: false, props: true, waiting: false,
  } })).json();
  const command = async (action, pieceID = '') => {
    const state = room.state;
    const response = await context.request.post(`/api/rooms/${room.id}/commands`, { data: {
      command_id: crypto.randomUUID(), game_id: state.id, expected_version: state.version, action,
      ...(pieceID ? { piece_id: pieceID } : {}),
    } });
    const result = await response.json();
    assert.equal(response.status(), 200, JSON.stringify(result));
    room = result;
  };
  for (let turn = 0; !room.state.winner && turn < 260; turn++) {
    if (room.state.phase === 'roll') await command('roll');
    else if (room.state.phase === 'move' && room.state.legal_moves[0]) await command('move', room.state.legal_moves[0].piece_id);
    else throw new Error(`Unexpected game state: ${room.state.phase}`);
  }
  assert.ok(room.state.winner, 'quick game did not finish within the action limit');
  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('region', { name: '本局结算' }).waitFor();
  const card = page.getByText('制作家庭回顾卡', { exact: true });
  await card.click();
  const preview = page.getByRole('img', { name: /获胜的家庭飞行棋回顾卡/ });
  await preview.waitFor();
  await page.screenshot({ path: `${output}/quick-winner-card.png`, fullPage: true });
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('button', { name: '保存回顾图片' }).click(),
  ]);
  assert.equal(download.suggestedFilename(), '一起飞-家庭回顾卡.png');
  const pngPath = `${output}/family-recap.png`;
  await download.saveAs(pngPath);
  const bytes = await readFile(pngPath);
  assert.deepEqual([...bytes.subarray(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10], 'download should be a valid PNG');
  await page.getByRole('button', { name: '复制分享文字' }).click();
  const text = await page.evaluate(() => navigator.clipboard.readText());
  assert.match(text, /小朋友|爸爸/);
  assert.match(text, /家庭十字棋盘|经典/);
  assert.doesNotMatch(text, /private|cookie|token|邀请链接|语音原文/i);
  assert.deepEqual(errors, []);

  await page.getByRole('button', { name: '同配置再玩一局' }).click();
  await page.getByRole('region', { name: '本局结算' }).waitFor({ state: 'detached' });
  assert.equal(await page.getByText('制作家庭回顾卡', { exact: true }).count(), 0, 'old recap must not appear in a new game');

  room = (await (await context.request.get('/api/session')).json()).room;
  const state = room.state;
  const ended = await context.request.post(`/api/rooms/${room.id}/commands`, { data: {
    command_id: crypto.randomUUID(), game_id: state.id, expected_version: state.version, action: 'end',
  } });
  assert.equal(ended.status(), 200);
  room = await ended.json();
  assert.equal(room.state.winner, undefined, 'manual end must not invent a champion');
  await page.reload({ waitUntil: 'networkidle' });
  assert.equal(await page.getByRole('region', { name: '本局结算' }).count(), 0);
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ result: 'PASS', quickWinner: true, pngValid: true, textCopied: true, recapBoundToGame: true, manualEndHasNoWinner: true, pageErrors: errors }));
} finally {
  await context.close();
  await browser.close();
}
