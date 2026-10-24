# 八种道具与反馈验收（#36）

日期：2026-10-24。规则 `11a7fd8`，语音确认 `6fa5816`，特效 `1a6eb72`，卡片布局 `05de003`。仅main，未创建其他分支/PR。覆盖率专项#34暂缓，既有CI/Codecov继续运行。

## 规则与兼容

- 新建趣味局八种各1张：起飞、加速、幸运骰、炸弹、护盾、回旋镖、救援、团队护航。亲子只补救援/护航，禁攻击道具；经典无道具。
- 回旋镖选择对手公共航线飞机后退3格，最低第1格；盾抵消，不触发跳跃/飞行/撞机。救援仅选自己公共航线，撤回起飞位并解除盾，不造起飞/送回统计。护航为全部自己未加盾公共航线飞机加盾，无需目标。
- 每次掷骰前限一张；非法/跨席位/无库存不消耗。旧库存不补发，创建或再玩才获得新补给。schema5保持兼容，没有改变已有棋子结构；效果元数据可选。
- hard机器人可用攻击/护航及有限救援，普通继续辅助卡，轻松不使用卡；没有改变真人骰子随机分布。

## 实际执行与视觉

新增prop_id与effects（飞机ID、前后位置、效果类型），由Go真实提交后写入历史。React展示队列去重，FX层只读，不发走棋请求；炸弹、盾/抵挡、回旋镖、救援、护航和准备类卡各有反馈，新卡有独立合成音效。平面棋盘标记目标与路径，立体棋盘展示中央效果提示；都不新建WebGL renderer，效果自动清理。减少动画保持静态结果，静音仍控制所有音效。

本地与公网真实Chrome：8种道具通过UI点击，确认库存、位置、盾、真实幸运双骰、音效节点、效果销毁与刷新不重播。三种新卡另用两个独立Cookie玩家的自然随机棋局找到合法目标，同一效果ID与权威状态双端一致，非当前席位403，原命令重试不重复消费。共11案例，页面错误0。演示局只用项目正式demo初始化；联网不启用demo；未注入点数、位置或转写。390px为桌面Chrome视口，不冒充实体手机。

30次实际投掷保持1个renderer且资源最终回到基线；另通过主动丢失/自动恢复/手动恢复和WebGL不可用的平面降级。主动制造的丢失单独计数，不混入“意外丢失0”。场景脚本修补了空Cookie新上下文需先创建房间的旧前置条件。

## 真实语音

Tingting合成WAV经实际Chrome麦克风AudioWorklet、Go网关和真实灵矽：

| 原话 | Go实际结果 | 后续回复 |
| --- | --- | --- |
| 使用回旋镖攻击爸爸的小熊号 | 16→13，版本2，库存0 | 说出回旋镖及后退3格 |
| 使用救援卡救援小朋友的小熊号 | 12→0，版本2，库存0 | 说出撤回起飞位 |
| 使用团队护航 | 两架飞机加盾，版本2，库存0 | 说出团队护航与护盾 |

三句均不掷骰、用卡统计1，动作确认后的实际语音转写与音频流匹配；不是只检查game_action文案，也不把连接开场音频当作回复。本地与公网各通过三种新卡；不是新增真人录音/扬声器回声测试。

曾实际发现护航执行后重复旧开场提示：现在发送session.update后等待返回instructions与本轮匹配的session.updated，才response.create。旧确认不触发生成，8秒未确认则提示重连并保留游戏结果；受控WS回归确认缺失/旧确认时不生成。护航事实文案同时去除逐架重复说明。未采用本供应商未验证可用的out-of-band回复参数。

公网五句真实灵矽掷骰回归：刻意延迟棋局WS，语音确认仍驱动一次4点弹窗；否定/含糊/过去时问句/待选子再掷均不增加版本，最终版本3、投掷统计1。既有幸运骰/引导/结束流程通过；Go vet/race、42项前端测试/构建通过。GitHub实际[特效CI](https://github.com/zkevindev/flytogether/actions/runs/40083165193)及[最终应用CI](https://github.com/zkevindev/flytogether/actions/runs/40083361212)两任务和Codecov上传通过；此CI不运行真实平台或浏览器脚本。

## 复跑

```sh
go test -race ./...
go vet ./...
npm --prefix web run test:unit
npm --prefix web run build
QA_BASE_URL=http://127.0.0.1:8080 npm --prefix web run test:props:browser
python3 scripts/voice_fixture.py --scenario boomerang --output-dir /tmp/prop-audio/boomerang
python3 scripts/voice_fixture.py --scenario rescue --output-dir /tmp/prop-audio/rescue
python3 scripts/voice_fixture.py --scenario escort --output-dir /tmp/prop-audio/escort
QA_BASE_URL=http://127.0.0.1:8080 QA_PROP_AUDIO_DIR=/tmp/prop-audio npm --prefix web run test:props:voice
```

语音需要本机/服务端私有配置，不能把密钥放仓库或浏览器。MCP核对另见[LINGXI-MCP](LINGXI-MCP.md)，本轮没有切换生产游戏写工具为MCP。

部署前在线备份，运行二进制与13份JS/CSS资源逐一核对一致，schema5、持久数据库和配置文件权限600，语音配置保留。当前10张界面由05de003公网重拍；特效四张来源1a6eb72。最终卡片布局又通过11例本地点击/联网复跑，未改规则或音频链路。
