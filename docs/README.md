# 文档目录

先从 [仓库首页](../README.md) 了解产品。想了解玩法读用户指南，想启动或修改项目读开发指南；架构、计划和验收文档分别回答不同问题。

## 按任务阅读

| 你想做什么 | 从这里开始 | 继续阅读 |
| --- | --- | --- |
| 了解和试玩游戏 | [仓库首页](../README.md) | [玩法指南](USER-GUIDE.md)、[截图说明](SCREENSHOTS.md) |
| 准备微信小程序体验版内测 | [小程序内测说明](MINIPROGRAM-PILOT.md) | AppID、体验成员、业务域名和真机语音验收 |
| 本地启动、测试或部署 | [开发、测试与部署](DEVELOPMENT.md) | [CI 与覆盖率](CI.md)、[存档与恢复](SAVES.md)、[演示与发布手册](DEMO-RUNBOOK.md) |
| 理解系统怎么工作 | [架构设计](ARCHITECTURE.md) | [灵矽 Realtime/MCP 接入核对](LINGXI-MCP.md)、[管理后台](ADMIN.md) |
| 查看当前状态或后续计划 | [项目进展](PROGRESS.md) | [产品路线图](ROADMAP.md)、[试玩与平衡记录](PLAYTEST-BALANCE.md) |
| 查找某项功能的验收证据 | 对应的 QA 文档 | [综合验收](QA-ACCEPTANCE.md) 汇总多人、AI、道具、稳定性模拟和恢复演练 |

## QA 验收记录

这些文档记录测试日期、环境、命令、结果与限制。自动模拟、合成音频、真实平台联调和现场设备验收应按各自实际来源理解。

### 产品与棋局

- [产品页](QA-PRODUCT.md)
- [新手引导、规则预设与按需加载](QA-P1.md)
- [道具](QA-PROPS.md)
- [起飞卡](QA-LAUNCH-CARD.md)
- [灵矽教练](QA-COACH.md)
- [星际救援任务](QA-MISSIONS.md)
- [棋盘与 3D 渲染](QA-3D.md)
- [无障碍、键盘和减少动画](QA-ACCESSIBILITY.md)

### 房间与家庭体验

- [联网与结算](QA-NETWORK.md)
- [观赛与互动](QA-AUDIENCE.md)
- [暂离托管与回位](QA-DELEGATION.md)
- [家庭飞行员与历史](QA-FAMILY.md)

### 语音、MCP 与运行保障

- [Realtime MCP 实际调用](QA-MCP.md)
- [语音与 Realtime](QA-VOICE.md)
- [声音设置、资源限制与恢复](QA-RELIABILITY.md)
- [管理排障](QA-ADMIN.md)
- [综合回归与恢复模拟](QA-ACCEPTANCE.md)

## 文档职责

- [README](../README.md)：产品是什么、快速启动和入口链接。
- [USER-GUIDE](USER-GUIDE.md)：面向玩家的创建房间、玩法规则、语音、观赛和道具说明。
- [DEVELOPMENT](DEVELOPMENT.md)：开发环境、私有配置、测试命令和部署入口。
- [ARCHITECTURE](ARCHITECTURE.md)：系统职责、数据流和协议边界；后续实现补充按历史记录阅读。
- [PROGRESS](PROGRESS.md)：已完成工作、Issue/PR 证据和按时间累积的交付记录，不作为待办清单。
- [ROADMAP](ROADMAP.md)：未交付计划与产品边界；已完成项以勾选状态和关联证据为准。
- [SCREENSHOTS](SCREENSHOTS.md)：截图来源、提交、视口、哈希和复拍方法。

## 维护约定

更新产品行为时同步玩法指南与对应 QA；更新待办时只改路线图；完成可验收切片后在进展记录和 GitHub Issue 回链 PR 与证据。历史测试结果不要改写成新的验收事实。
