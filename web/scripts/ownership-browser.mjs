import { chromium } from "playwright";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
assert.ok(process.env.QA_BASE_URL, "Set QA_BASE_URL");
const base = process.env.QA_BASE_URL,
  out = process.env.QA_OUTPUT_DIR || "test-results/ownership";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  channel: "chrome",
  headless: true,
  args: process.platform === "darwin" ? ["--use-angle=metal"] : [],
});
const contexts = [],
  errors = [];
const context = async (mobile = false) => {
  const c = await browser.newContext({
    baseURL: base,
    viewport: mobile
      ? { width: 390, height: 844 }
      : { width: 1360, height: 1000 },
  });
  contexts.push(c);
  await c.addInitScript(() => {
    localStorage.setItem("fly-auto-voice", "off");
    localStorage.setItem("fly-host-voice", "off");
    localStorage.setItem("fly-tutorial-v1", "skipped");
    localStorage.setItem(
      "fly-experience",
      JSON.stringify({ board: "flat", motion: "reduced" }),
    );
    window.__hostMic = 0;
    navigator.mediaDevices.getUserMedia = async () => {
      window.__hostMic++;
      throw Error("Click-only ownership QA");
    };
  });
  return c;
};
const page = async (c) => {
  const p = await c.newPage();
  p.setDefaultTimeout(15000);
  p.on("pageerror", (e) => errors.push(e.message));
  return p;
};
const session = async (c) =>
  (await (await c.request.get("/api/session")).json()).room;
const wait = async (f, message, timeout = 15000) => {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    if (await f()) return;
    await new Promise((r) => setTimeout(r, 150));
  }
  throw Error(message);
};
const command = async (c, action, extra = {}) => {
  const r = await session(c);
  return c.request.post("/api/rooms/" + r.id + "/commands", {
    data: {
      command_id: crypto.randomUUID(),
      game_id: r.state.id,
      expected_version: r.state.version,
      action,
      ...extra,
    },
  });
};
try {
  const h = await context(),
    g = await context(true),
    stranger = await context();
  let hp = await page(h),
    gp = await page(g);
  const create = await h.request.post("/api/rooms", {
    data: {
      room_name: "房主交接试玩机场",
      names: ["家长", "等待朋友", "灵矽机器人"],
      network: true,
      ai: true,
      difficulty: "easy",
      board: "cross",
      mode: "quick",
      props: true,
    },
  });
  assert.equal(create.status(), 201);
  let room = await create.json();
  const roomID = room.id,
    invite = room.network.invite;
  await hp.goto("/play");
  await hp.getByRole("region", { name: "联网准备区" }).waitFor();
  await gp.goto("/#join=" + roomID + "." + invite);
  await gp.getByLabel("你的昵称").fill("朋友");
  await gp.getByRole("button", { name: "加入房间", exact: true }).click();
  await gp.getByRole("region", { name: "联网准备区" }).waitFor();
  await wait(
    async () => (await session(h)).network.seats[1].online,
    "guest did not become online",
  );
  await hp.getByRole("button", { name: "房间管理", exact: true }).click();
  let dialog = hp.getByRole("dialog", { name: "房间管理" });
  await dialog.waitFor();
  assert.ok(await dialog.locator("input[value=player-0]").isDisabled());
  assert.ok(await dialog.locator("input[value=player-2]").isDisabled());
  await dialog.locator("input[value=player-1]").check();
  await dialog.getByRole("button", { name: "转让房主", exact: true }).click();
  await hp.screenshot({
    path: join(out, "handover-confirm.png"),
    fullPage: false,
  });
  await dialog.getByRole("button", { name: "先不交接" }).click();
  assert.equal((await session(h)).me.host, true);
  // A selected receiver going offline must invalidate confirmation rather than transfer anyway.
  await gp.close();
  await wait(
    async () => !(await session(h)).network.seats[1].online,
    "guest offline state",
  );
  await wait(
    () => dialog.locator("input[value=player-1]").isDisabled(),
    "offline guest still selectable",
  );
  gp = await page(g);
  await gp.goto("/play");
  await wait(
    async () => (await session(h)).network.seats[1].online,
    "guest reconnection",
  );
  await wait(
    async () => !(await dialog.locator("input[value=player-1]").isDisabled()),
    "reconnected guest unavailable",
  );
  const before = (await session(h)).state;
  await dialog.locator("input[value=player-1]").check();
  await dialog.getByRole("button", { name: "转让房主", exact: true }).click();
  await dialog.getByRole("button", { name: "确认交接房主" }).click();
  await wait(async () => (await session(g)).me.host, "transfer not committed");
  await dialog.waitFor({ state: "hidden" });
  assert.deepEqual((await session(h)).state, before);
  await wait(
    async () =>
      await gp
        .getByRole("button", { name: "开始游戏", exact: true })
        .isVisible(),
    "new host start control missing",
  );
  assert.equal(
    await hp.getByRole("button", { name: "开始游戏", exact: true }).count(),
    0,
  );
  assert.equal((await (await command(h, "start")).json()).code, "HOST_ONLY");
  await gp.screenshot({
    path: join(out, "handover-mobile.png"),
    fullPage: true,
  });
  assert.ok(
    await gp.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  );
  await gp.getByRole("button", { name: "开始游戏", exact: true }).click();
  await wait(
    async () => (await session(h)).status === "playing",
    "new host cannot start",
  );
  await hp.getByRole("button", { name: /道具口袋/ }).click();
  await hp.getByRole("button", { name: "使用起飞卡", exact: true }).click();
  await wait(
    async () =>
      await hp
        .locator(".play-turn-actions")
        .getByRole("button", { name: "掷骰子", exact: true })
        .isEnabled(),
    "original seat lost roll rights",
  );
  await hp
    .locator(".play-turn-actions")
    .getByRole("button", { name: "掷骰子", exact: true })
    .click();
  await Promise.all(
    [hp, gp].map((p) =>
      p.getByRole("dialog", { name: "3D 掷骰结果" }).waitFor(),
    ),
  );
  const dice = (await session(h)).state.dice;
  await wait(
    async () =>
      (await hp.getByRole("dialog", { name: "3D 掷骰结果" }).count()) === 0,
    "dice did not automatically finish",
  );
  await hp
    .locator(".play-turn-actions")
    .getByRole("button", { name: "小熊号", exact: true })
    .click();
  await hp
    .locator(".move-preview")
    .getByRole("button", { name: /确认.*飞行/ })
    .click();
  await wait(
    async () => (await session(h)).state.players[0].stats.takeoffs === 1,
    "original seat could not take off",
  );
  const inPlay = (await session(h)).state;
  const transferInPlay = async (p, target) => {
    await p.getByRole("button", { name: "房间管理", exact: true }).click();
    const d = p.getByRole("dialog", { name: "房间管理" });
    await d.locator("input[value=" + target + "]").check();
    await d.getByRole("button", { name: "转让房主", exact: true }).click();
    await d.getByRole("button", { name: "确认交接房主" }).click();
    await d.waitFor({ state: "hidden" });
  };
  await transferInPlay(gp, "player-0");
  await wait(
    async () => (await session(h)).me.host,
    "active-game transfer did not reach first seat",
  );
  assert.deepEqual((await session(h)).state, inPlay);
  await transferInPlay(hp, "player-1");
  await wait(
    async () => (await session(g)).me.host,
    "active-game transfer back failed",
  );
  assert.deepEqual((await session(g)).state, inPlay);
  await gp.getByRole("button", { name: "暂停", exact: true }).click();
  await wait(
    async () => (await session(h)).status === "paused",
    "new host cannot pause",
  );
  // Closing just one of a host's tabs cannot enable a takeover.
  let otherHostTab = await page(g);
  await otherHostTab.goto("/play");
  await gp.close();
  await wait(
    async () => (await session(h)).network.host_online,
    "remaining host tab not counted",
  );
  assert.equal(
    (await (await command(h, "claim_host")).json()).code,
    "HOST_ONLINE",
  );
  await otherHostTab.close();
  await wait(
    async () => !!(await session(h)).network.host_claim_at,
    "missing absence grace",
  );
  await hp.getByRole("button", { name: "房间管理", exact: true }).click();
  dialog = hp.getByRole("dialog", { name: "房间管理" });
  await dialog.getByRole("region", { name: "房主接任状态" }).waitFor();
  assert.ok(
    await dialog
      .getByRole("button", { name: "接任房主", exact: true })
      .isDisabled(),
  );
  assert.equal(
    (await (await command(h, "claim_host")).json()).code,
    "HOST_GRACE_PERIOD",
  );
  await hp.screenshot({ path: join(out, "host-grace.png"), fullPage: true });
  // Reconnection cancels this grace period; the next full absence starts a fresh one.
  gp = await page(g);
  await gp.goto("/play");
  await wait(
    async () => (await session(h)).network.host_online,
    "host did not reappear",
  );
  assert.equal((await session(h)).network.host_claim_at, undefined);
  const absentAt = Date.now();
  await gp.close();
  const settings = await (await h.request.get("/api/config")).json();
  await wait(
    async () => !!(await session(h)).network.can_claim_host,
    "host grace did not expire",
    Math.max(20000, (settings.host_reclaim_seconds + 10) * 1000),
  );
  const waitedMS = Date.now() - absentAt;
  assert.ok(
    waitedMS >= settings.host_reclaim_seconds * 1000 - 2500,
    "claim granted before continuous absence grace",
  );
  await wait(
    async () =>
      await dialog
        .getByRole("button", { name: "接任房主", exact: true })
        .isEnabled(),
    "eligible claim button missing",
  );
  await dialog.getByRole("button", { name: "接任房主", exact: true }).click();
  await hp.screenshot({
    path: join(out, "host-reclaim-confirm.png"),
    fullPage: false,
  });
  const paused = (await session(h)).state;
  await dialog.getByRole("button", { name: "确认交接房主" }).click();
  await wait(async () => (await session(h)).me.host, "explicit reclaim failed");
  assert.deepEqual((await session(h)).state, paused);
  assert.equal((await session(h)).network.invite, invite);
  gp = await page(g);
  await gp.goto("/play");
  await gp.getByRole("region", { name: "联网准备区" }).waitFor();
  assert.equal((await session(g)).me.player_id, "player-1");
  assert.equal((await session(g)).me.host, false);
  assert.equal((await (await command(g, "resume")).json()).code, "HOST_ONLY");
  assert.equal(
    (
      await stranger.request.post("/api/rooms/" + roomID + "/commands", {
        data: {
          command_id: crypto.randomUUID(),
          game_id: paused.id,
          expected_version: paused.version,
          action: "claim_host",
        },
      })
    ).status(),
    401,
  );
  await hp
    .getByRole("banner")
    .getByRole("button", { name: "继续游戏", exact: true })
    .click();
  await wait(
    async () => (await session(h)).status === "playing",
    "reclaimed host cannot resume",
  );
  await hp.reload();
  await wait(async () => (await session(h)).me.host, "refresh lost management");
  await hp.screenshot({
    path: join(out, "reclaimed-room.png"),
    fullPage: true,
  });
  assert.equal(await hp.evaluate(() => window.__hostMic), 0);
  assert.equal(await gp.evaluate(() => window.__hostMic), 0);
  assert.deepEqual(errors, []);
  const report = {
    passed: true,
    results: [
      "real invite join; explicit transfer/cancel/offline target validation",
      "new host controls; original seat rolls real dice and takes off",
      "multi-tab presence; reconnect cancels grace; full real-time absence then explicit claim",
      "former host returns to same seat without management; stranger denied",
      "metadata-only handover preserves board, dice, inventory, invite and game ID",
      "390px responsive management and no microphone requests",
    ],
    actual_die: dice,
    grace_seconds: settings.host_reclaim_seconds,
    actual_absence_wait_ms: waitedMS,
    errors,
  };
  await writeFile(join(out, "results.json"), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
} finally {
  await Promise.all(contexts.map((c) => c.close()));
  await browser.close();
}
