# 3D 棋盘回归记录

2026-10-23，跟踪 #9、#10、#11。Chrome 实际渲染页面，使用 Playwright 控制时钟推进动画；独立浏览器会话通过正常房间接口建立测试棋局，骰子来自 Go 随机结果。

| 场景 | 结果 |
| --- | --- |
| 桌面棋盘与 390 像素手机视口 | 四角机场完整；16 个飞机编号均在视口内；昵称可查对照 |
| 30 次点击掷骰、弹窗关闭，轮换经典/长航线/环形新局 | 仅分配 1 个 WebGL 上下文；0 次非预期上下文丢失 |
| 返回相同的四人经典棋盘 | GPU 资源恢复到基线：378 buffers、17 textures、5 programs、4 framebuffers |
| 原生 WEBGL_lose_context 模拟丢失 | 自动使用可操作的平面棋盘，继续点击掷骰成功 |
| 原生上下文自动恢复 | 重新显示 3D，棋局 ID 和版本保留 |
| 再次丢失后点击恢复按钮 | 只有 1 个有效上下文，棋局保留 |
| WebGL 初始化不可用 | 页面不白屏，平面玩法和 CSS 3D 骰子弹窗可用 |
| 骰子弹窗期间收到走棋快照 | 仍显示机库且没有起飞音效调度；关闭后进入起飞位，起飞音效调度一次 |

资源计数通过浏览器原生 GL 分配与删除调用记录；上下文丢失会由浏览器销毁旧 GPU 资源，因此测试同步清空该上下文计数。不同路线和合法走法高亮可能需要不同资源，比较基线时使用相同棋盘与状态。模拟故障时 Three.js 输出 Context Lost/Restored 属于预期事件，不计为自发故障。

复跑：先启动应用，再进入 `web` 安装依赖并运行。默认使用本机 Chrome；也可安装 Playwright Chromium 并设置 `QA_BROWSER_CHANNEL=chromium`。

```sh
npm ci
npm run test:scene
QA_BASE_URL=http://127.0.0.1:8080 npm run test:scene:browser
```

脚本、截图输出默认位于被 Git 忽略的 `web/test-results/scene/`，可用 `QA_OUTPUT_DIR` 改为本机其他目录。`QA_BASE_URL` 仅作为测试运行参数，不保存真实部署目标。测试会在独立会话中创建并反复重开测试棋局。

截图：[桌面](images/board-3d-desktop.png)、[手机](images/board-3d-mobile.png)、[故障降级](images/board-3d-recovery.png)。

覆盖范围：渲染顺序、几何取景、资源回收和故障恢复。未覆盖所有真实显卡、长期挂机、真实麦克风回声、多人家庭试玩；这些仍由 #3 跟踪。

## 新手与落点预览追加回归（2026-10-23，#18）

应用 [c535364](https://github.com/zkevindev/flytogether/commit/c535364) 在本地和公网 Chrome 验证：首次引导、前后步骤、完成/跳过保存、刷新和重看不修改棋局；点选、换选和取消预览不提交动作，连续确认只执行一次。暂停、新局或其他输入完成动作后清除旧预览。

使用 Go 的演示起点和真实随机掷骰准备跳格、飞行、撞机、终点、护盾局面，预览取自服务端 legal_moves，并逐一核对实际落点与受影响飞机。手机视口的预览卡片和顶栏无横向溢出，受控禁用 WebGL 后 2D 预览与确认可用。没有注入飞机位置或指定骰子。

30 次掷骰/切棋盘/恢复回归仍保留 1 个渲染上下文；相同棋盘资源数一致，新预览复用场景资源。当前截图见 [清单](images/current/manifest.json)；手机视口不等于真实手机验收，家庭试玩仍归 #3。

```sh
QA_BASE_URL=http://127.0.0.1:8080 npm --prefix web run test:onboarding:browser
QA_BASE_URL=http://127.0.0.1:8080 npm --prefix web run test:scene:browser
```
