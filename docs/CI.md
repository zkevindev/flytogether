# 自动测试与覆盖率

[CI运行](https://github.com/zkevindev/flytogether/actions/workflows/ci.yml) · [Codecov文件级报告](https://app.codecov.io/gh/zkevindev/flytogether)

main推送、目标main的PR、手动运行触发两个任务；不会自动部署，也不读取本机或服务器的真实配置。Action固定到已核对的提交。

- Go：`go vet ./...`，`go test -race -count=1 -covermode=atomic -coverpkg=./... -coverprofile=test-results/coverage.out ./...`。包含所有Go生产包；多测试二进制产生的相同代码块按位置合并计数，避免将覆盖率按包平均。
- 前端：Node24、`npm ci`、`npm run test:unit`（棋盘、语音生命周期、展示、场景、播报音效、显示偏好），随后类型检查与生产构建。尚无前端行覆盖率，不能把Go数字当作全仓库覆盖率。
- Actions Summary显示总语句与分包覆盖率；`go-coverage-<commit>` artifact保留14天，包含coverage.out、函数列表、HTML及测试输出。失败时可能只有部分报告，以任务状态为准。
- Codecov只上传明确的Go coverage.out，使用GitHub OIDC，无需把长效Token写入仓库；上传失败使任务失败，不伪报成功。若第三方仓库尚未启用，需要仓库管理员在Codecov授权/启用本仓库后重跑。项目状态与基线比较，patch为信息提示，未编造固定覆盖率门槛。

README徽章读取main最新上报状态，具体分支/提交、文件和未覆盖行以Codecov为准。真实灵矽与Chrome端到端测试单独记录在各QA文档；不将未运行的浏览器/实机测试算入本工作流。

本地复跑：上述Go命令，随后 `python3 scripts/coverage_summary.py test-results/coverage.out`；前端在 `web` 下执行 `npm ci && npm run test:unit && npm run build`。覆盖率是测试触达生产语句的比例，并不保证规则、交互或异常路径都正确。

Action与OIDC用法依据 [Codecov官方Action](https://github.com/codecov/codecov-action#using-oidc)。

首次实际运行：[7e0f591 CI](https://github.com/zkevindev/flytogether/actions/runs/40077207606)。初次Codecov尚未启用仓库而返回Repository not found，用户启用后重跑成功，前端与Go两任务通过，公开仓库已active、覆盖率徽章可读。后续提交继续自动验证，不把首次失败隐藏成测试成功。

## 80% 阶段目标验收（2026-10-24，#34）

用户将本阶段 Codecov 目标设为至少 80%。[0947890](https://github.com/zkevindev/flytogether/commit/0947890d594d7d0c76ac000cd94a3ad0171167b0) 的 [真实 CI](https://github.com/zkevindev/flytogether/actions/runs/40093025545) 已通过；[Codecov 提交报告](https://app.codecov.io/gh/zkevindev/flytogether/commit/0947890d594d7d0c76ac000cd94a3ad0171167b0) 处理状态为 complete，31 个 Go 生产文件、5046 行，4209 行完全覆盖、280 行部分覆盖、557 行未覆盖，实际覆盖率 **83.41%**。可通过 [Codecov API](https://api.codecov.io/api/v2/github/zkevindev/repos/flytogether/commits/0947890d594d7d0c76ac000cd94a3ad0171167b0/) 核对。

本地全包 Go 语句覆盖率约 88.2%；race 全包连续三轮通过、go vet 通过，前端 42 个现有单测及生产构建通过。新增测试覆盖 HTTP 生命周期、鉴权与参数拒绝、家庭档案、观赛只读语音与互动隔离、SQLite 损坏隔离和保存回滚、规则状态验证、语音意图与 TTS 握手/异常/缓存。服务启动配置、路由注册与安全头从 main 抽成可直接测试的函数；保留全部 Go 生产包统计，不排除低覆盖文件，不关闭上传失败检查。

83.41% 是 Codecov 按行结果，不是前端或全产品覆盖率；真实浏览器及灵犀服务验收仍与单测分开。#34 原有 90% 长期目标尚未完成，80% 阶段目标已完成。

## 两个覆盖率口径

应用2f1bb79的同一份CI报告：Go工具与Summary按语句计为71.5%；Codecov处理后27个Go文件、3933行，2529行完全覆盖、327行部分覆盖、1077行未覆盖，页面为64.3%，徽章四舍五入显示64%。这是按行/部分覆盖与按语句计算的差异，不用手改徽章数字或排除未覆盖生产代码来抬高结果。后续以对应提交的报告为准。Codecov的解释见 [覆盖率百分比](https://docs.codecov.com/docs/coverage-percentages)。
