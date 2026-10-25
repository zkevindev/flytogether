# 综合验收矩阵与恢复演练

日期：2026-10-25。对应后续验收 Issue #69。此前已确认的亲子完整对局和实体手机试玩保持为用户实机验收；本文记录可复跑的桌面浏览器与隔离故障模拟，不把模拟描述为现场声学或移动网络结果。

## 隔离环境

本轮使用本机 Go 服务 `127.0.0.1:19169`、专用 SQLite `/tmp/flytogether-issue69-runtime/games.db`、实际 Chrome 和正式 API/随机骰子。灵矽未配置，麦克风不申请；浏览器页面测试无状态注入。输出位于仓库外 `/tmp/flytogether-issue69-runtime/`，生产服务与家庭数据未访问。

统一复跑命令：

```sh
QA_BASE_URL=http://127.0.0.1:8080 \
QA_OUTPUT_ROOT=/tmp/flytogether-acceptance \
npm --prefix web run test:acceptance:matrix
```

命令依次运行真人联机、混合 AI、八道具、页面离线/可见性模拟和语音客户端/PCM 测试。Go 校验与备份/回滚模拟单独运行：

```sh
go test -race ./...
go vet ./...
python3 scripts/recovery_simulation.py
npm --prefix web run test:unit
npm --prefix web run build
```

## 席位、机器人难度、胜局与重开

| 席位与组成 | 实际覆盖 | 结果 | 证据脚本 |
| --- | --- | --- | --- |
| 2 个真人、无机器人 | 邀请、准备、席位授权、重复命令幂等、断线恢复、自然胜局、刷新结算和同房间重开 | 通过；自然局执行 25 条合法命令，双方结算一致 | `test:network:browser` |
| 3 个真人、无机器人 | 环形棋盘、准备与开局 | 通过 | `test:network:browser` |
| 4 个真人、无机器人 | 长航线、四端准备、房主结束后各端回首页 | 通过 | `test:network:browser` |
| 2 席：1 真人 + 轻松 AI | 自动准备、AI 正常掷骰、不使用道具、手动结束与席位边界 | 通过 | `test:network:bot` |
| 3 席：2 真人 + 挑战 AI | 权限隔离、暂停阻止 AI、恢复行动、挑战 AI 参与随机自然局并完成胜局、原房间同配置再玩 | 通过；完整自然局在第二次综合运行中 242 次轮询结束，真人最终获胜，机器人道具统计 5 | `test:network:bot` |
| 4 席：3 真人 + 普通 AI | 多人席位上限、自动准备与行动、普通 AI 走辅助道具、结束后多人回首页 | 通过 | `test:network:bot` |

这是一套代表性覆盖矩阵，不是 `席位 × AI 难度 × 每张道具` 的完全笛卡尔积。AI 三档均有实际浏览器席位验证；自然胜局和同配置重开各有真实规则回归。非法越权、同一 command ID 重试、暂停和结束边界由脚本与 Go race 测试覆盖。

## 八种道具

`test:props:browser` 使用正式房间 API 和服务端效果逐张验证库存、动作记录、棋子状态、视觉事件、刷新后不重播、麦克风未请求、renderer 数量不增加：起飞、加速、幸运骰、炸弹、护盾、回旋镖、救援、团队护航。幸运骰使用真实随机结果，不锁定骰点。联机局另验证回旋镖、救援和护航的合法目标、错误席位拒绝、双端事件一致和幂等重试。11 项全通过，页面异常为 0。

本矩阵确保八种道具都在服务端规则中单独执行；不会声称每张卡都与每个人数、AI 难度和每种棋盘排列组合全部跑过。所有组合扩展可通过同一脚本继续增加。

## 断网、后台、噪声与长时语音模拟

- `test:lifecycle:simulation` 让 Chrome 离线再恢复，验证离线时写操作受阻，收到新权威快照后重新开放；模拟 Page Visibility API 的 `hidden → visible`，验证隐藏期间的真实服务端骰子状态恢复。页面异常 0。
- `test:voice` 共 24 项：连接取消/迟到事件/打断/受限 PCM 队列；75 次连续 24 秒合成回复模拟累计 30 分钟播放时长，队列逐轮排空、连接不重建。
- `test:voice:capture` 对实际 `pcm-capture.js` 工作线程执行 48 kHz 到 16 kHz 转换；清洁音频、10 dB/0 dB 白噪声、50 ms 延迟回声和过载输入均输出 20 ms、640 字节、有界的单声道 PCM 帧。另断言浏览器采集请求启用单声道、浏览器回声消除、降噪和自动增益。
- `scripts/recovery_simulation.py` 覆盖离线备份时 SQLite WAL 中已提交行、备份完整性、权限 600、拒绝覆盖；在临时 release 目录切到失败版本，再原子切回最后良好版本并核对健康标记。

以上噪声/回声是 PCM 编码和帧边界的确定性模拟，不评判 ASR 在对应信噪比下识别是否准确。30 分钟是加速生成/消费合成帧，不是墙钟运行 30 分钟；可见性事件不冻结操作系统进程。真实蜂窝切换、系统挂起/杀进程、扬声器回声、距离与真人识别效果仍要在现场逐项记录，不能用脚本结论代替。

## 备份、回滚和公开分发资料

- `scripts/save_backup.py` 使用 SQLite backup API，可安全读取活动 WAL；要求全新目标路径，执行 `quick_check`，原子发布并设文件权限 `600`。
- `scripts/recovery_simulation.py` 使用活动 WAL 数据库副本验证备份和恢复演练，并在临时目录验证 release 软链原子切换。未访问线上库、未调用 systemd、未执行生产恢复或数据库降级。
- `deploy/install.sh` 以 `current.next` 加原子 `mv -T` 切换 release；健康检查失败时不会偷偷改写数据库。失败发布的回退按 [演示与发布手册](DEMO-RUNBOOK.md) 保留失败现场、确认数据库版本兼容、切回上一个 release 并检查健康。需要 schema 降级时，必须停服并恢复发布前数据库备份；只切二进制不安全。
- 项目 MIT 许可证为仓库根目录 [`LICENSE`](../LICENSE)。运行时 Go 模块与前端 production 包的精确版本和上游许可全文见 [`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md) / [`third_party/licenses`](../third_party/licenses)。仓库没有第三方照片、真人录音或外部字体；图标来自已锁定的 Phosphor 包，棋盘图形和音效由代码生成，截图取自产品自身。

## 汇总结果

2026-10-25 隔离环境汇总：`test:acceptance:matrix` 全部通过（真人 2/3/4 席、混合 AI 三档、八道具、断网/可见性、24 项语音与 PCM 测试）；`go test -race ./...`、`go vet ./...`、`npm run test:unit`、`npm run build` 和 `python3 scripts/recovery_simulation.py` 通过。浏览器回归记录页面异常 0，联机脚本记录控制台错误 0；没有请求麦克风，生产环境改动数为 0。
