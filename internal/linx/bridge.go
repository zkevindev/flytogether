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
			if control.Type == "abort" {
				if e = sendUp(map[string]any{"type": "response.cancel"}); e != nil {
					errCh <- e
					return
				}
			} else if control.Type == "sleep" {
				select {
				case controls <- "sleep":
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
	grants := map[string]string{}
	calls := map[string][]functionCall{}
	seenInputs := map[string]bool{}
	idle := func() {
		if gate.awake(time.Now()) {
			r.VoiceState("ready")
		} else {
			r.VoiceState("standby")
		}
	}
	sleep := func() error {
		if inFlight {
			_ = sendUp(map[string]any{"type": "response.cancel"})
		}
		gate.sleep()
		currentToken, inFlight = "", false
		idle()
		return sendWake()
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
		case <-controls:
			if e := sleep(); e != nil {
				return e
			}
			continue
		case now := <-ticker.C:
			if settings := r.VoiceSettings(); settings != gate.settings {
				gate.settings = settings
				if e := sleep(); e != nil {
					return e
				}
			} else if !inFlight && !gate.awake(now) && r.Snapshot().Voice != "standby" {
				idle()
				if e := sendWake(); e != nil {
					return e
				}
			}
			continue
		case event = <-incoming:
		}
		switch event.Type {
		case "input_audio_buffer.speech_started":
			// Revoke the previous utterance before accepting a new transcript.
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
				args, _ := json.Marshal(map[string]string{"piece_id": action.PieceID})
				if action.Name != "move_piece" {
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
				response["response"] = map[string]any{"tool_choice": "none", "instructions": "只根据本轮工具结果简短回答。结果含error或notice时没有执行操作，按真实棋局回答问题、解释原因或追问选择，不能宣称已操作成功。本轮不要再调用工具。不要朗读内部ID和技术字段。"}
			}
			if e := sendUp(response); e != nil {
				return e
			}
			continue
		case "response.created":
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
			if inFlight {
				currentToken, inFlight = "", false
				idle()
				_ = sendBrowser(map[string]any{"type": "error", "message": "灵矽实时语音遇到问题，请重新说一次"})
			}
			continue
		}
		if !inFlight || grants[event.ResponseID] != currentToken {
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
		"instructions": "你是一起飞家庭飞行棋的语音搭子。用简短、亲切的中文交流。只用玩家昵称、棋子昵称或序号与玩家交流，不朗读内部ID、工具名和技术字段。用户仅呼唤你时简短回应我在，不操作棋局。棋局状态和规则以工具结果为准；掷骰或移动必须先查询状态，并且只能按玩家明确指令操作。玩家明确指定合法棋子起飞或移动时，调用move_piece实际执行，再播报真实结果。多个候选且玩家没指定时先追问。起飞需要真实掷出六点，不能跳过规则。一次话语最多执行一个会改变棋局的动作。操作成功后根据工具返回播报结果，不得声称未执行的动作成功。",
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
	}
}

func (b *Bridge) executeTool(r *room.Room, call functionCall) string {
	args := map[string]string{}
	if call.Arguments != "" && json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return `{"error":"工具参数无效"}`
	}
	if call.Name == "roll_dice" || call.Name == "move_piece" {
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
