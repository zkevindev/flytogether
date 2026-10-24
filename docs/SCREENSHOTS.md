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
| [coach-desktop.png](images/current/coach-desktop.png) | 实际教练推荐、比较按钮和高亮落点，未执行移动 |
| [coach-mobile.png](images/current/coach-mobile.png) | 手机视口的教练预览和确认入口 |
| [play-mobile.png](images/current/play-mobile.png) | 实际确认起飞后，390×844 视口的响应式布局 |

截图关闭对讲麦克风和主动播报，避免文档采集申请声音权限；不伪造已连接状态、语音回复、骰子或飞机位置。骰子截图临时暂停浏览器展示计时，防止短弹窗在截图时消失；随机结果仍取真实 Go 事件，其他流程恢复正常计时。两位玩家使用通用昵称。新手指南通过正式入口打开，其余操作均经实际页面完成。

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


## 邀请房间与结算

新增 [`images/network/manifest.json`](images/network/manifest.json) 独立记录本次应用 293d3b2 的四张联网截图：桌面/手机准备区、桌面/手机自然获胜结算。由 `test:network:browser` 使用独立 Cookie、实际随机骰子与合法走法采集；关闭麦克风及播报，不记录邀请链接或身份凭据。脚本没有注入棋盘位置。复跑时用 `QA_OUTPUT_DIR` 指定临时目录，挑选有效截图后更新对应哈希清单。


## 真人＋机器人（#23）

当前同屏/创建界面 10 张与纯真人联机 4 张已更新到 f4b1ac0，新增 [`images/network-bot/`](images/network-bot/) 的混合房间配置、桌面/手机自动准备、自然结算四张，来源、视口与 SHA-256 见各目录 manifest。混合局通过正式邀请、真实随机投掷和服务端机器人调度推进，无注入状态；图中机器人有独立标记且不需要第三台设备。创建弹窗截图滚动到机器人配置区，以展示席位组成与语音助手说明。


## 存档与独立声音（#24 / #25）

`images/current/` 10 张已从 b485e95 运行应用重新采集，主图、手机和教练图包含当前声音设置入口。新增 [reliability/manifest.json](images/reliability/manifest.json) 记录5张：重启后首页、骰子选择阶段恢复、SQLite写入失败、混合席位恢复和声音设置。前四张来自真实进程重启/受控磁盘写入失败；声音设置图使用受控 PCM 和 Chrome 合成麦克风，未使用实体录音。已检查棋盘、弹窗、手机内容及保存提示可见。历史 network/network-bot 图的版本保留在原清单中。


## P1：陪玩与规则预设

当前界面10张已重新采集；新增 [p1/manifest.json](images/p1/manifest.json) 记录6张真实页面：陪玩偏好、亲子创建配置、亲子规则棋盘、辅助补给、经典手机视口和棋盘加载失败恢复。拍摄等待画布和布局稳定，普通局使用真实随机骰子；加载失败图仅刻意中止模块下载，未注入棋局。逐张版本与哈希保留在清单，实体反馈与合成/受控音频分开记录。

## 家庭飞行员与历史（#29）

当前10张图已从 `1887b61` 重拍，首页含家庭入口，创建页包含档案复用；新增 [family/manifest.json](images/family/manifest.json) 记录4张：昵称/头像档案、带本人头像的真实棋盘、自然结束后的逐局战绩与390px手机视口战绩。资料和历史分为两个页签，手机六列完整显示。

由 `test:family:browser` 经真实页面建档/选人/掷骰、正式命令与服务端随机骰子完成自然棋局后拍摄，无棋盘位置/点数注入或麦克风采集；资料中的称呼是QA示例。该脚本可用 `QA_OUTPUT_DIR` 指向临时目录复拍，确认图片后再复制并更新哈希清单。已逐张检查画布、头像、页签、表格及手机布局。版本与旧P1/网络/保存等历史图分开保留。

## 语音掷骰动作确认（#30）

[voice-roll/manifest.json](images/voice-roll/manifest.json) 记录 `5683f8e` 的落定骰子弹窗：合成说“好，掷骰子”经真实灵矽转写，Go实际随机3点、版本3、投掷与弹窗均1次。截图等待骰子停止转动，文字和正面点数一致。测试页面刻意延迟棋局WS消息，实际语音动作确认仍驱动弹窗；没有注入转写/点数/棋盘，也不是实体麦克风验收。公网对应五句样本实际1点，完整转写/确认/回复见 [QA-VOICE](QA-VOICE.md)。
