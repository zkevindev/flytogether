# 开发、测试与部署

本文汇总本地启动、灵矽配置、测试和服务器发布入口。涉及真实凭据时只配置在仓库外的本机或服务器环境中；不要写入 .env.example、文档、截图或 Issue。

## 本地启动

需要 **Go 1.26+、Node.js 22.18+、npm**。不填灵矽密钥也可玩完整点击玩法。

```sh
# 在仓库根目录；已有 .env 时保留现有配置
test -f .env || cp .env.example .env
npm --prefix web ci
npm --prefix web run build
go run ./cmd/server
```

默认打开 <http://127.0.0.1:8080/>。Go 同时托管网页、游戏 API 和语音代理；不要通过 `file://` 直接打开 HTML。

前端开发热更新：保持 Go 服务运行，另开终端执行 `npm --prefix web run dev`，打开 Vite 输出的地址；API 与 WebSocket 会代理到本机 8080。

### 配置灵矽

在本机 `.env` 或服务器 `/etc/flytogether.env` 中设置：

```dotenv
LINGXI_REALTIME_URL=wss://xrobo-io.qiniuapi.com/v1/realtime
LINGXI_REALTIME_MODEL=lingxi-realtime
LINGXI_REALTIME_API_KEY=你的服务端API密钥
```

配置后重启 Go。密钥只由服务端读取，通过 Bearer 请求头访问灵矽，不放入网页构建产物。浏览器语音需要 localhost 或 HTTPS。

## 验证与演示

常规提交检查：

```sh
go vet ./...
go test -race ./...
npm --prefix web run test:unit
npm --prefix web run build
python3 scripts/recovery_simulation.py
```

需要真实浏览器回归时，先启动使用独立数据库的本地 Go 服务，再运行综合矩阵：

```sh
QA_BASE_URL=http://127.0.0.1:8080 \
QA_OUTPUT_ROOT=/tmp/flytogether-acceptance \
npm --prefix web run test:acceptance:matrix
```

矩阵需要本机 Chrome；会运行多人、AI、道具、生命周期与语音 PCM 用例。特定功能的独立浏览器脚本列在 web/package.json。家庭档案的真实进程重启与联网回归可单独运行：

```sh
go build -o /tmp/flytogether-qa-server ./cmd/server
QA_FAMILY_SERVER=/tmp/flytogether-qa-server npm --prefix web run test:family:browser
```

脚本只启动和重启自己的子进程，使用临时数据库；访问已运行环境可用 `QA_BASE_URL`。详见[家庭档案验收](QA-FAMILY.md)。各功能测试的环境、范围和限制见[文档目录](README.md#qa-验收记录)。

## 部署

部署工具需要 **Python 3.10+ 与 Paramiko**，使用 SSH root 登录目标 Linux 服务器。复制 [部署配置示例](../.env.deploy.example) 到仓库外的安全目录，填写服务器、端口、域名与凭据路径，再执行：

```sh
python3 -m venv .venv-deploy
.venv-deploy/bin/pip install paramiko
.venv-deploy/bin/python scripts/deploy.py --config /本机安全目录/flytogether-deploy.env
```

工具构建网页和 Go Linux 二进制，安装服务并检查健康；配置域名时由 Caddy 提供 HTTPS。灵矽凭据留在服务器 `/etc/flytogether.env`，权限为 `600`。仓库不保存真实服务器地址、部署域名、密钥或用户录音。

## 延伸阅读

- [存档与备份恢复](SAVES.md)
- [演示与发布运行手册](DEMO-RUNBOOK.md)
- [自动测试与覆盖率](CI.md)
