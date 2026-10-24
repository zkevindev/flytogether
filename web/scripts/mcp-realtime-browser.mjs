import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

assert.ok(process.env.QA_BASE_URL && process.env.QA_AUDIO_FILE, 'Set QA_BASE_URL and QA_AUDIO_FILE');
const mode = process.env.QA_MCP_MODE || 'coop';
assert.ok(['quick', 'classic', 'coop'].includes(mode), 'QA_MCP_MODE must be quick, classic or coop');
const expectedMCPTool = process.env.QA_MCP_EXPECT_TOOL || (mode === 'coop' ? 'get_mission_progress' : 'get_game_summary');
const outputDir = process.env.QA_OUTPUT_DIR || 'test-results/mcp-realtime';
await mkdir(outputDir, { recursive: true });
const browser = await chromium.launch({
  channel: 'chrome',
  headless: true,
  args: [
    '--use-fake-device-for-media-stream',
    '--use-fake-ui-for-media-stream',
    '--autoplay-policy=no-user-gesture-required',
    '--use-file-for-fake-audio-capture=' + resolve(process.env.QA_AUDIO_FILE),
    ...(process.platform === 'darwin' ? ['--use-angle=metal'] : []),
  ],
});

try {
  const context = await browser.newContext({
    baseURL: process.env.QA_BASE_URL,
    permissions: ['microphone'],
    viewport: { width: 1360, height: 980 },
  });
  await context.addInitScript(() => {
    localStorage.setItem('fly-tutorial-v1', 'skipped');
    localStorage.setItem('fly-auto-voice', 'off');
    localStorage.setItem('fly-host-voice', 'off');
    localStorage.setItem('fly-companion', JSON.stringify({ style: 'concise', frequency: 'quiet' }));
    window.__mcpQA = { events: [], tracks: [] };
    const original = WebSocket;
    window.WebSocket = class extends original {
      constructor(url, ...args) {
        super(url, ...args);
        if (!String(url).includes('/ws/voice?')) return;
        this.addEventListener('message', event => {
          if (typeof event.data !== 'string') return;
          let message;
          try { message = JSON.parse(event.data); } catch { return; }
          const record = { at: Date.now(), type: message.type };
          if (message.status) record.status = message.status;
          if (message.tool) record.tool = message.tool;
          if (message.name) record.tool = message.name;
          if (message.tool_name) record.tool = message.tool_name;
          if (message.text || message.transcript) record.text = message.text || message.transcript;
          if (message.response?.status) record.responseStatus = message.response.status;
          if (message.item?.type) record.itemType = message.item.type;
          if (message.item?.name) record.toolName = message.item.name;
          if (message.error?.code) record.errorCode = message.error.code;
          if (message.error?.message) record.errorMessage = message.error.message.slice(0, 300);
          if (message.type === 'mcp_list_tools.failed') {
            record.failureKeys = Object.keys(message);
            if (message.error && typeof message.error === 'object') {
              record.errorKeys = Object.keys(message.error);
              for (const key of ['code', 'type', 'message']) if (message.error[key]) record['error' + key[0].toUpperCase() + key.slice(1)] = String(message.error[key]).slice(0, 300);
            }
          }
          window.__mcpQA.events.push(record);
        });
      }
    };
    const get = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async (...args) => {
      const stream = await get(...args);
      window.__mcpQA.tracks.push(...stream.getTracks());
      return stream;
    };
  });

  const page = await context.newPage();
  const pageErrors = [];
  page.on('pageerror', error => pageErrors.push(error.message));
  await page.goto('/', { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: '创建游戏', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '创建家庭房间', exact: true });
  await dialog.getByRole('button', { name: '2 位', exact: true }).click();
  await dialog.getByRole('checkbox', { name: /添加机器人陪玩/ }).uncheck();
  if (mode === 'coop') {
    await dialog.getByRole('button', { name: /星河救援 · 合作挑战/ }).click();
    await dialog.locator('#mission-card').selectOption('signal-network');
  } else if (mode === 'classic') {
    await dialog.getByRole('button', { name: /完整飞行/ }).click();
  }
  await dialog.getByRole('button', { name: '创建并开始游戏', exact: true }).click();
  if (mode === 'coop') await page.getByRole('region', { name: '星际救援任务' }).waitFor();

  const snapshot = async () => (await (await context.request.get('/api/session')).json()).room;
  const before = await snapshot();
  assert.equal(before.state.mode, mode);
  if (mode === 'coop') {
    assert.equal(before.state.theme_id, 'star-rescue');
    assert.equal(before.state.mission.id, 'signal-network');
    assert.equal(before.state.mission.progress, 0);
  } else {
    assert.equal(before.state.mission, undefined, 'a standard mode should have no optional mission');
  }
  assert.ok(before.state.version >= 1);

  await page.getByRole('checkbox', { name: '自动连接语音，直接说话', exact: true }).check();
  await page.getByRole('button', { name: '关闭对讲麦克风', exact: true }).waitFor({ timeout: 30000 });
  await page.waitForFunction(() => window.__mcpQA.events.some(e => e.type === 'mcp_status' && ['loaded', 'unavailable'].includes(e.status)), null, { timeout: 30000 });
  const toolStatus = await page.evaluate(() => window.__mcpQA.events.findLast(e => e.type === 'mcp_status')?.status);
  assert.equal(toolStatus, 'loaded', `Realtime MCP tool import failed: ${JSON.stringify(await page.evaluate(() => window.__mcpQA.events))}`);
  await page.waitForFunction(() => window.__mcpQA.events.some(e => e.type === 'stt'), null, { timeout: 45000 });
  await page.waitForFunction(() => window.__mcpQA.events.some(e => e.type === 'mcp_call_status' && ['completed', 'failed'].includes(e.status)), null, { timeout: 60000 }).catch(async error => {
    const events = await page.evaluate(() => window.__mcpQA.events);
    throw new Error(`Realtime did not finish an MCP call: ${JSON.stringify({ events, cause: error.message })}`);
  });
  const mcpCallStatus = await page.evaluate(() => window.__mcpQA.events.findLast(e => e.type === 'mcp_call_status')?.status);
  assert.equal(mcpCallStatus, 'completed', `Realtime MCP call failed: ${JSON.stringify(await page.evaluate(() => window.__mcpQA.events))}`);
  await page.waitForFunction(() => window.__mcpQA.events.some(e => e.type === 'response.output_audio_transcript.done' || e.type === 'response.audio_transcript.done'), null, { timeout: 45000 });
  await page.getByRole('button', { name: '关闭对讲麦克风', exact: true }).click();
  await page.waitForFunction(() => window.__mcpQA.tracks.every(track => track.readyState === 'ended'));

  const events = await page.evaluate(() => window.__mcpQA.events);
  const userText = events.find(event => event.type === 'stt')?.text || '';
  const assistantText = events.filter(event => /response\.(output_audio|audio)_transcript\.done|response\.output_text\.done/.test(event.type)).map(event => event.text || '').join('');
  if (mode === 'coop') assert.match(userText, /任务|救援/);
  else assert.match(userText, /轮到|谁|幸运骰|规则|飞机|走/);
  assert.ok(assistantText.length > 0, 'Realtime should produce a spoken answer after the MCP query');
  assert.ok(events.some(event => event.type === 'mcp_call_status' && event.status === 'completed'), 'Realtime must complete a real native MCP call, not only answer from context');
  const actualMCPTool = events.find(event => event.type === 'mcp_call_status' && event.status === 'completed' && event.tool)?.tool || events.find(event => event.type === 'response.mcp_call_arguments.done' && event.tool)?.tool;
  assert.equal(actualMCPTool, expectedMCPTool, `Realtime must call ${expectedMCPTool}: ${JSON.stringify(events)}`);
  const after = await snapshot();
  assert.equal(after.state.id, before.state.id);
  assert.equal(after.state.version, before.state.version, 'read-only MCP must not mutate the game');
  assert.equal(after.state.mission?.progress, before.state.mission?.progress);
  assert.deepEqual(pageErrors, []);

  const result = {
    passed: true,
    game_id: before.state.id,
    room_id: before.id,
    mode,
    theme_id: before.state.theme_id || null,
    mission_id: before.state.mission?.id || null,
    mission_progress: after.state.mission?.progress ?? null,
    game_version_unchanged: true,
    mcp_tools_loaded: true,
    mcp_call_completed: true,
    mcp_tool: expectedMCPTool,
    recognized_query: userText,
    spoken_answer: assistantText,
    event_types: events.map(event => event.type),
    page_errors: pageErrors,
  };
  await writeFile(join(outputDir, 'mcp-realtime-results.json'), JSON.stringify(result, null, 2));
  console.log(JSON.stringify({ ...result, spoken_answer: assistantText.slice(0, 300) }));
} finally {
  await browser.close();
}
