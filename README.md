# 一起飞 · FlyTogether

**会说话的家庭飞行棋搭子。**家长和孩子围坐在同一块屏幕前，可以点击或直接开口掷骰、选飞机、询问棋局。Go 服务掌握真实棋局，灵矽负责语音对话。

公开仓库：[zkevindev/flytogether](https://github.com/zkevindev/flytogether)

## 当前状态

网页飞行棋、Go 规则服务、家庭房间、AI 对手和公开 HTTPS 部署已可用。网页通过 Go 网关连接灵矽 Realtime，提供查询棋局、掷骰和移动棋子的游戏工具。已用合成中文语音通过真实平台验证唤醒、查询、掷骰、指定飞机起飞及 PCM 音频回复；浏览器真实麦克风、扬声器回声和打断体验仍需试玩验收。

棋盘可选经典十字、84 格长航线十字或环形航线；开局设置、棋盘显示和服务端路线参数保持一致。新版已部署，并完成浏览器中的开局、掷骰和合法棋子移动演示。

棋盘已按传统飞行棋布局重做：四角机场、连续彩色航线、朝向终点的跑道和中央可点击骰子，支持手机放大查看。默认启用趣味道具，也可在设置关闭，回到经典无道具玩法。

## 功能

- 同屏 2～4 位玩家，支持一名 Go 规则策略 AI。
- 快速局和完整局，棋盘演示局面，实时同步棋局。
- 三种棋盘路线可在每局开始前选择。
- 每人各一张起飞卡、加速卡和幸运骰，每次掷骰前最多用一张；AI 同样消耗自己的库存。
- 点击掷骰和移动，或通过浏览器实时语音操作与询问规则。
- 默认用“小灵同学”唤醒；网页语音卡片中可编辑唤醒词，或切换自由对话。唤醒后可连续对话 30 秒。
- Go 服务端校验骰子、合法走法、回合和胜负；重复请求不会重复执行动作。
- 可配置部署服务器；服务器地址与凭据不写入代码仓库。

## 本地运行

需要 Go 1.26+、Node.js 22.18+。

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
LINGXI_REALTIME_MODEL=lingxi-realtime
LINGXI_REALTIME_API_KEY=你的服务端API密钥
```

API Key 只由 Go 服务读取，通过 `Authorization: Bearer` 请求头访问实时接口，不会发送给浏览器。修改配置后需要重启 Go 服务。不要把真实密钥写入仓库或聊天记录。

首次使用先点击“开启语音唤醒”并授权麦克风，再说“小灵同学，掷骰子”，或在道具模式中先说“小灵同学，使用起飞卡”。每句话最多执行一个操作，用卡后另说“掷骰子”。经典起飞需要六点；起飞卡生效时可用任意点数起飞。玩家明确指定合法飞机后由 Go 实际执行并同步棋盘。等待唤醒时音频仍发送给灵矽识别；网页关闭或语音断开后唤醒不生效。自定义唤醒词保存在当前浏览器，并同步到当前房间。

## 道具怎么玩

| 道具 | 效果 |
| --- | --- |
| 起飞卡 | 下一次掷骰后，任意点数都可选一架机库飞机起飞；只有真实六点才奖励额外回合 |
| 加速卡 | 下一次掷骰后，在飞的飞机多走两格；起飞动作仍只到起飞位，终点仍须精确抵达 |
| 幸运骰 | 由 Go 随机掷两次、选较大值；网页和飞行小记显示两次点数 |

每人开局各一张，每次掷骰只可使用一张。用完后继续正常飞行；重新开局时恢复库存。

## 验证

```sh
go test ./...
npm --prefix web run test:board
npm --prefix web run build
```

几何检查覆盖三种棋盘的格数、路线连续、机场不遮挡公共格和同格棋子分开显示；Go 测试覆盖道具库存、回合、真实点数与非法操作不改变棋局。真实接口的合成语音联调和浏览器演示记录见[项目进展](docs/PROGRESS.md)。

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
- [Issue #4：可编辑语音唤醒](https://github.com/zkevindev/flytogether/issues/4)
- [Issue #5：棋盘布局重做](https://github.com/zkevindev/flytogether/issues/5)
- [Issue #6：家庭道具与语音使用](https://github.com/zkevindev/flytogether/issues/6)
- [灵矽官方文档](https://linx.qiniu.com/docs/)
