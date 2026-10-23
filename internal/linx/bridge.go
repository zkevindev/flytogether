// Package linx connects the browser audio client to Linx's Realtime API.
package linx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
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
	up, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{Subprotocols: []string{"realtime", "openai-insecure-api-key." + b.Config.APIKey}})
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
			r.VoiceState("ready")
			break
		}
	}

	var responseHasTools bool
	var pendingCalls []functionCall
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-errCh:
			if ctx.Err() != nil {
				return nil
			}
			return e
		default:
		}
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
		switch event.Type {
		case "input_audio_buffer.speech_started":
			if e = r.BeginSpeech(); e != nil {
				// A duplicate VAD edge can occur while the same utterance is active.
				if voiceErr, ok := e.(*room.Error); !ok || voiceErr.Code != "VOICE_BUSY" {
					return e
				}
			}
			_ = sendBrowser(map[string]any{"type": "speech_started"})
		case "input_audio_buffer.speech_stopped":
			r.VoiceState("thinking")
			_ = sendBrowser(map[string]any{"type": "speech_stopped"})
		case "conversation.item.input_audio_transcription.completed":
			var body struct {
				Transcript string `json:"transcript"`
			}
			_ = json.Unmarshal(event.Raw, &body)
			_ = sendBrowser(map[string]any{"type": "stt", "text": body.Transcript})
		case "response.created":
			responseHasTools = false
		case "response.output_item.done":
			var body struct {
				Item functionCall `json:"item"`
			}
			if json.Unmarshal(event.Raw, &body) == nil && body.Item.Type == "function_call" {
				responseHasTools = true
				pendingCalls = append(pendingCalls, body.Item)
			}
		case "response.audio.delta":
			if r.Snapshot().Voice != "speaking" {
				r.VoiceState("speaking")
			}
		case "response.done":
			if len(pendingCalls) > 0 {
				for _, call := range pendingCalls {
					output := b.executeTool(r, call)
					item := map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": output}}
					if e = sendUp(item); e != nil {
						return errors.New("游戏操作结果没有送回灵矽")
					}
				}
				pendingCalls = nil
				if e = sendUp(map[string]any{"type": "response.create"}); e != nil {
					return errors.New("灵矽无法继续生成语音回复")
				}
				responseHasTools = false
				continue
			}
			if !responseHasTools && r.Snapshot().Voice != "listening" {
				r.VoiceState("ready")
			}
		case "error":
			_ = sendBrowser(map[string]any{"type": "error", "message": "灵矽实时语音遇到问题，请重新说一次"})
		default:
		}
		if shouldForward(event.Type) {
			if e = browser.Write(ctx, websocket.MessageText, data); e != nil {
				return e
			}
		}
	}
}

type realtimeEvent struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

func (e *realtimeEvent) UnmarshalJSON(data []byte) error {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	e.Type, e.Raw = header.Type, append(e.Raw[:0], data...)
	return nil
}

type functionCall struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
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
		"type": "realtime", "model": model, "output_modalities": []string{"audio", "text"},
		"instructions": "你是一起飞家庭飞行棋的语音搭子。用简短、亲切的中文交流。棋局状态和规则以工具结果为准；掷骰或移动必须先查询状态，并且只能按玩家明确指令操作。棋子不明确时先追问。一次话语最多执行一个会改变棋局的动作。操作成功后根据工具返回播报结果，不得声称未执行的动作成功。",
		"audio": map[string]any{
			"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 16000}, "transcription": map[string]any{},
				"turn_detection": map[string]any{"type": "server_vad", "create_response": true, "interrupt_response": true}},
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
		args["action_token"] = r.CurrentActionToken()
	}
	value, err := r.Tool(call.Name, args)
	if err != nil {
		encoded, _ := json.Marshal(map[string]string{"error": err.Error(), "instruction": "操作未执行。请按错误说明重新查询状态或向用户澄清。"})
		return string(encoded)
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
		"response.output_text.delta", "response.output_text.done", "conversation.item.input_audio_transcription.completed",
		"input_audio_buffer.speech_started", "input_audio_buffer.speech_stopped", "response.created", "response.done":
		return true
	default:
		return false
	}
}
