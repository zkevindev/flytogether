import { useEffect, useRef, useState } from "react";
import {
  ArrowClockwise,
  ArrowLeft,
  ArrowRight,
  Check,
  House,
  LockKey,
  SignOut,
  MagnifyingGlass,
} from "@phosphor-icons/react";
import type { Game } from "../lib/types";
import "./admin.css";

type Match = {
  game_id: string;
  room_id: string;
  name: string;
  status: string;
  version: number;
  seq: number;
  network: boolean;
  recorded_from: string;
  updated_at: string;
  first_version: number;
  state?: Game;
};
type Entry = {
  id: number;
  at: string;
  room_id: string;
  game_id: string;
  version: number;
  kind: string;
  source: string;
  session_id?: string;
  input_id?: string;
  ref?: string;
  text?: string;
  action?: string;
  code?: string;
  duration_ms?: number;
};
type Settings = {
  enabled: boolean;
  transcripts: boolean;
  retention_hours: number;
  write_failures: number;
};
const statusName: Record<string, string> = {
  waiting: "等待开始",
  playing: "对局中",
  paused: "已暂停",
  ended: "主动结束",
  finished: "自然获胜",
};
const kindName: Record<string, string> = {
  game: "棋局事件",
  command: "操作已保存",
  user_text: "玩家转写",
  assistant_text: "助手回复",
  tool: "语音工具",
  connection: "语音连接",
  error: "异常",
  tts: "回合播报",
  first_audio: "首段音频到达",
};
const actionName: Record<string, string> = {
  roll: "掷骰子",
  move: "移动飞机",
  prop: "使用道具",
  start: "开始",
  pause: "暂停",
  resume: "继续",
  end: "结束",
  replay: "同配置再玩",
  room_update: "房间更新 / 机器人",
  recording_started: "记录起点",
  connecting: "开始连接",
  connected: "上游已连接",
  closed: "连接关闭",
  connection_failed: "连接结束 / 异常",
  roll_dice: "语音掷骰",
  move_piece: "语音移动",
  use_prop: "语音道具",
  get_game_state: "查询棋局",
  coach_move: "教练建议",
  confirm_coach: "确认建议",
};
const codeName: Record<string, string> = {
  completed: "完成",
  read_only: "未执行写操作",
  rejected: "拒绝执行",
  closed: "已关闭",
  synthesized: "已合成并返回；播放状态未知",
  stale_result: "回合已变化，丢弃音频",
  synthesis_failed: "合成失败",
  quiet: "本设备只问才答",
  not_configured: "语音未配置",
  upstream_error: "上游语音错误",
};
const time = (value: string) =>
  new Date(value).toLocaleString("zh-CN", { hour12: false });
class RequestError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
async function call<T>(path: string, options?: RequestInit): Promise<T> {
  const signal = options?.signal
    ? AbortSignal.any([options.signal, AbortSignal.timeout(12000)])
    : AbortSignal.timeout(12000);
  const r = await fetch("/api/admin" + path, {
    ...options,
    signal,
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json", ...options?.headers },
  });
  const data = await r.json();
  if (!r.ok) throw new RequestError(data.message || "请求失败", r.status);
  return data;
}
export default function Admin() {
  const [auth, setAuth] = useState(false),
    [checking, setChecking] = useState(true),
    [key, setKey] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [settings, setSettings] = useState<Settings>(),
    [rows, setRows] = useState<Match[]>([]),
    [total, setTotal] = useState(0),
    [page, setPage] = useState(0),
    [search, setSearch] = useState(""),
    [query, setQuery] = useState(""),
    [status, setStatus] = useState(""),
    [selected, setSelected] = useState(""),
    [match, setMatch] = useState<Match>(),
    [events, setEvents] = useState<Entry[]>([]),
    [before, setBefore] = useState(0),
    [kind, setKind] = useState(""),
    [revision, setRevision] = useState(0),
    [listLoading, setListLoading] = useState(false),
    [detailLoading, setDetailLoading] = useState(false),
    [olderLoading, setOlderLoading] = useState(false),
    [refreshed, setRefreshed] = useState("");
  const generation = useRef(0);
  const fail = (e: unknown) => {
    if (e instanceof DOMException && e.name === "AbortError") return;
    if (e instanceof RequestError && e.status === 401) {
      setAuth(false);
      setRows([]);
      setMatch(undefined);
      setEvents([]);
      setSelected("");
      setSettings(undefined);
    }
    setError(e instanceof Error ? e.message : "请求失败，请重试");
  };
  useEffect(() => {
    const controller = new AbortController();
    call<{ diagnostics: Settings }>("/session", { signal: controller.signal })
      .then((x) => {
        setSettings(x.diagnostics);
        setAuth(true);
      })
      .catch((e) => {
        if (e instanceof RequestError && e.status === 401) return;
        fail(e);
      })
      .finally(() => {
        if (!controller.signal.aborted) setChecking(false);
      });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (!auth) return;
    const c = new AbortController();
    setListLoading(true);
    setError("");
    const params = new URLSearchParams({
      q: query,
      status,
      limit: "25",
      offset: String(page * 25),
    });
    Promise.all([
      call<{ matches: Match[]; total: number }>("/matches?" + params, {
        signal: c.signal,
      }),
      call<{ diagnostics: Settings }>("/session", { signal: c.signal }),
    ])
      .then(([x, s]) => {
        setSettings(s.diagnostics);
        setRows(x.matches);
        setTotal(x.total);
        setRefreshed(new Date().toISOString());
      })
      .catch(fail)
      .finally(() => {
        if (!c.signal.aborted) setListLoading(false);
      });
    return () => c.abort();
  }, [auth, page, query, status, revision]);
  useEffect(() => {
    generation.current++;
    if (!auth || !selected) {
      setMatch(undefined);
      setEvents([]);
      setBefore(0);
      return;
    }
    const c = new AbortController();
    setDetailLoading(true);
    setMatch(undefined);
    setEvents([]);
    setBefore(0);
    setError("");
    Promise.all([
      call<Match>("/matches/" + encodeURIComponent(selected), {
        signal: c.signal,
      }),
      call<{ events: Entry[]; next_before: number }>(
        "/matches/" +
          encodeURIComponent(selected) +
          "/events?" +
          new URLSearchParams({ kind, limit: "50" }),
        { signal: c.signal },
      ),
    ])
      .then(([m, e]) => {
        setMatch(m);
        setEvents(e.events);
        setBefore(e.next_before);
      })
      .catch(fail)
      .finally(() => {
        if (!c.signal.aborted) setDetailLoading(false);
      });
    return () => c.abort();
  }, [auth, selected, kind, revision]);
  const login = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await call("/login", { method: "POST", body: JSON.stringify({ key }) });
      const s = await call<{ diagnostics: Settings }>("/session");
      setSettings(s.diagnostics);
      setAuth(true);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
      setKey("");
    }
  };
  const logout = async () => {
    setBusy(true);
    try {
      await call("/logout", { method: "POST", body: "{}" });
      setAuth(false);
      setRows([]);
      setEvents([]);
      setMatch(undefined);
      setSelected("");
      setSettings(undefined);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const older = async () => {
    const g = generation.current;
    setOlderLoading(true);
    try {
      const out = await call<{ events: Entry[]; next_before: number }>(
        "/matches/" +
          encodeURIComponent(selected) +
          "/events?" +
          new URLSearchParams({ kind, limit: "50", before: String(before) }),
      );
      if (g === generation.current) {
        setEvents((old) => [
          ...old,
          ...out.events.filter((x) => !old.some((y) => x.id === y.id)),
        ]);
        setBefore(out.next_before);
      }
    } catch (e) {
      if (g === generation.current) fail(e);
    } finally {
      setOlderLoading(false);
    }
  };
  return (
    <div className="admin-app">
      <header className="admin-header">
        <a href="/" className="admin-brand">
          <House />
          一起飞
        </a>
        <span className="admin-header-title">
          <LockKey />
          运行与排障
        </span>
        {auth && (
          <div className="admin-header-actions">
            <button
              onClick={() => setRevision((x) => x + 1)}
              disabled={listLoading || detailLoading}
            >
              <ArrowClockwise />
              刷新记录
            </button>
            <button onClick={logout} disabled={busy}>
              <SignOut />
              退出
            </button>
          </div>
        )}
      </header>
      <main className="admin-main">
        {checking ? (
          <p role="status">正在检查管理会话…</p>
        ) : !auth ? (
          <section className="admin-login">
            <span className="admin-eyebrow">PRIVATE OPERATIONS</span>
            <h1>
              查看一次对局，
              <br />
              找回问题的来龙去脉。
            </h1>
            <p>
              使用服务器配置的独立管理员密钥登录。玩家身份不能访问对局与语音排障记录。
            </p>
            <form onSubmit={login}>
              <label htmlFor="admin-key">管理员密钥</label>
              <input
                id="admin-key"
                type="password"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                required
                autoComplete="off"
                spellCheck={false}
              />
              <button className="primary" disabled={busy || !key}>
                {busy ? "正在验证…" : "登录管理后台"}
                <ArrowRight />
              </button>
            </form>
            <small>
              会话有效期8小时，重启服务后需要重新登录。密钥仅用于本次验证，不写入浏览器存储。
            </small>
          </section>
        ) : (
          <>
            <div className="admin-intro">
              <div>
                <span className="admin-eyebrow">MATCH DIAGNOSTICS</span>
                <h1>对局与语音记录</h1>
                <p>先按房间或对局编号定位，再对照真实棋步、转写和回复。</p>
              </div>
              <div className="admin-policy">
                <Check />
                只读排障
                <span>
                  {settings?.transcripts ? "记录对话文本" : "仅记录元数据"} ·
                  保留
                  {settings
                    ? Math.round((settings.retention_hours / 24) * 10) / 10
                    : "—"}
                  天 · 不保存录音
                </span>
              </div>
            </div>
            {settings && settings.write_failures > 0 && (
              <p className="admin-warning">
                当前进程已有{settings.write_failures}
                次语音排障记录写入失败，时间线可能不完整。棋局写入仍由存档事务校验。
              </p>
            )}
            <section className="admin-tools" aria-label="对局筛选">
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setQuery(search);
                  setPage(0);
                  setSelected("");
                  setRevision((x) => x + 1);
                }}
              >
                <label htmlFor="admin-search">
                  房间名 / 房间编号 / 对局编号
                </label>
                <div>
                  <input
                    id="admin-search"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="例如：周末家庭机场"
                    maxLength={160}
                  />
                  <button>
                    <MagnifyingGlass />
                    查询
                  </button>
                </div>
              </form>
              <label>
                对局状态
                <select
                  aria-label="对局状态"
                  value={status}
                  onChange={(e) => {
                    setStatus(e.target.value);
                    setPage(0);
                    setSelected("");
                  }}
                >
                  <option value="">全部状态</option>
                  {Object.entries(statusName).map(([v, t]) => (
                    <option key={v} value={v}>
                      {t}
                    </option>
                  ))}
                </select>
              </label>
            </section>
            <div className="admin-layout">
              <section className="admin-list" aria-label="对局列表">
                <div className="admin-section-heading">
                  <h2>
                    对局记录 <span>{total}</span>
                  </h2>
                  <small>
                    {listLoading
                      ? "加载中…"
                      : refreshed
                        ? "更新于 " + time(refreshed)
                        : ""}
                  </small>
                </div>
                {!listLoading && rows.length === 0 && (
                  <div className="admin-empty">
                    <h3>没有匹配记录</h3>
                    <p>
                      更换筛选，或开始一局新游戏。启用前的语音无法补回，超过保留期的记录不会显示。
                    </p>
                  </div>
                )}
                {rows.map((m) => (
                  <button
                    className={
                      "admin-match " +
                      (selected === m.game_id ? "selected" : "")
                    }
                    key={m.game_id}
                    onClick={() => setSelected(m.game_id)}
                    aria-pressed={selected === m.game_id}
                  >
                    <span className="admin-match-top">
                      <strong>{m.name}</strong>
                      <span className={"admin-status " + m.status}>
                        {statusName[m.status] || m.status}
                      </span>
                    </span>
                    <span>
                      {m.network ? "联网" : "同屏"} · 版本 {m.version} ·{" "}
                      {time(m.updated_at)}
                    </span>
                    <code>对局 {m.game_id}</code>
                    <code>房间 {m.room_id}</code>
                  </button>
                ))}
                <div className="admin-pagination">
                  <button
                    disabled={page === 0 || listLoading}
                    onClick={() => {
                      setPage((x) => x - 1);
                      setSelected("");
                    }}
                  >
                    <ArrowLeft />
                    上一页
                  </button>
                  <span>第{page + 1}页</span>
                  <button
                    disabled={(page + 1) * 25 >= total || listLoading}
                    onClick={() => {
                      setPage((x) => x + 1);
                      setSelected("");
                    }}
                  >
                    下一页
                    <ArrowRight />
                  </button>
                </div>
              </section>
              <section className="admin-detail" aria-label="对局详情">
                {detailLoading ? (
                  <p role="status">正在读取棋局与时间线…</p>
                ) : !match ? (
                  <div className="admin-empty">
                    <h2>选择一局，展开详情</h2>
                    <p>
                      对局重开后上一局仍保留。这里显示服务器确认的状态，不能据此推断声音已在玩家设备播放。
                    </p>
                  </div>
                ) : (
                  <>
                    <div className="admin-section-heading">
                      <h2>{match.name}</h2>
                      <span className={"admin-status " + match.status}>
                        {statusName[match.status]}
                      </span>
                    </div>
                    <dl className="admin-facts">
                      <div>
                        <dt>对局编号</dt>
                        <dd>
                          <code>{match.game_id}</code>
                        </dd>
                      </div>
                      <div>
                        <dt>房间编号</dt>
                        <dd>
                          <code>{match.room_id}</code>
                        </dd>
                      </div>
                      <div>
                        <dt>记录起点</dt>
                        <dd>
                          {time(match.recorded_from)} · 版本{" "}
                          {match.first_version}
                        </dd>
                      </div>
                      <div>
                        <dt>最新状态</dt>
                        <dd>
                          版本 {match.version} · {match.state?.phase} · 第
                          {match.state?.turn}回合
                        </dd>
                      </div>
                    </dl>
                    <p className="admin-note">
                      时间为服务器记录时间。记录起点前、超过保留期的事件以及断开时未完成的回复可能缺失；旧局快照不代表拥有完整历史。
                    </p>
                    {match.state && (
                      <>
                        <div className="admin-player-scroll">
                          <table>
                            <caption>
                              真实棋局快照 · {match.state.board.name} ·{" "}
                              {match.state.ruleset}
                            </caption>
                            <thead>
                              <tr>
                                <th>玩家</th>
                                <th>投掷</th>
                                <th>起飞</th>
                                <th>抵达</th>
                                <th>送回</th>
                                <th>道具</th>
                              </tr>
                            </thead>
                            <tbody>
                              {match.state.players.map((p, i) => (
                                <tr key={p.id}>
                                  <th>
                                    {p.name}
                                    {p.ai ? " · 机器人" : ""}
                                    {i === match.state?.current
                                      ? " · 当前"
                                      : ""}
                                  </th>
                                  <td>{p.stats.rolls}</td>
                                  <td>{p.stats.takeoffs}</td>
                                  <td>{p.stats.arrivals}</td>
                                  <td>{p.stats.captures}</td>
                                  <td>{p.stats.props_used}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                        <details className="admin-snapshot">
                          <summary>展开飞机、库存与完整棋局快照</summary>
                          <pre>{JSON.stringify(match.state, null, 2)}</pre>
                        </details>
                      </>
                    )}
                    <div className="admin-section-heading">
                      <h3>
                        事件时间线 <small>最新在上</small>
                      </h3>
                      <label>
                        类型
                        <select
                          aria-label="事件类型"
                          value={kind}
                          onChange={(e) => setKind(e.target.value)}
                        >
                          <option value="">全部事件</option>
                          <option value="voice">所有语音</option>
                          {Object.entries(kindName).map(([v, t]) => (
                            <option key={v} value={v}>
                              {t}
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>
                    {events.length === 0 && (
                      <p className="admin-note">
                        此筛选下暂无记录。未连接语音时不会生成对话记录；关闭文本记录时仅有元数据。
                      </p>
                    )}
                    <ol className="admin-timeline">
                      {events.map((e) => (
                        <li
                          key={e.id}
                          className={e.kind === "error" ? "has-error" : ""}
                        >
                          <div className="admin-event-head">
                            <strong>{kindName[e.kind] || e.kind}</strong>
                            <time>{time(e.at)}</time>
                            <span>v{e.version}</span>
                          </div>
                          <div className="admin-event-tags">
                            {e.action && (
                              <span>{actionName[e.action] || e.action}</span>
                            )}
                            {e.code && (
                              <span>{codeName[e.code] || e.code}</span>
                            )}
                            <span>{e.source}</span>
                            {!!e.duration_ms && <span>{e.duration_ms} ms</span>}
                          </div>
                          {e.text ? (
                            <p>{e.text}</p>
                          ) : (
                            (e.kind === "assistant_text" ||
                              e.kind === "user_text" ||
                              e.kind === "tts") && (
                              <p className="admin-note">
                                没有可用文本（文本记录关闭、静默或上游未提供最终文本）。
                              </p>
                            )
                          )}
                          {e.session_id && (
                            <small>
                              语音会话 <code>{e.session_id}</code>
                            </small>
                          )}
                          {e.input_id && (
                            <small>
                              关联发言 <code>{e.input_id}</code>
                            </small>
                          )}
                          {e.ref && (
                            <small>
                              请求 / 事件 <code>{e.ref}</code>
                            </small>
                          )}
                        </li>
                      ))}
                    </ol>
                    {before > 0 && (
                      <button
                        className="admin-older"
                        onClick={older}
                        disabled={olderLoading}
                      >
                        {olderLoading ? "加载中…" : "加载更早记录"}
                      </button>
                    )}
                  </>
                )}
              </section>
            </div>
          </>
        )}
        {error && (
          <div className="admin-error" role="alert">
            {error}
          </div>
        )}
      </main>
    </div>
  );
}
