import { chromium } from "playwright";
import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
assert.ok(
  process.env.QA_BASE_URL && process.env.QA_ADMIN_KEY_FILE,
  "Set QA_BASE_URL and private QA_ADMIN_KEY_FILE",
);
const out = process.env.QA_OUTPUT_DIR || "test-results/admin-client-error";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  channel: "chrome",
  headless: true,
  args: process.platform === "darwin" ? ["--use-angle=metal"] : [],
});
try {
  const c = await browser.newContext({
    baseURL: process.env.QA_BASE_URL,
    viewport: { width: 1280, height: 1000 },
  });
  // Browser-level permission denial, without replacing getUserMedia or the diagnostics endpoint.
  const p = await c.newPage();
  p.setDefaultTimeout(15000);
  const cdp = await c.newCDPSession(p);
  const target = await cdp.send("Target.getTargetInfo");
  await cdp.send("Browser.setPermission", {
    browserContextId: target.targetInfo.browserContextId,
    permission: { name: "microphone" },
    setting: "denied",
    origin: process.env.QA_BASE_URL,
  });
  await c.addInitScript(() => {
    localStorage.setItem("fly-tutorial-v1", "skipped");
    localStorage.setItem("fly-host-voice", "off");
    localStorage.setItem("fly-auto-voice", "off");
    localStorage.setItem(
      "fly-experience",
      JSON.stringify({ board: "flat", motion: "reduced" }),
    );
  });
  const result = await c.request.post("/api/rooms", {
    data: {
      room_name: "权限拒绝实测",
      names: ["孩子", "家长"],
      waiting: false,
      board: "cross",
      mode: "quick",
      difficulty: "normal",
      props: true,
      ai: false,
    },
  });
  assert.equal(result.status(), 201);
  const room = await result.json();
  const config = await (await c.request.get("/api/config")).json();
  assert.ok(config.voice_configured && config.diagnostics?.enabled);
  let reports = 0;
  p.on("request", (r) => {
    if (r.url().endsWith("/diagnostics") && r.method() === "POST") reports++;
  });
  await p.goto("/play");
  const button = p.getByRole("button", { name: "开启自动语音", exact: true });
  await button.waitFor();
  await button.click();
  await p.getByRole("button", { name: "点击启用麦克风并重试" }).waitFor();
  assert.ok(
    (await p.locator(".voice-panel").innerText()).includes("麦克风权限未开启"),
  );
  const key = (await readFile(process.env.QA_ADMIN_KEY_FILE, "utf8")).trim();
  assert.equal(
    (await c.request.post("/api/admin/login", { data: { key } })).status(),
    200,
  );
  const events = async () =>
    await (
      await c.request.get(
        "/api/admin/matches/" + room.state.id + "/events?kind=error",
      )
    ).json();
  if (process.env.QA_EXPECT_MISSING === "1") {
    await p.waitForTimeout(500);
    assert.equal(reports, 0);
    assert.equal((await events()).events.length, 0);
    console.log(
      "REPRODUCED: real browser permission failure visible but stale config prevented reporting",
    );
  } else {
    await assertEventually(async () => {
      const data = await events();
      assert.ok(
        data.events.some(
          (e) =>
            e.code === "microphone_denied" &&
            e.source === "browser" &&
            e.game_id === room.state.id &&
            e.version === room.state.version,
        ),
      );
    });
    assert.ok(reports > 0);
    await writeFile(
      out + "/results.json",
      JSON.stringify(
        {
          passed: true,
          scope:
            "Chrome CDP microphone permission denied; original getUserMedia and real diagnostic HTTP endpoint; no audio capture or injected fault log",
          client_error_reported: true,
          game_unchanged: true,
        },
        null,
        2,
      ),
    );
    console.log(
      "PASS: actual browser microphone denial persisted through real UI callback and is readable in private admin timeline",
    );
  }
  const after = (await (await c.request.get("/api/session")).json()).room.state;
  assert.deepEqual(after, room.state);
  await c.close();
} finally {
  await browser.close();
}
async function assertEventually(f) {
  let last;
  for (let i = 0; i < 30; i++) {
    try {
      await f();
      return;
    } catch (e) {
      last = e;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw last;
}
