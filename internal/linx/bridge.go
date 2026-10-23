// Package linx connects the browser audio client to Linx's Realtime API.
package linx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

type Config struct{ URL, APIKey, Model string }

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.Model) != "" && strings.TrimSpace(c.URL) != ""
}

type Bridge struct {
	Config Config
	mu     sync.Mutex
	active bool
}

func (b *Bridge) Serve(ctx context.Context, browser *websocket.Conn, r *room.Room) error {
	b.mu.Lock()
	if b.active {
		b.mu.Unlock()
		return errors.New("另一间房正在使用语音，请稍后再试")
	}
	b.active = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.active = false; b.mu.Unlock(); r.VoiceState("disconnected") }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	endpoint, err := realtimeURL(b.Config.URL, b.Config.Model)
	if err != nil {
		return errors.New("灵矽实时语音地址或模型配置无效")
	}
	dialCtx, stop := context.WithTimeout(ctx, 12*time.Second)
	up, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{
		Subprotocols: []string{"realtime"},
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + b.Config.APIKey}},
	})
	stop()
	if err != nil {
		return errors.New("灵矽实时连接失败，请检查服务器上的 API Key 和模型配置")
	}
	defer up.CloseNow()
	up.SetReadLimit(1 << 20)
	browser.SetReadLimit(1 << 20)
	var upWrite sync.Mutex
	sendUp := func(value any) error {
		data, e := json.Marshal(value)
		if e != nil {
			return e
		}
		upWrite.Lock()
		defer upWrite.Unlock()
		writeCtx, done := context.WithTimeout(ctx, 8*time.Second)
		defer done()
		return up.Write(writeCtx, websocket.MessageText, data)
	}
	sendBrowser := func(value any) error {
		data, e := json.Marshal(value)
		if e != nil {
			return e
		}
		writeCtx, done := context.WithTimeout(ctx, 8*time.Second)
		defer done()
		return browser.Write(writeCtx, websocket.MessageText, data)
	}

	errCh := make(chan error, 2)
	controls := make(chan string, 4)
	gate := wakeGate{settings: r.VoiceSettings()}
	sendWake := func() error {
		return sendBrowser(map[string]any{"type": "wake_state", "awake": gate.awake(time.Now()), "wake_enabled": gate.settings.WakeEnabled, "wake_word": gate.settings.WakeWord, "window_seconds": int(wakeWindow.Seconds())})
	}
	go func() {
		defer cancel()
		for {
			kind, data, e := browser.Read(ctx)
			if e != nil {
				errCh <- e
				return
			}
			if kind == websocket.MessageBinary {
				if len(data) > 6400 {
					errCh <- errors.New("录音数据帧过大")
					return
				}
				if e = sendUp(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(data)}); e != nil {
					errCh <- e
					return
				}
				continue
			}
			var control struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &control) != nil {
				errCh <- errors.New("语音控制消息无效")
				return
			}
			if control.Type == "abort" || control.Type == "sleep" {
				select {
				case controls <- control.Type:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// The provider creates the session first. Install server-owned audio, VAD,
	// instructions, and game tools before allowing the browser to send audio.
	for {
		kind, data, e := up.Read(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("灵矽实时会话已断开")
		}
		if kind != websocket.MessageText {
			continue
		}
		var event realtimeEvent
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		if event.Type == "error" {
			return errors.New("灵矽拒绝了实时会话，请检查模型、API Key 和控制台配置")
		}
		if event.Type == "session.created" {
			if e = sendUp(realtimeSessionUpdate(b.Config.Model)); e != nil {
				return errors.New("灵矽实时会话初始化失败")
			}
			break
		}
	}

	// Do not start the microphone stream until the provider confirms the
	// negotiated session configuration.
	for {
		kind, data, e := up.Read(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("灵矽实时会话初始化超时")
		}
		if kind != websocket.MessageText {
			continue
		}
		var event realtimeEvent
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		if event.Type == "error" {
			return errors.New("灵矽没有接受实时音频配置，请确认该模型支持 PCM 和语音工具")
		}
		if event.Type == "session.updated" {
			if e = sendBrowser(map[string]any{"type": "hello", "audio_params": map[string]any{"format": "pcm", "sample_rate": 24000}}); e != nil {
				return e
			}
			if gate.settings.WakeEnabled {
				r.VoiceState("standby")
			} else {
				r.VoiceState("ready")
			}
			if e = sendWake(); e != nil {
				return e
			}
			break
		}
	}

	incoming := make(chan realtimeEvent, 16)
	go func() {
		for {
			kind, data, err := up.Read(ctx)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			if kind != websocket.MessageText {
				continue
			}
			var event realtimeEvent
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			select {
			case incoming <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var currentToken string
	var inFlight bool
	var pendingHost bool
	var hostSince time.Time
	var seenEvent string
	var cheered Cheer
	hostIDs := map[string]bool{}
	grants := map[string]string{}
	calls := map[string][]functionCall{}
	seenInputs := map[string]bool{}
	hostActive := func() bool { return pendingHost || len(hostIDs) > 0 }
	cancelHost := func() {
		if !hostActive() {
			return
		}
		_ = sendUp(map[string]any{"type": "response.cancel"})
		pendingHost = false
		hostIDs = map[string]bool{}
		_ = sendBrowser(map[string]any{"type": "host_cancel"})
	}
	speakHost := func(script string) error {
		if strings.TrimSpace(script) == "" || hostActive() || inFlight {
			return nil
		}
		if e := sendBrowser(map[string]any{"type": "host_cue"}); e != nil {
			return e
		}
		pendingHost = true
		hostSince = time.Now()
		r.VoiceState("speaking")
		return sendUp(map[string]any{"type": "response.create", "response": map[string]any{
			"tool_choice":  "none",
			"instructions": "你是家庭飞行棋主持人。只把下面这句话说出来，可以稍微说顺，不要增加事实，不要调用工具：" + script,
		}})
	}
	idle := func() {
		if gate.awake(time.Now()) {
			r.VoiceState("ready")
		} else {
			r.VoiceState("standby")
		}
	}
	sleep := func() error {
		cancelHost()
		if inFlight {
			_ = sendUp(map[string]any{"type": "response.cancel"})
		}
		gate.sleep()
		currentToken, inFlight = "", false
		idle()
		return sendWake()
	}
	if state, status, ok := roomGame(r); ok && status == "playing" {
		seenEvent = lastEventID(state)
		if e := speakHost(hostCue(state)); e != nil {
			return e
		}
	}
	for {
		var event realtimeEvent
		select {
		case <-ctx.Done():
			return nil
		case <-errCh:
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("灵矽实时会话已断开")
		case control := <-controls:
			if control == "sleep" {
				if e := sleep(); e != nil {
					return e
				}
			} else {
				cancelHost()
				if inFlight {
					_ = sendUp(map[string]any{"type": "response.cancel"})
				}
				currentToken, inFlight = "", false
				idle()
				if e := sendWake(); e != nil {
					return e
				}
			}
			continue
		case now := <-ticker.C:
			if settings := r.VoiceSettings(); settings != gate.settings {
				gate.settings = settings
				if e := sleep(); e != nil {
					return e
				}
			} else if !inFlight && !hostActive() && !gate.awake(now) && r.Snapshot().Voice != "standby" {
				idle()
				if e := sendWake(); e != nil {
					return e
				}
			}
			if pendingHost && time.Since(hostSince) > 8*time.Second {
				pendingHost = false
			}
			if !inFlight && !hostActive() {
				voice := r.Snapshot().Voice
				if state, status, ok := roomGame(r); ok && status == "playing" && voice != "listening" && voice != "thinking" {
					script, mark := nextHostLine(state, seenEvent)
					if script != "" {
						if note := cheered.take(state); note != "" {
							script = note + script
						}
						if e := speakHost(script); e != nil {
							return e
						}
						if pendingHost {
							seenEvent = mark
						}
					} else if mark != "" {
						seenEvent = mark
					}
				}
			}
			continue
		case event = <-incoming:
		}
		switch event.Type {
		case "input_audio_buffer.speech_started":
			// Revoke the previous utterance before accepting a new transcript.
			if hostActive() {
				_ = sendUp(map[string]any{"type": "response.cancel"})
				pendingHost = false
				hostIDs = map[string]bool{}
				if !gate.awake(time.Now()) {
					if e := sendBrowser(map[string]any{"type": "host_cancel"}); e != nil {
						return e
					}
				}
			}
			currentToken, inFlight = "", false
			r.VoiceState("standby")
			if gate.awake(time.Now()) {
				r.VoiceState("listening")
				if e := sendBrowser(map[string]any{"type": "speech_started"}); e != nil {
					return e
				}
			}
			continue
		case "input_audio_buffer.speech_stopped":
			if gate.awake(time.Now()) {
				r.VoiceState("thinking")
				if e := sendBrowser(map[string]any{"type": "speech_stopped"}); e != nil {
					return e
				}
			}
			continue
		case "conversation.item.input_audio_transcription.completed":
			var body struct {
				Transcript string `json:"transcript"`
				ItemID     string `json:"item_id"`
			}
			if json.Unmarshal(event.Raw, &body) != nil || seenInputs[body.ItemID] {
				continue
			}
			if body.ItemID != "" {
				seenInputs[body.ItemID] = true
			}
			if !gate.accept(body.Transcript, time.Now()) {
				// Keep unrelated background speech out of later model context.
				if body.ItemID != "" {
					_ = sendUp(map[string]any{"type": "conversation.item.delete", "item_id": body.ItemID})
				}
				idle()
				if e := sendWake(); e != nil {
					return e
				}
				continue
			}
			r.VoiceState("standby")
			if e := r.BeginSpeech(); e != nil {
				return e
			}
			currentToken, inFlight = r.CurrentActionToken(), true
			r.VoiceState("thinking")
			if e := sendWake(); e != nil {
				return e
			}
			if e := sendBrowser(map[string]any{"type": "stt", "text": body.Transcript}); e != nil {
				return e
			}
			response := map[string]any{"type": "response.create"}
			if action, handled := planVoiceAction(r, gate.commandText(body.Transcript)); handled {
				before := ""
				if state, _, ok := roomGame(r); ok {
					before = lastEventID(state)
				}
				if action.Cheer > 0 {
					cheered = Cheer{Die: action.Cheer, After: before}
				}
				args, _ := json.Marshal(map[string]string{"piece_id": action.PieceID})
				if action.Name == "use_prop" {
					args, _ = json.Marshal(map[string]string{"prop_id": action.PropID, "piece_id": action.PieceID})
				} else if action.Name != "move_piece" {
					args = []byte(`{}`)
				}
				call := functionCall{Type: "function_call", Name: action.Name, CallID: "voice-" + game.ID(), Arguments: string(args), Grant: currentToken}
				output := ""
				if action.Notice != "" {
					output = actionOutput(r, action.Notice)
				} else {
					output = b.executeTool(r, call)
				}
				// Commit explicit commands before requesting any success narration.
				for _, item := range []any{
					call,
					map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": output},
				} {
					if e := sendUp(map[string]any{"type": "conversation.item.create", "item": item}); e != nil {
						return errors.New("游戏操作结果没有送回灵矽")
					}
				}
				instructions := "只根据本轮工具结果用一两句日常中文回答，语气像主持人，优先说实际效果或一条具体建议；除非玩家问库存，不要逐项报剩余道具。结果含error或notice时没有执行操作，按真实棋局回答问题、解释原因或追问选择，不能宣称已操作成功。需要掷骰时请玩家说‘掷骰子’，点数由游戏服务随机生成，不能询问、接受或编造玩家自报点数。本轮不要再调用工具。玩家称呼只取工具中的昵称，唤醒词是助手的名字，不是玩家姓名。不要朗读内部ID和技术字段，不要提授权或操作次数用完；用过道具不表示回合结束，玩家可以在下一句话掷骰。"
				if action.Notice != "" {
					instructions += "本轮具体要求（必须遵守）：“" + action.Notice + "”。"
				}
				if state, _, ok := roomGame(r); ok {
					seenEvent = lastEventID(state)
					if action.Notice == "" {
						instructions += "先说这次真实发生了什么，再说：" + hostCue(state) + "。"
					}
					if note := cheered.take(state); note != "" {
						instructions += "把这句也说进去：" + note
					}
					switch state.Phase {
					case "move":
						instructions += "当前已经掷骰，正在等待选择飞机。禁止要求再次掷骰，必须根据合法候选询问选择。"
					case "roll":
						instructions += "当前正在等待掷骰。不能邀请玩家直接移动飞机。"
					}
				}
				response["response"] = map[string]any{"tool_choice": "none", "instructions": instructions}
			}
			if e := sendUp(response); e != nil {
				return e
			}
			continue
		case "response.created":
			if pendingHost && event.ResponseID != "" {
				hostIDs[event.ResponseID] = true
				pendingHost = false
			}
			if inFlight {
				grants[event.ResponseID] = currentToken
			}
		case "response.output_item.done":
			if !inFlight || grants[event.ResponseID] != currentToken {
				continue
			}
			var body struct {
				Item functionCall `json:"item"`
			}
			if json.Unmarshal(event.Raw, &body) == nil && body.Item.Type == "function_call" {
				body.Item.Grant = grants[event.ResponseID]
				calls[event.ResponseID] = append(calls[event.ResponseID], body.Item)
			}
		case "response.done":
			if hostIDs[event.ResponseID] {
				delete(hostIDs, event.ResponseID)
				pendingHost = false
				if !inFlight {
					idle()
					if e := sendWake(); e != nil {
						return e
					}
				}
				if e := browser.Write(ctx, websocket.MessageText, event.Raw); e != nil {
					return e
				}
				continue
			}
			pending := calls[event.ResponseID]
			delete(calls, event.ResponseID)
			grant := grants[event.ResponseID]
			delete(grants, event.ResponseID)
			if !inFlight || grant == "" || grant != currentToken {
				continue
			}
			var body struct {
				Response struct {
					Status string `json:"status"`
				} `json:"response"`
			}
			_ = json.Unmarshal(event.Raw, &body)
			if body.Response.Status == "completed" && len(pending) > 0 {
				for _, call := range pending {
					output := b.executeTool(r, call)
					item := map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": output}}
					if e := sendUp(item); e != nil {
						return errors.New("游戏操作结果没有送回灵矽")
					}
				}
				if e := sendUp(map[string]any{"type": "response.create"}); e != nil {
					return errors.New("灵矽无法继续生成语音回复")
				}
				continue
			}
			currentToken, inFlight = "", false
			gate.finish(time.Now())
			idle()
			if e := sendWake(); e != nil {
				return e
			}
			if e := browser.Write(ctx, websocket.MessageText, event.Raw); e != nil {
				return e
			}
			continue
		case "error":
			if hostActive() {
				pendingHost = false
				hostIDs = map[string]bool{}
				_ = sendBrowser(map[string]any{"type": "host_cancel"})
			}
			if inFlight {
				currentToken, inFlight = "", false
				idle()
				_ = sendBrowser(map[string]any{"type": "error", "message": "灵矽实时语音遇到问题，请重新说一次"})
			}
			continue
		}
		if !hostIDs[event.ResponseID] && (!inFlight || grants[event.ResponseID] != currentToken) {
			continue
		}
		if event.Type == "response.audio.delta" || event.Type == "response.output_audio.delta" {
			if r.Snapshot().Voice != "speaking" {
				r.VoiceState("speaking")
			}
		}
		if shouldForward(event.Type) {
			if e := browser.Write(ctx, websocket.MessageText, event.Raw); e != nil {
				return e
			}
		}
	}
}

type realtimeEvent struct {
	Type       string          `json:"type"`
	ResponseID string          `json:"response_id"`
	Raw        json.RawMessage `json:"-"`
}

func (e *realtimeEvent) UnmarshalJSON(data []byte) error {
	var header struct {
		Type       string `json:"type"`
		ResponseID string `json:"response_id"`
		Response   struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	e.Type, e.Raw = header.Type, append(e.Raw[:0], data...)
	e.ResponseID = header.ResponseID
	if e.ResponseID == "" {
		e.ResponseID = header.Response.ID
	}
	return nil
}

type functionCall struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Grant     string `json:"-"`
}

func realtimeURL(raw, model string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Path != "/v1/realtime" && u.Path != "/v1/realtime/") {
		return "", errors.New("invalid realtime endpoint")
	}
	query := u.Query()
	if existing := query.Get("model"); existing != "" && existing != model {
		return "", errors.New("realtime model mismatch")
	}
	query.Set("model", model)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func realtimeSessionUpdate(model string) map[string]any {
	return map[string]any{"type": "session.update", "session": map[string]any{
		"type": "realtime", "model": model, "output_modalities": []string{"audio"},
		"instructions": "你是一起飞家庭飞行棋的语音主持人。用简短亲切的中文交流，只用玩家和棋子昵称，不朗读内部ID和技术字段。玩家只是呼唤或问怎么办时，根据工具结果说当前一个最有用的选择，不操作棋局。撞某人、走最前面、回家、我要几点，都由游戏服务判断；你只播报工具结果。棋局状态、道具库存和规则以工具结果为准；写操作前先查询状态，只按明确指令操作。合法起飞或移动必须调用move_piece执行，再播报真实结果；多个候选先追问。经典起飞需六点，起飞卡生效时可用任意点数起飞。加速卡对已离开机库的飞机增加两步，幸运骰由游戏服务掷两次取较大值，不能接受玩家自报点数。炸弹可把指定对手公共航线上的飞机送回机场，护盾可保护自己公共航线上的飞机一次。两者都需piece_id，不能选机库、起飞位和终点航道。用户问战术或推荐时，先查状态，根据库存、prop_targets、合法走法给一句具体建议，不擅自操作。道具必须在掷骰前使用，一次掷骰只能用一张。一次话语最多执行一个写动作，使用道具后等待玩家另说掷骰子。不得声称未执行的动作成功。",
		"audio": map[string]any{
			"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 16000}, "transcription": map[string]any{},
				"turn_detection": map[string]any{"type": "server_vad", "create_response": false, "interrupt_response": true}},
			"output": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}},
		},
		"tools": realtimeTools(), "tool_choice": "auto",
	}}
}

func realtimeTools() []map[string]any {
	return []map[string]any{
		{"type": "function", "name": "get_game_state", "description": "查询当前棋局、当前玩家、棋子位置、骰子和合法走法。执行任何棋局操作前先调用。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{"type": "function", "name": "roll_dice", "description": "按当前真人玩家的明确指令掷一次骰子。不能指定骰子点数。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{"type": "function", "name": "move_piece", "description": "移动玩家明确指定的一架合法飞机。piece_id 必须来自刚查询的棋局状态。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"piece_id": map[string]any{"type": "string"}}, "required": []string{"piece_id"}, "additionalProperties": false}},
		{"type": "function", "name": "use_prop", "description": "按玩家明确指令，在掷骰前使用一张库存道具。launch 起飞卡、boost 加速卡、luck 幸运骰、bomb 炸弹、shield 护盾。炸弹和护盾需在prop_targets中明确选择piece_id；其他道具不填写piece_id。使用后不自动掷骰。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"prop_id": map[string]any{"type": "string", "enum": []string{"launch", "boost", "luck", "bomb", "shield"}}, "piece_id": map[string]any{"type": "string", "description": "炸弹或护盾的目标ID，必须来自prop_targets；其他道具省略"}}, "required": []string{"prop_id"}, "additionalProperties": false}},
	}
}

func roomGame(r *room.Room) (game.State, string, bool) {
	view := r.Snapshot()
	var state game.State
	if json.Unmarshal(view.State, &state) != nil {
		return game.State{}, view.Status, false
	}
	return state, view.Status, true
}

func (b *Bridge) executeTool(r *room.Room, call functionCall) string {
	args := map[string]string{}
	if call.Arguments != "" && json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return `{"error":"工具参数无效"}`
	}
	if call.Name == "roll_dice" || call.Name == "move_piece" || call.Name == "use_prop" {
		args["action_token"] = call.Grant
	}
	value, err := r.Tool(call.Name, args)
	if err != nil {
		encoded, _ := json.Marshal(map[string]string{"error": err.Error(), "instruction": "操作未执行。请按错误说明重新查询状态或向用户澄清。"})
		return string(encoded)
	}
	if result, ok := value.(map[string]any); ok {
		delete(result, "action_token")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("工具结果不可用")
	}
	return string(encoded)
}

func shouldForward(event string) bool {
	switch event {
	case "response.audio.delta", "response.audio.done", "response.audio_transcript.delta", "response.audio_transcript.done",
		"response.output_audio.delta", "response.output_audio.done", "response.output_audio_transcript.delta", "response.output_audio_transcript.done",
		"response.output_text.delta", "response.output_text.done", "conversation.item.input_audio_transcription.completed",
		"input_audio_buffer.speech_started", "input_audio_buffer.speech_stopped", "response.created", "response.done":
		return true
	default:
		return false
	}
}
