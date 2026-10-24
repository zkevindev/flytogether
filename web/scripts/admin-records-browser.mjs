import { chromium } from "playwright";
import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
assert.ok(
  process.env.QA_BASE_URL && process.env.QA_ADMIN_KEY_FILE,
  "Set QA_BASE_URL and private QA_ADMIN_KEY_FILE",
);
const out = process.env.QA_OUTPUT_DIR || "test-results/admin-records";
await mkdir(out, { recursive: true });
const key = (await readFile(process.env.QA_ADMIN_KEY_FILE, "utf8")).trim();
const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  const c = await browser.newContext({
    baseURL: process.env.QA_BASE_URL,
    viewport: { width: 1440, height: 1100 },
  });
  let mic = 0;
  const p = await c.newPage();
  p.setDefaultTimeout(15000);
  await p.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = async () => {
      throw Error("Admin cannot use microphone");
    };
  });
  const errors = [];
  p.on("pageerror", (e) => errors.push(e.message));
  assert.equal(
    (await c.request.post("/api/admin/login", { data: { key } })).status(),
    200,
  );
  const list = await (
    await c.request.get(
      "/api/admin/matches?q=" + encodeURIComponent("直接对话联调"),
    )
  ).json();
  let match, events;
  for (const m of list.matches) {
    const r = await (
      await c.request.get(
        "/api/admin/matches/" + m.game_id + "/events?limit=100",
      )
    ).json();
    if (
      r.events.filter((x) => x.kind === "user_text").length >= 6 &&
      r.events.filter((x) => x.kind === "assistant_text" && x.text).length >= 6
    ) {
      match = m;
      events = r.events;
      break;
    }
  }
  assert.ok(
    match && events,
    "Run test:voice:realtime first; real completed dialogues required",
  );
  const users = events.filter((e) => e.kind === "user_text");
  const assistants = events.filter((e) => e.kind === "assistant_text");
  const tools = events.filter((e) => e.kind === "tool");
  const audio = events.filter((e) => e.kind === "first_audio");
  for (const user of users) {
    assert.ok(user.input_id && user.session_id);
    assert.ok(
      assistants.some(
        (e) =>
          e.input_id === user.input_id &&
          e.session_id === user.session_id &&
          e.text,
      ),
    );
    assert.ok(tools.some((e) => e.input_id === user.input_id));
    assert.ok(
      audio.some((e) => e.input_id === user.input_id && e.duration_ms >= 0),
    );
  }
  await p.goto("/admin");
  await p.getByLabel("房间名 / 房间编号 / 对局编号").fill(match.game_id);
  await p.getByRole("button", { name: "查询", exact: true }).click();
  await p.locator(".admin-match").filter({ hasText: match.game_id }).click();
  await p.getByLabel("事件类型", { exact: true }).selectOption("voice");
  await p
    .locator(".admin-timeline li")
    .filter({ hasText: "玩家转写" })
    .first()
    .waitFor();
  await p.screenshot({ path: join(out, "admin-voice.png"), fullPage: true });
  const before = (
    await (await c.request.get("/api/admin/matches/" + match.game_id)).json()
  ).version;
  await p.getByRole("button", { name: "刷新记录" }).click();
  await p
    .locator(".admin-timeline li")
    .filter({ hasText: "助手回复" })
    .first()
    .waitFor();
  const after = (
    await (await c.request.get("/api/admin/matches/" + match.game_id)).json()
  ).version;
  assert.equal(before, after);
  assert.deepEqual(errors, []);
  assert.equal(mic, 0);
  const report = {
    passed: true,
    source:
      "Actual completed real Lingxi dialogues from synthetic WAV, protected admin API and actual Chrome UI",
    user_text: users.length,
    assistant_text: assistants.length,
    tools: tools.length,
    first_audio: audio.length,
    all_inputs_correlated: true,
    read_only_version_unchanged: true,
    errors,
  };
  await writeFile(join(out, "results.json"), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
  await c.close();
} finally {
  await browser.close();
}
