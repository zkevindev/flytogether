# 对局与语音排障后台

从游戏同域名的 `/admin` 访问。入口独立于玩家页面，管理页面不会申请麦克风或加入房间；管理脚本和样式按需加载。管理员只能查询，不能从后台改骰子、移动飞机或结束玩家对局。

## 访问与配置

| 变量 | 默认 / 作用 |
| --- | --- |
| `ADMIN_ACCESS_KEY` | 空则关闭后台登录和新记录。启用时需要32～256字符的独立随机密钥 |
| `DIAGNOSTICS_RETENTION_HOURS` | 168小时，可配置1～2160小时 |
| `DIAGNOSTICS_TRANSCRIPTS` | true；false时不保存新语音文本，仍记录连接、工具和事件元数据 |

本机用Git忽略的 `.env`，服务器用发布目录外的 `/etc/flytogether.env`。真实值不得进入Git、Issue、录屏、截图或命令输出。生成密钥到私人文件，再通过环境变量传入，例如：

```sh
mkdir -p ~/.config/flytogether
(umask 077; openssl rand -hex 24 > ~/.config/flytogether/admin-key)
export ADMIN_ACCESS_KEY="$(cat ~/.config/flytogether/admin-key)"
```

配置在启动时读取，修改后需重启。登录表单使用密码输入，提交后清空；浏览器不保存密钥。管理员会话独立于玩家Cookie，8小时有效，HttpOnly、SameSite=Strict，配置HTTPS时Secure；退出立即撤销当前会话，进程重启或更换密钥后旧会话不可用。生产通过HTTPS访问。登录失败限流按服务端直连地址计数，不信任客户端伪造的转发头；反向代理部署会共用该地址的一分钟失败预算。

## 查看一局

1. 按房间名称、房间编号或对局编号搜索，可筛选等待、进行、暂停、主动结束或自然获胜。每页最多50局，页面默认25局。
2. 同一房间每次重开都有独立对局编号，前一局保留其最终快照、统计和事件；玩家主动结束也可查看，但不虚构冠军。删除玩家存档不级联删除排障记录。
3. 详情包括记录起点、首个记录版本、最新棋局版本、真实玩家统计，展开可看飞机位置、库存与合法走法。
4. 时间线最新在上，可按棋局、操作、全部语音、转写、回复、工具、异常、连接或TTS筛选；每次读取最多100条，页面默认50条，可加载更早记录。刷新重新读取权威记录，不改变棋局。

首次启用仅为当前存档建立起点快照，无法恢复已经丢失的对话或完整棋步历史。旧存档中的有限游戏历史只作为快照内容，不能当作带真实发生时间的排障事件。所有时间都是服务器记录时间，界面按浏览器本地时区显示。

## 语音记录怎么串起来

- `session_id` 是新生成的排障会话编号，不是玩家Cookie或语音动作授权。
- `input_id` 将一条最终转写、工具执行结果、助手最终文本与首段音频关联起来；`ref` 是上游响应/发言或游戏事件编号。
- Go持久化的棋步和操作带棋局版本，语音工具记录区分完成、拒绝与只读；动作成功以Go棋局事件和快照为准。系统房间更新也包含机器人行动。
- 助手文本来自上游最终assistant消息，不存音频delta、二进制帧或整个协议包。打断/断开或上游未给最终文本时可能没有完整回复，空文本会明确显示。
- 首段音频时间是服务端收到最终转写到收到首段上游PCM的时间，不是设备实际播放或端到端声学时延。
- 输出式TTS区分静默、合成失败、旧回合丢弃和已合成返回；已合成返回不等于玩家听到了声音。
- 浏览器可上报麦克风拒绝、声音未解锁、回复过长、解码失败、连接中断或连接失败。只接受原房间成员和当前game_id的固定类别，不接受任意日志/堆栈/文本；每局每类别10秒内合并。未成功发送的浏览器故障不会凭空出现在服务器。

## 保存与边界

使用现有SQLite中的 `diagnostic_matches`、`diagnostic_events`。数据库版本升级到4，保留原存档、家庭资料和战绩。对局变化与对应排障快照/棋步在同一个事务中提交；点击重试不额外生成成功操作，任何事务写入失败回滚棋局。语音传输记录独立写入，失败计数在后台可见，刷新更新；进程重启后此计数归零，不能当作完整历史故障统计。

不记录API Key、Authorization、Cookie、邀请凭证、动作令牌或原始录音。文本限制2000字符并屏蔽常见凭据模式；不能保证识别所有自由表达中的个人信息，管理员应限制访问并避免在对讲中口述敏感信息。启用文本记录时玩家页面有保留期提示。

过期事件查询立即不可见，启动与每小时清理数据库旧事件、闲置对局。活跃对局的旧语音事件也按各自时间过期。关闭文本记录只影响之后的新文本，不立即抹掉已有记录；已有记录继续遵循保留期。SQLite删除不是安全擦除，备份有独立生命周期，需要管理员一并保管和清理。语音记录量主要受会话/每日额度和保留期限制，未实现独立磁盘字节配额。

升级前应做在线SQLite备份；schema4旧程序会拒绝启动，回滚需停止服务并恢复升级前的匹配数据库备份，不能仅切换旧二进制。已有备份与权限说明见 [SAVES](SAVES.md)。

## 复跑与证据

```sh
go vet ./...
go test -race ./...
npm --prefix web run test:unit
npm --prefix web run build
QA_BASE_URL=http://127.0.0.1:8080 QA_ADMIN_KEY_FILE=/path/to/private/admin-key npm --prefix web run test:admin:browser
QA_BASE_URL=http://127.0.0.1:8080 QA_ADMIN_KEY_FILE=/path/to/private/admin-key npm --prefix web run test:admin:client-error
```

真实语音六句联调后，可用相同管理员密钥验证已记录内容与实际页面：

```sh
QA_BASE_URL=http://127.0.0.1:8080 QA_AUDIO_FILE=/path/to/synthetic/browser-input.wav QA_COMPANION_STYLE=coach QA_COMPANION_FREQUENCY=quiet npm --prefix web run test:voice:realtime
QA_BASE_URL=http://127.0.0.1:8080 QA_ADMIN_KEY_FILE=/path/to/private/admin-key npm --prefix web run test:admin:records
```

后一个脚本读取真实完成的“直接对话联调”记录，不注入对话、音频或棋局数据。音源准备见 [QA-VOICE](QA-VOICE.md)，验证范围见 [QA-ADMIN](QA-ADMIN.md)。脚本生成的JSON、截图默认在Git忽略的 `web/test-results/`；仅人工检查过的合成QA截图进入文档。


已检查的界面证据：[登录](images/admin/admin-login.png) · [对局详情](images/admin/admin-match.png) · [语音时间线](images/admin/admin-voice.png) · [手机视口](images/admin/admin-mobile.png)。来源/视口/哈希见 [manifest](images/admin/manifest.json)。
