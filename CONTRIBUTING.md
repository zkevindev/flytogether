# 项目协作与记录约定

本项目当前按单人、三天两晚的 Hackathon 范围推进。

## 开始任务

1. 阅读 [架构设计](docs/ARCHITECTURE.md) 与 [进展记录](docs/PROGRESS.md)。
2. 将当前目标对应到 GitHub Issue；同一时间优先推进一个可验收的目标。
3. 修改产品规则、状态归属、语音协议或部署方式时，同步更新设计文档。

## 提交

- 使用清楚的提交说明，例如 docs: define MVP architecture、feat: add authoritative dice action、fix: deduplicate voice commands。
- 提交说明可以引用 Issue 编号。
- 使用开源代码和素材时，在引入提交中记录来源及许可证。
- 不提交真实凭据、音频录制、儿童个人信息或带认证参数的调试日志。

## 记录进展

在 [PROGRESS.md](docs/PROGRESS.md) 中追加日期、实际变更、验证证据和下一步。

只有实际验收通过的任务才标为完成。接口文档、示意图和模拟演示不代表真实平台联调通过。Issue 完成时补充验证结果；存在未解决事项时明确列出。

## 验证

文档修改检查链接、示例和术语一致性。实现阶段重点验证游戏规则、重复请求、点击与语音竞争、灵矽真实音频链路，以及语音失效后的点击继续游戏。

具体构建和检查命令见 [README](README.md)。新手流程、预览与确认要检查“预览不写入”和过期版本不提交；语音故障时点击玩法应保持可用。

## 文档与截图

README 面向首次访问者，只写已实现能力、运行方式与实际限制；后续计划放在 [ROADMAP](docs/ROADMAP.md)，历史证据放在 [PROGRESS](docs/PROGRESS.md)。可见 UI 变更按 [截图说明](docs/SCREENSHOTS.md) 复拍，保留提交、视口与校验清单。截图来自实际应用，不伪造语音状态或固定骰子；历史截图继续保留在历史记录中。
