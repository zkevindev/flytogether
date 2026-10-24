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
