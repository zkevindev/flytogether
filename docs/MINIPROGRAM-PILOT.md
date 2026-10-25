# 微信小程序小范围内测

当前实现是一个极简 `web-view` 壳：棋局、房间、语音和 Go 服务仍由现有 FlyTogether 网页承担，小程序不复制游戏逻辑，也不保存灵矽 API Key。它用于验证微信内打开现有网页的体验，不是原生小程序重写。

## 本机准备

1. 安装并打开微信开发者工具。
2. 在仓库根目录执行 `cp miniprogram/config.example.js miniprogram/config.js`。
3. 编辑被 Git 忽略的 `miniprogram/config.js`，把 `webViewUrl` 设置为体验网页的 HTTPS 地址。不要把真实部署地址复制到示例文件或提交到仓库。
4. 在微信开发者工具导入 `miniprogram/` 目录。`project.config.json` 的 `touristappid` 仅供本地开发工具预览；要上传体验版，必须在本机项目配置中换成你自己的小程序 AppID。不要提交真实 AppID 或 `project.private.config.json`。
5. 本地工具预览时，如尚未配置业务域名，可在开发者工具「详情 → 本地设置」临时打开“不校验合法域名、web-view（业务域名）、TLS 版本以及 HTTPS 证书”。该选项只影响当前开发者工具的本地调试，不能让体验成员的微信客户端绕过校验。

## 体验版分发

体验版需由有权限的小程序管理员/开发成员上传，然后在小程序后台把内测者加入体验成员，并分享体验版入口。体验版仅对有权限的成员开放；分享二维码或链接不会把它变成任何人都能打开的公开网页。

微信客户端加载 `web-view` 前，需要在小程序后台配置匹配的业务域名，并满足微信对 HTTPS 证书及域名的要求。开发者工具的校验绕过不影响真机。当前演示域名不能默认认为可直接通过业务域名校验；如果后台拒绝该域名，需要换成自己控制且符合微信要求的域名，并按平台提示完成备案和 HTTPS 配置。正式发布还需完成小程序平台要求的审核与配置。

官方入口：[web-view 组件](https://developers.weixin.qq.com/miniprogram/dev/component/web-view.html)、[小程序网络配置](https://developers.weixin.qq.com/miniprogram/dev/framework/ability/network.html)。具体可配置规则以微信公众平台当前提示为准。

## 验收范围与限制

- 在开发者工具先检查加载、房间创建/加入、棋局点击操作和断网恢复。
- 必须用真实 iOS/Android 微信客户端验证 H5 麦克风权限、录音采集、Realtime WebSocket（WSS）、扬声器播放、页面切后台/返回和语音打断。开发者工具通过不代表真机语音通过。
- 语音不可用时，现有网页点击玩法应可继续使用；小程序壳本身不接触灵矽密钥。
- 网页登录/房间流程沿用现有 Web 实现。如果微信 `web-view` 的 Cookie、麦克风授权或 WebSocket 行为与普通浏览器不同，需要再做兼容适配后才能扩大内测。

## 内测前需要项目管理员提供

- 已注册的小程序 AppID，并确认有上传体验版的开发者权限（只需在本机配置，不要提交仓库）。
- 一组内测微信号，供管理员在微信公众平台添加为体验成员。
- 一个已配置为该小程序业务域名的 HTTPS 网页域名。不要把服务器 IP、SSH 凭据、AppSecret 或灵矽 API Key 放进仓库或前端。
