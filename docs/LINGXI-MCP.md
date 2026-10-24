# 灵矽 Realtime 原生 MCP 接入核对

核对日期：2026-10-24，关联[#37](https://github.com/zkevindev/flytogether/issues/37)。已用本机配置连接真实灵矽 `/v1/realtime`，不是模拟上游；没有修改平台Agent，也没有将游戏控制接口公开成MCP。

## 结论与实测

灵矽 Realtime **可以原生触发远端 MCP**。在 `session.update.session.tools` 中加入 `type: "mcp"`，平台负责导入并调用远端工具，不需要客户端把它伪装成function再执行。配置形式参考 [OpenAI官方Realtime工具文档](https://developers.openai.com/api/docs/guides/realtime-mcp)。兼容性以下述实测为准。

实测用不需要认证的 [DeepWiki公开MCP](https://docs.devin.ai/work-with-devin/deepwiki-mcp)，限制只导入 `read_wiki_structure`，仅查询公开 `golang/go` 仓库目录，不发送用户对话、私人仓库或游戏信息。成功观察到：

1. `session.updated` 返回 `type: mcp`。
2. `mcp_list_tools.in_progress` → `mcp_list_tools.completed`，`conversation.item.done` 的 `mcp_list_tools` 列出允许的工具及schema。
3. `response.mcp_call_arguments.done` 得到 `{"repoName":"golang/go"}`。
4. `response.mcp_call.in_progress` → `response.mcp_call.completed`。
5. `conversation.item.done` 的 `mcp_call` 有output、无error；再请求生成结果摘要，文本回复完成。

本次 `/mcp`（Streamable HTTP）成功；同服务 `/sse` 导入失败，另一个公开文档MCP地址导入也失败，失败事件未提供具体错误。不能由此推断所有SSE服务都不支持，或所有远端服务都兼容。

## 配置示例

下例是上述只读能力验证的形状；已有的音频、模型等配置继续由服务端提供。API Key只放服务端 `.env`，不在网页发送。演示服务不作为游戏运行依赖。

```json
{
  "type": "session.update",
  "session": {
    "tools": [{
      "type": "mcp",
      "server_label": "public_docs",
      "server_url": "https://mcp.deepwiki.com/mcp",
      "allowed_tools": ["read_wiki_structure"],
      "require_approval": "never"
    }],
    "tool_choice": "auto"
  }
}
```

`require_approval: never`只用于本次限制工具的公开只读测试。带写操作或第三方账户的服务，需要独立审批策略和服务端凭据；不能照抄成全量免审批。此次未验证authorization/自定义headers、approval请求、断线恢复及多MCP并行。

## 必须处理的异步顺序

真实会话中，第一次 `response.done(status=completed)` 出现在 `response.mcp_call.completed` **之前**。它只表示该次生成已结束，不等于远端工具完成。

集成时按item_id跟踪工具导入、调用与结果，不能第一次response.done就关闭连接、释放该轮或立即播报成功。等工具返回并检查error后，再发送 `response.create`（结果摘要阶段 `tool_choice: none`）生成回复。本次这样完成了工具结果后的文本摘要。不能发送 `function_call_output`冒充原生MCP结果。语音播放、实体麦克风打断与MCP并发仍需专门验收，本轮是实际WS＋只读工具＋文本续答验证。

## 本项目接入边界

Issue #62 已把只读棋局查询接入灵矽 Realtime MCP；游戏动作仍由现有 Go function 工具链完成。两条工具链共享同一房间事实，但权限隔离：MCP 查询不能掷骰、走棋、用道具或改房间，玩家动作继续经过房间授权、幂等命令和 Go 规则。

游戏 MCP 会话短时绑定当前房间与玩家；会话过期或结束时撤销令牌。服务端按 `item_id` 处理异步工具生命周期，不把第一个 `response.done` 当成查询已结束。成功时等待 Realtime MCP 结果再续答；失败时只报告暂时无法读取，不让模型猜测棋局。工具导入超时不会阻断语音连接，但会向页面报告 MCP 不可用。

## 与另一条接口的区别

灵矽[设备WebSocket文档](https://linx.qiniu.com/docs/xrobot/platform/websocket)中的 `/v1/ws/` 使用hello及包装的JSON-RPC消息，属于另一种端侧接入；[云端MCP](https://linx.qiniu.com/docs/xrobot/mcp/software-mcp)及[Agent绑定说明](https://linx.qiniu.com/docs/xrobot/guide/mcp-store)描述平台管理入口。它们不能代替本次Realtime session.tools配置，也不需要为了浏览器应用虚构硬件设备。

## 本项目实现（#62）

游戏语音会话现在可收到一个 Realtime 原生 `type: "mcp"` 工具配置。`MCP_PUBLIC_URL` 必须是可被灵矽访问的 HTTPS 源站；未配置时，HTTPS 部署会从 `PUBLIC_URL` 推导，纯本地 HTTP 开发不启用 MCP。灵矽 Realtime API Key 仍只留在 Go 服务端环境中；短时 MCP bearer 只发送给 Realtime 上游，不下发浏览器。

Go MCP 服务只开放四项只读能力：`get_game_summary`、`get_mission_progress`、`get_game_rules`、`get_legal_actions`。授权绑定现有语音会话中的房间与玩家，设有过期时间、调用上限和撤销；房间会话失效时撤销令牌。MCP 查询通过房间快照读取事实，不产生骰子、道具、走棋或房间管理写操作。写动作仍由原 Realtime 游戏工具链调用 Go 规则服务。

初始化会等工具导入完成再向浏览器确认语音握手（发现工具超时则保留语音连接，并提示只读工具不可用）。MCP 调用按 `item_id` 跟踪；若 Realtime 先发 `response.done`、之后才完成 MCP，则暂存本轮并在工具结果后续答。OpenAI Realtime 当前文档以 `response.output_item.done`（`item.type=mcp_call`）表示成功结果；桥接也兼容灵矽实测记录的 `response.mcp_call.completed` / `conversation.item.done` 事件形状。若同一回复还包含普通 Go 游戏函数调用，会先返回这些函数的真实结果，再等 MCP 完成后续答。工具失败时回复明确表示无法确认棋局事实，不拿模型猜测当结果。需要部署时，把 `MCP_PUBLIC_URL` 配为可访问的 HTTPS 源站；不要在 GitHub 或浏览器填写任何 API Key。
