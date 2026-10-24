# Issue #62 灵矽 Realtime MCP 联调

## 端到端浏览器回归

该回归需要一台已配置真实灵矽 Realtime 凭据、公开 HTTPS 域名的运行中服务，以及 macOS `say`/FFmpeg 和 Chrome。API Key 只留在服务器，不传入测试命令或浏览器。

```sh
python3 scripts/voice_fixture.py --scenario mcp --output-dir /tmp/flytogether-mcp-audio
QA_BASE_URL=https://你的测试域名 \
QA_AUDIO_FILE=/tmp/flytogether-mcp-audio/browser-input.wav \
QA_OUTPUT_DIR=/tmp/flytogether-mcp-live \
npm --prefix web run test:mcp:realtime
```

测试创建一间星际救援合作房间，选择“建立全员联络”任务卡，通过 Chrome 合成麦克风向真实灵矽提问。回归断言工具已导入、识别到任务问题、Realtime MCP 调用完成、最终语音转写完整到达，并且 MCP 查询前后游戏版本和任务进度不变。麦克风会在最终播报完成后才关闭，避免测试提前截断回复。结果文件含本次合成问题、回答和随机生成的游戏 ID；请保留在本机临时目录，不要提交到仓库。

## 验证真实 MCP 调用

仅浏览器能播放语音或看到模型提到任务，不足以证明 Realtime 实际调用了游戏 MCP。使用受保护的管理后台，按上一步结果文件中的 `game_id` 查看该局事件，确认出现 `kind=mcp`、`action=readonly_call` 的 `running` 与 `completed` 记录。若显示 `failed`，需保留失败码并修复后重跑；不能把模型生成的任务进度当作工具结果。后台管理员密钥只在登录页面输入，不传入测试脚本。

## 当前验收范围

- Go 的 MCP Streamable HTTP 协议用 MCP Go SDK 实际连接，验证四个只读工具导入、任务查询、未授权拒绝和会话撤销。
- 浏览器桥接测试模拟 Realtime 异步成功、失败、迟到完成和同一响应混合 MCP/Go 函数调用。
- 本回归在真实灵矽 Realtime 上验证项目 MCP 的工具导入、实际工具调用、任务问答续答和棋局只读不变；它使用合成 WAV，不代表实体麦克风或亲子现场验收。

## 最近一次公网回归

2026-10-25，公网 `/readyz` 返回 `ok` 且 `voice_configured=true`。真实 Realtime 成功导入工具并执行只读任务查询，合成语音识别的问题为“请查一下本局星际救援任务卡的目标和当前进展”，播报结果为“任务‘建立全员联络’的目标是每位飞行员至少完成 3 次掷骰，目前进度为 0。”工具调用完成，棋局版本和任务进度保持不变，浏览器错误为 0。测试没有暴露密钥，也没有触碰真实玩家棋局。
