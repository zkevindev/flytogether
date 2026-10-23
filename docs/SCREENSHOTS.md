# 当前界面截图与维护

README 使用 [`images/current/`](images/current/) 中从真实应用重新截取的图片。每次拍摄的时间、应用提交、Chrome 版本、视口与文件 SHA-256 记录在 [manifest.json](images/current/manifest.json)，不记录真实服务器地址、Cookie、密钥或录音。

## 图片对应的实际流程

| 文件 | 内容 |
| --- | --- |
| [home.png](images/current/home.png) | 首页创建入口，没有挂载对局棋盘或麦克风 |
| [setup.png](images/current/setup.png) | 实际创建弹窗，两个真人席位、经典十字、趣味道具 |
| [play-desktop.png](images/current/play-desktop.png) | 桌面独立对局页，1440×1120 视口 |
| [onboarding.png](images/current/onboarding.png) | 可跳过、可重看的新手飞行指南 |
| [props.png](images/current/props.png) | 五种道具、实际库存和不可用原因 |
| [dice-3d.png](images/current/dice-3d.png) | 使用起飞卡后，Go 实际随机点数的 3D 弹窗 |
| [move-preview.png](images/current/move-preview.png) | 选飞机后的预计落点与确认入口；此时尚未执行移动 |
| [play-mobile.png](images/current/play-mobile.png) | 实际确认起飞后，390×844 视口的响应式布局 |

截图关闭对讲麦克风和主动播报，避免文档采集申请声音权限；不伪造已连接状态、语音回复、骰子或飞机位置。两位玩家使用通用昵称。新手指南通过正式入口打开，其余操作均经实际页面完成。

手机图片是桌面 Chrome 缩小视口的结果，不能替代真实 Android/iOS 验收。语音与撒花、坠机等额外证据见 [语音验收](QA-VOICE.md)；`images/` 根目录旧图保留为历史证据，不能拿来代表最新完整页面。

## 复拍

需要 Chrome，以及已安装依赖并启动的应用。将 `QA_SOURCE_COMMIT` 换为当前运行版本的提交；拍摄已部署服务时，不要误填尚未部署的本机 HEAD。

```sh
QA_BASE_URL=http://127.0.0.1:8080 \
QA_SOURCE_COMMIT=实际运行版本的提交SHA \
QA_OUTPUT_DIR="$PWD/docs/images/current" \
npm --prefix web run capture:docs
```

在仓库根目录执行。访问其他环境时，通过本机环境变量传入真实地址，不将它写到脚本、文档或截图清单。脚本为独立浏览器会话创建测试游戏，完成创建、道具、真实随机掷骰、预览与确认起飞，检查页面无横向溢出、控制台无错误、麦克风请求为 0，最后更新清单。

## 文档维护约定

1. 首页、对局、引导、道具或骰子等可见界面改变时，复拍相关截图并更新清单。
2. 逐张查看图片，确认有实际渲染、内容可辨认、弹窗和手机布局完整；截图文件存在不表示验收成功。
3. README 描述已经实现的行为；未来能力写入 [产品化路线](ROADMAP.md)。
4. 更新 [开发进展](PROGRESS.md) 与对应 Issue，回链应用提交、文档提交和验证范围。
