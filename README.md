# 一起飞 · FlyTogether

**会说话的家庭飞行棋搭子。**家长和孩子围坐在同一块屏幕前，可以点击或直接开口掷骰、选飞机、询问棋局。Go 服务掌握真实棋局，灵矽负责语音对话。

公开仓库：[zkevindev/flytogether](https://github.com/zkevindev/flytogether)

## 当前状态

网页飞行棋、Go 规则服务、家庭房间、AI 对手和公开 HTTPS 部署已可用。语音接入已切换为浏览器麦克风经 Go 网关连接灵矽 Realtime，并提供查询棋局、掷骰和移动棋子的游戏工具；真实灵矽凭据尚未配置，语音链路仍待联调。

棋盘可选经典十字、84 格长航线十字或环形航线；开局设置、棋盘显示和服务端路线参数保持一致。新版已部署，并完成浏览器中的开局、掷骰和合法棋子移动演示。

## 功能

- 同屏 2～4 位玩家，支持一名 Go 规则策略 AI。
- 快速局和完整局，棋盘演示局面，实时同步棋局。
- 三种棋盘路线可在每局开始前选择。
- 点击掷骰和移动，或通过浏览器实时语音操作与询问规则。
- Go 服务端校验骰子、合法走法、回合和胜负；重复请求不会重复执行动作。
- 可配置部署服务器；服务器地址与凭据不写入代码仓库。

## 本地运行

需要 Go 1.26+、Node.js 22+。

```sh
cp .env.example .env
go run ./cmd/server
```

另开终端运行网页开发服务器：

```sh
cd web
npm ci
npm run dev
```

打开 Vite 输出的本地地址。语音使用浏览器麦克风，需要在 localhost 或 HTTPS 页面授权。

要启用灵矽语音，在本机 `.env` 或部署服务器 `/etc/flytogether.env` 设置：

```dotenv
LINGXI_REALTIME_URL=wss://xrobo-io.qiniuapi.com/v1/realtime
LINGXI_REALTIME_MODEL=控制台中可用的实时模型名
LINGXI_REALTIME_API_KEY=你的服务端API密钥
```

API Key 只由 Go 服务读取，不会发送给浏览器。不要把真实密钥写入仓库或聊天记录。

## 部署

部署脚本从仓库外的配置文件读取 SSH 地址、端口、目录、域名和凭据。复制 `.env.deploy.example` 到本机仓库之外并填写，再运行：

```sh
python3 scripts/deploy.py --config /本机安全目录/flytogether-deploy.env
```

部署后的灵矽凭据保存在服务器 `/etc/flytogether.env`，权限为仅 root 可读。详见 [部署脚本](deploy/install.sh) 与 [部署工具](scripts/deploy.py)。

## 项目记录

- [项目架构](docs/ARCHITECTURE.md)
- [开发进展](docs/PROGRESS.md)
- [贡献约定](CONTRIBUTING.md)
- [Issue #1：实时语音接入](https://github.com/zkevindev/flytogether/issues/1)
- [Issue #2：家庭飞行棋玩法](https://github.com/zkevindev/flytogether/issues/2)
- [Issue #3：试玩与比赛交付](https://github.com/zkevindev/flytogether/issues/3)
- [灵矽官方文档](https://linx.qiniu.com/docs/)
