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

## 本项目下一步接入位置

当前生产游戏动作仍走 `internal/linx/bridge.go` 的function工具与Go动作授权。上面的原生MCP探测没有偷偷切换生产链路。若将游戏工具暴露为MCP：

- Go提供窄范围工具，调用仍复用房间的真实规则、幂等命令与事务；不让模型计算骰子或直接改棋盘。
- 每个语音会话下发短时、绑定房间/席位/轮次的服务端授权；MCP服务端校验成员、当前玩家和过期/重放。房号或邀请链接不能代替动作授权。
- 管理权与走棋权分别校验；状态变化、打断、退出、重连撤销旧授权；回传真实事件后再播报。
- bridge增加MCP导入/调用/审批状态，不因首个response.done提前回到空闲；排障只记录脱敏工具状态，凭据不下发浏览器。
- 用真实语音和双端联机验证，尤其幸运骰只掷一次且弹窗完整、目标不明确先澄清、取消/跨轮次调用不落子。

因此优先保留现有稳定动作链路，再做一个可独立验收的原生游戏MCP切片。本次已证明供应商原生能力，尚未声称生产游戏已切到MCP。

## 与另一条接口的区别

灵矽[设备WebSocket文档](https://linx.qiniu.com/docs/xrobot/platform/websocket)中的 `/v1/ws/` 使用hello及包装的JSON-RPC消息，属于另一种端侧接入；[云端MCP](https://linx.qiniu.com/docs/xrobot/mcp/software-mcp)及[Agent绑定说明](https://linx.qiniu.com/docs/xrobot/guide/mcp-store)描述平台管理入口。它们不能代替本次Realtime session.tools配置，也不需要为了浏览器应用虚构硬件设备。

## 本项目实现（#62）

游戏语音会话现在可收到一个 Realtime 原生 `type: "mcp"` 工具配置。`MCP_PUBLIC_URL` 必须是可被灵矽访问的 HTTPS 源站；未配置时，HTTPS 部署会从 `PUBLIC_URL` 推导，纯本地 HTTP 开发不启用 MCP。灵矽 Realtime API Key 仍只留在 Go 服务端环境中；短时 MCP bearer 只发送给 Realtime 上游，不下发浏览器。

Go MCP 服务只开放四项只读能力：`get_game_summary`、`get_mission_progress`、`get_game_rules`、`get_legal_actions`。授权绑定现有语音会话中的房间与玩家，设有过期时间、调用上限和撤销；房间会话失效时撤销令牌。MCP 查询通过房间快照读取事实，不产生骰子、道具、走棋或房间管理写操作。写动作仍由原 Realtime 游戏工具链调用 Go 规则服务。

初始化会等工具导入完成再向浏览器确认语音握手。MCP 调用按 `item_id` 跟踪；若 Realtime 先发 `response.done`、之后才完成 MCP，则暂存本轮并在工具结果后续答。工具失败时回复明确表示无法确认棋局事实，不拿模型猜测当结果。需要部署时，把 `MCP_PUBLIC_URL` 配为可访问的 HTTPS 源站；不要在 GitHub 或浏览器填写任何 API Key。
