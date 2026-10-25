# 项目协作与记录约定

本项目当前按单人、三天两晚的 Hackathon 范围推进。

## 开始任务

1. 从 [文档目录](docs/README.md) 选择与你的任务相关的指南；理解系统时读 [架构设计](docs/ARCHITECTURE.md)，不要把历史进展记录当作当前待办。
2. 将当前目标对应到 GitHub Issue；同一时间优先推进一个可验收的目标。
3. 修改产品规则、状态归属、语音协议或部署方式时，同步更新设计文档。

## 提交

- 使用清楚的提交说明，例如 docs: define MVP architecture、feat: add authoritative dice action、fix: deduplicate voice commands。
- 每个可验收切片使用独立 `codex/` 分支和对应 Issue 的 PR；PR 描述引用 Issue。一个 PR 只解决一个切片，避免把互不相关的 Issue 打包提交。
- PR 检查通过并由维护者合并后，再在 Issue 记录合并提交、验证结果和剩余事项；不要直接推送 `main`，不要自行合并 PR。
- 使用开源代码和素材时，在引入提交中记录来源及许可证。
- 不提交真实凭据、音频录制、儿童个人信息或带认证参数的调试日志。

## 记录进展

在 [PROGRESS.md](docs/PROGRESS.md) 中追加日期、实际变更、验证证据、PR 与下一步。

只有实际验收通过的任务才标为完成。接口文档、示意图和模拟演示不代表真实平台联调通过。Issue 完成时补充验证结果；存在未解决事项时明确列出。

## 验证

文档修改检查链接、示例和术语一致性。实现阶段重点验证游戏规则、重复请求、点击与语音竞争、灵矽真实音频链路，以及语音失效后的点击继续游戏。

具体构建和检查命令见 [开发、测试与部署](docs/DEVELOPMENT.md)。新手流程、预览与确认要检查“预览不写入”和过期版本不提交；语音故障时点击玩法应保持可用。

## 文档与截图

README 面向首次访问者，只写已实现能力、运行方式与实际限制；玩家操作见 [USER-GUIDE](docs/USER-GUIDE.md)，开发和部署步骤见 [DEVELOPMENT](docs/DEVELOPMENT.md)，后续计划放在 [ROADMAP](docs/ROADMAP.md)，历史证据放在 [PROGRESS](docs/PROGRESS.md)。可见 UI 变更按 [截图说明](docs/SCREENSHOTS.md) 复拍，保留提交、视口与校验清单。截图来自实际应用，不伪造语音状态或固定骰子；历史截图继续保留在历史记录中。
