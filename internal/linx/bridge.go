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
	Observe          func(room.DiagnosticEvent)
	Config           Config
	MaxConnections   int
	SpectatorMax     int
	SessionTimeout   time.Duration
	Reserve          func(context.Context) error
	ReserveSpectator func(context.Context) error
	mu               sync.Mutex
	active           int
	spectators       int
}

func (b *Bridge) Serve(ctx context.Context, browser *websocket.Conn, r room.GameSession, preferences ...CompanionPreferences) (result error) {
	spectator := false
	if marker, ok := r.(interface{ SpectatorVoice() bool }); ok {
		spectator = marker.SpectatorVoice()
	}
	voiceSource := "realtime"
	if spectator {
		voiceSource = "spectator"
	}
	sessionID := game.ID()
	currentInput := ""
	inputTimes := map[string]time.Time{}
	responseInputs := map[string]string{}
	heardAudio := map[string]bool{}
	initial := r.Snapshot()
	var gameState game.State
	_ = json.Unmarshal(initial.State, &gameState)
	trace := func(e room.DiagnosticEvent) {
		if b.Observe != nil {
			e.RoomID = initial.ID
			e.GameID = gameState.ID
			e.SessionID = sessionID

			if e.Source == "" {
				e.Source = voiceSource
			}
			view := r.Snapshot()
			var state game.State
			if json.Unmarshal(view.State, &state) == nil && state.ID == gameState.ID {
				e.Version = state.Version
			}
			b.Observe(e)
		}
	}
	started := time.Now()
	trace(room.DiagnosticEvent{Kind: "connection", Action: "connecting"})
	defer func() {
		code := "closed"
		if result != nil {
			code = "connection_failed"
		}
		trace(room.DiagnosticEvent{Kind: "connection", Action: code, DurationMS: time.Since(started).Milliseconds()})
	}()
	companion := CompanionPreferences{}
	if len(preferences) > 0 {
		companion = preferences[0]
	}
	var prefErr error
	companion, prefErr = companion.Normalized()
	if prefErr != nil {
		return prefErr
	}
	limit := b.MaxConnections
	if limit <= 0 {
		limit = 4
	}
	duration := b.SessionTimeout
	if duration <= 0 {
		duration = 15 * time.Minute
	}
	b.mu.Lock()
	if spectator {
		spectatorLimit := b.SpectatorMax
		if spectatorLimit <= 0 {
			spectatorLimit = 4
		}
		if b.spectators >= spectatorLimit {
			b.mu.Unlock()
			return errors.New("观赛助手名额已满，仍可安静观看棋局")
		}
		b.spectators++
	} else if b.active >= limit {
		b.mu.Unlock()
		return errors.New("另一间房正在使用语音，请稍后再试")
	} else {
		b.active++
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if spectator {
			b.spectators--
		} else {
			b.active--
		}
		b.mu.Unlock()
		r.VoiceState("disconnected")
	}()
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	defer func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result = errors.New("本次语音已到时限，请点击重新连接；仍可点击棋盘继续游戏")
		}
	}()
	if spectator {
		if b.ReserveSpectator != nil {
			if err := b.ReserveSpectator(ctx); err != nil {
				if errors.Is(err, room.ErrVoiceBudget) {
					return errors.New("观赛助手今天的额度已用完，仍可安静观看")
				}
				return errors.New("观赛助手暂时不可用，仍可安静观看")
			}
		}
	} else if b.Reserve != nil {
		if err := b.Reserve(ctx); err != nil {
			return err
		}
	}

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
	trace(room.DiagnosticEvent{Kind: "connection", Action: "connected", DurationMS: time.Since(started).Milliseconds()})
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
	preferencesChanged := make(chan CompanionPreferences, 4)
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
				CompanionPreferences
			}
			if json.Unmarshal(data, &control) != nil {
				errCh <- errors.New("语音控制消息无效")
				return
			}
			if control.Type == "companion_preferences" {
				p, e := control.CompanionPreferences.Normalized()
				if e != nil {
					errCh <- e
					return
				}
				select {
				case preferencesChanged <- p:
				case <-ctx.Done():
					return
				}
				continue
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
			update := realtimeSessionUpdate(b.Config.Model)
			if spectator {
				update = realtimeSpectatorSessionUpdate(b.Config.Model)
			}
			if e = sendUp(update); e != nil {
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
	var allowModelTools bool
	var pendingHost bool
	var awaitingLaunchChoice bool
	var hostSince time.Time
	var seenEvent string
	var cheered Cheer
	var narrationContext string
	var pendingResponse map[string]any
	var pendingResponseToken, pendingInstructions string
	var pendingResponseSince time.Time
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
		if e := sendBrowser(map[string]any{"type": "host_cue", "text": script}); e != nil {
			return e
		}
		pendingHost = true
		hostSince = time.Now()
		r.VoiceState("speaking")
		return sendUp(map[string]any{"type": "response.create", "response": map[string]any{
			"tool_choice":  "none",
			"instructions": readFactsInstruction(script, companion),
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
		if e := speakHost(companion.Opening(state)); e != nil {
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
		case preference := <-preferencesChanged:
			companion = preference
			cancelHost()
			if state, _, ok := roomGame(r); ok {
				seenEvent = lastEventID(state)
			}
			if !inFlight {
				idle()
			}
			if e := sendBrowser(map[string]any{"type": "companion_updated", "style": companion.Style, "frequency": companion.Frequency}); e != nil {
				return e
			}
			continue
		case control := <-controls:
			trace(room.DiagnosticEvent{Kind: "connection", Action: "browser_" + control})
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
			if pendingResponse != nil && now.Sub(pendingResponseSince) > 8*time.Second {
				pendingResponse = nil
				if inFlight && currentToken == pendingResponseToken {
					currentToken, inFlight = "", false
					idle()
					trace(room.DiagnosticEvent{Kind: "error", Code: "session_ack_timeout"})
					if e := sendBrowser(map[string]any{"type": "error", "message": "操作结果已保留，语音配置暂未确认，请重新连接"}); e != nil {
						return e
					}
				}
			}
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
					script, mark := companion.Next(state, seenEvent)
					if script == "" {
						seenEvent = mark
					}
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
			// Only a real turn interrupts the host. Table noise while waiting for the wake word lets the line finish.
			if hostActive() && gate.awake(time.Now()) {
				_ = sendUp(map[string]any{"type": "response.cancel"})
				pendingHost = false
				hostIDs = map[string]bool{}
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
			currentInput = body.ItemID
			inputTimes[body.ItemID] = time.Now()
			trace(room.DiagnosticEvent{Kind: "user_text", InputID: body.ItemID, Ref: body.ItemID, Text: body.Transcript})
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
			if hostActive() {
				_ = sendUp(map[string]any{"type": "response.cancel"})
				pendingHost = false
				hostIDs = map[string]bool{}
				if e := sendBrowser(map[string]any{"type": "host_handoff"}); e != nil {
					return e
				}
			}
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
			allowModelTools = true
			// A previous read-aloud script must never become the next turn's facts.
			if narrationContext != "" {
				if e := sendUp(map[string]any{"type": "conversation.item.delete", "item_id": narrationContext}); e != nil {
					return errors.New("游戏播报上下文没有更新")
				}
				narrationContext = ""
			}
			sessionInstructions := realtimeSessionUpdate(b.Config.Model)
			if spectator {
				sessionInstructions = realtimeSpectatorSessionUpdate(b.Config.Model)
			}
			session := sessionInstructions["session"].(map[string]any)
			session["instructions"] = session["instructions"].(string) + companion.Instructions()
			voiceText := gate.commandText(body.Transcript)
			action, handled := planVoiceAction(r, voiceText)
			if awaitingLaunchChoice {
				if choice, ok := pendingLaunchChoice(r, voiceText); ok {
					action, handled = choice, true
				} else {
					awaitingLaunchChoice = false
				}
			}
			if action.Pending {
				awaitingLaunchChoice = true
			} else if action.Name == "use_prop" && action.PropID == "launch" {
				awaitingLaunchChoice = false
			}
			if spectator && handled && (action.Name != "get_game_state" || strings.Contains(action.Notice, "掷骰") || strings.Contains(action.Notice, "飞机") || strings.Contains(action.Notice, "操作")) {
				action = voiceAction{Name: "get_game_state", Notice: "这是只属于你的观赛助手。我不能替选手掷骰、走棋、用卡或控制房间，也不会把这句话转成比赛指令。你可以继续问现在轮到谁、谁更接近终点，或刚才发生了什么。"}
				awaitingLaunchChoice = false
			}
			if handled {
				allowModelTools = false
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
				} else if action.Name == "coach_move" {
					args, _ = json.Marshal(map[string]string{"choice": action.Choice})
				} else if action.Name == "confirm_coach" {
					args, _ = json.Marshal(map[string]string{"coach_id": action.CoachID})
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
				code := "read_only"
				if action.Notice == "" {
					code = "completed"
				}
				if strings.Contains(output, `"error"`) {
					code = "rejected"
				}
				trace(room.DiagnosticEvent{Kind: "tool", InputID: body.ItemID, Ref: body.ItemID, Action: action.Name, Code: code})
				if action.Name == "coach_move" && !strings.Contains(output, `"error"`) {
					if action.Choice == "cancel" {
						action.Notice = "建议已取消，没有移动飞机或掷骰。"
					} else {
						action.Notice = coachCue(r.Snapshot())
					}
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
				spokenCheer := ""
				instructions := "只根据本轮工具结果用一两句日常中文回答，语气像主持人，优先说实际效果或一条具体建议；除非玩家问库存，不要逐项报剩余道具。结果含error或notice时没有执行操作，按真实棋局回答问题、解释原因或追问选择，不能宣称已操作成功。需要掷骰时请玩家说‘掷骰子’，点数由游戏服务随机生成，不能询问、接受或编造玩家自报点数。本轮不要再调用工具。玩家称呼只取工具中的昵称，唤醒词是助手的名字，不是玩家姓名。不要朗读内部ID和技术字段，不要提授权或操作次数用完；幸运骰使用时已实际掷两次，不能要求玩家为了让幸运骰生效再掷一次；其他道具使用后玩家在下一句话掷骰。"
				if action.Name == "coach_move" {
					instructions += "本轮是教练建议，还没有走棋。只能说相对稳妥或有被撞的可能，必须说明后续走棋或道具会改变局面。禁止称没有风险、不会被撞、很安全或保证安全。简短说明推荐的飞机和真实效果，再邀请说就它确认，不要逐项念操作菜单。"
				}
				if action.Notice != "" {
					instructions += "本轮具体要求（必须遵守）：“" + action.Notice + "”。"
				}
				if state, _, ok := roomGame(r); ok {
					seenEvent = lastEventID(state)
					if action.Notice == "" {
						instructions += "先说这次真实发生了什么，再说：" + hostCue(state) + "。"
					}
					if note := cheered.take(state); note != "" {
						spokenCheer = note
					}
					switch state.Phase {
					case "move":
						instructions += "当前已经掷骰，正在等待选择飞机。禁止要求再次掷骰，必须根据合法候选询问选择。"
					case "roll":
						instructions += "当前正在等待掷骰。不能邀请玩家直接移动飞机。"
					}
					instructions += narrationFacts(state, before)
				}
				script := actionNarration(r, action, before, output, companion) + spokenCheer
				instructions = readFactsInstruction(script, companion)
				// This acknowledgement is independent of model speech and the game WS.
				// The browser presents the committed events, never a generated dice value.
				after := r.Snapshot()
				if e := sendBrowser(map[string]any{"type": "game_action", "input_id": body.ItemID, "action": action.Name, "text": script, "room": after}); e != nil {
					return e
				}
				// Keep the current facts in conversation context as well as response
				// instructions; the provider can otherwise repeat an earlier host cue.
				narrationContext = game.ID()
				if e := sendUp(map[string]any{"type": "conversation.item.create", "item": map[string]any{
					"id": narrationContext, "type": "message", "role": "system", "content": []any{map[string]any{"type": "input_text", "text": instructions}},
				}}); e != nil {
					return errors.New("游戏播报上下文没有送回灵矽")
				}
				// Also replace session-level instructions: the provider can retain a
				// prior host-only instruction despite a newer response override.
				sessionInstructions["session"].(map[string]any)["instructions"] = instructions
				response["response"] = map[string]any{"tool_choice": "none", "instructions": instructions}
			}
			if spectator && !handled {
				fresh := b.executeTool(r, functionCall{Type: "function_call", Name: "get_game_state", CallID: "watch-" + game.ID(), Arguments: "{}"})
				session := sessionInstructions["session"].(map[string]any)
				session["instructions"] = session["instructions"].(string) + "本轮已刷新的权威棋局，回答必须以此为准。recent_events 没有记录的过去事件要明确说明不知道，禁止编造点数、胜负，禁止声称已替选手操作：" + fresh
			}
			if e := sendUp(sessionInstructions); e != nil {
				return e
			}
			// Session updates may be applied asynchronously by the provider.
			// Generate only after the matching instructions have been acknowledged.
			pendingResponse, pendingResponseToken = response, currentToken
			pendingResponseSince = time.Now()
			pendingInstructions = sessionInstructions["session"].(map[string]any)["instructions"].(string)
			continue
		case "session.updated":
			if pendingResponse == nil || !inFlight || pendingResponseToken != currentToken {
				continue
			}
			var updated struct {
				Session struct {
					Instructions string `json:"instructions"`
				} `json:"session"`
			}
			if json.Unmarshal(event.Raw, &updated) != nil || updated.Session.Instructions != pendingInstructions {
				continue
			}
			response := pendingResponse
			pendingResponse = nil
			if e := sendUp(response); e != nil {
				return e
			}
			continue
		case "response.created":
			if inFlight {
				responseInputs[event.ResponseID] = currentInput
			}
			if pendingHost && event.ResponseID != "" {
				hostIDs[event.ResponseID] = true
				pendingHost = false
			}
			if inFlight {
				grants[event.ResponseID] = currentToken
			}
		case "response.output_item.done":
			if !inFlight || !allowModelTools || grants[event.ResponseID] != currentToken {
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
			entry := responseDiagnostic(event)
			entry.InputID = responseInputs[event.ResponseID]
			trace(entry)
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
					code := "completed"
					if strings.Contains(output, `"error"`) {
						code = "rejected"
					}
					trace(room.DiagnosticEvent{Kind: "tool", InputID: responseInputs[event.ResponseID], Ref: call.CallID, Action: call.Name, Code: code})
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
			trace(room.DiagnosticEvent{Kind: "error", Code: "upstream_error"})
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
			if !heardAudio[event.ResponseID] {
				heardAudio[event.ResponseID] = true
				input := responseInputs[event.ResponseID]
				elapsed := int64(0)
				if start, ok := inputTimes[input]; ok {
					elapsed = time.Since(start).Milliseconds()
				}
				trace(room.DiagnosticEvent{Kind: "first_audio", Ref: event.ResponseID, InputID: input, DurationMS: elapsed, Code: "upstream_received"})
			}
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
		"instructions": "你是一起飞家庭飞行棋的语音主持人。用简短亲切的中文交流，只用玩家和棋子昵称，不朗读内部ID和技术字段。玩家只是呼唤或问怎么办时，根据工具结果说当前一个最有用的选择，不操作棋局。撞某人、走最前面、回家、我要几点，都由游戏服务判断；你只播报工具结果。棋局状态、道具库存和规则以工具结果为准；写操作前先查询状态，只按明确指令操作。合法起飞或移动必须调用对应工具执行，再播报真实结果；多个候选先追问。普通起飞条件读取本局rules：亲子轻松五点或六点起飞，经典/自定义六点起飞；只有六点可再掷。起飞卡需要明确选择一架自己仍在机库的飞机，使用后直接移到起飞位，不用掷骰；只说使用起飞卡而未说飞机名字时，先询问选哪架，不能自行猜测。加速卡对已离开机库的飞机增加两步，幸运骰由游戏服务掷两次取较大值，不能接受玩家自报点数。炸弹可把指定对手公共航线上的飞机送回机场，护盾可保护自己公共航线上的飞机一次。回旋镖让对手公共航线飞机后退3格（最低第1格，不触发跳跃或撞机），护盾可抵挡；救援卡把自己公共航线飞机撤回起飞位并解除护盾；团队护航保护自己全部未加盾公共航线飞机。起飞卡需要自己机库飞机ID，炸弹、护盾、回旋镖和救援卡需prop_targets中的合法目标；不能猜测目标。用户问战术或推荐时，先查状态，根据库存、prop_targets、合法走法给一句具体建议，不擅自操作。其他道具必须在掷骰前使用，一次掷骰只能用一张。一次话语最多执行一个写动作，使用幸运骰会立即真实掷两次取较大值，其余道具按真实规则执行。只有本次新增rolled事件才能播报刚掷出了点数，state.dice可能来自上一回合，不能当成本次投掷。不得声称未执行的动作成功。",
		"audio": map[string]any{
			"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 16000}, "transcription": map[string]any{},
				"turn_detection": map[string]any{"type": "server_vad", "create_response": false, "interrupt_response": true}},
			"output": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}},
		},
		"tools": realtimeTools(), "tool_choice": "auto",
	}}
}

func realtimeSpectatorSessionUpdate(model string) map[string]any {
	return map[string]any{"type": "session.update", "session": map[string]any{
		"type": "realtime", "model": model, "output_modalities": []string{"audio"},
		"instructions": "你是一位观众的私人飞行棋讲解员，只和这一位观众对话。用简短中文回答谁领先、轮到谁、刚才发生了什么、规则和道具。每次回答棋局问题都以本轮刷新的工具结果为准，不凭旧对话记忆编造点数或胜负；没有记录的过去事件要直接说明不知道。你不能掷骰、走棋、使用道具、暂停、结束或把观众的话转成选手指令。回复只给这位观众，不提及其他人的私人对话。不要朗读内部ID、授权或技术字段。",
		"audio": map[string]any{
			"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 16000}, "transcription": map[string]any{},
				"turn_detection": map[string]any{"type": "server_vad", "create_response": false, "interrupt_response": true}},
			"output": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}},
		},
		"tools": []map[string]any{realtimeTools()[0]}, "tool_choice": "auto",
	}}
}

func realtimeTools() []map[string]any {
	return []map[string]any{
		{"type": "function", "name": "get_game_state", "description": "查询当前棋局、当前玩家、棋子位置、骰子和合法走法。执行任何棋局操作前先调用。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{"type": "function", "name": "roll_dice", "description": "按当前真人玩家的明确指令掷一次骰子。不能指定骰子点数。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{"type": "function", "name": "coach_move", "description": "只提供当前合法走法的建议和棋盘落点预览，不移动飞机。best 推荐，next 另一架，safe 风险较少，previous 刚才那个，cancel 取消。仅在玩家询问或要求比较时调用，等待下一句话明确确认。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"choice": map[string]any{"type": "string", "enum": []string{"best", "next", "safe", "previous", "cancel"}}}, "required": []string{"choice"}, "additionalProperties": false}},
		{"type": "function", "name": "confirm_coach", "description": "仅当玩家本句话明确说就它、确认飞行等，才执行当前建议。建议ID来自当前room.coach.id，不能确认已变化或被取消的建议；询问或比较不执行。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"coach_id": map[string]any{"type": "string"}}, "required": []string{"coach_id"}, "additionalProperties": false}},
		{"type": "function", "name": "move_piece", "description": "移动玩家明确指定的一架合法飞机。piece_id 必须来自刚查询的棋局状态。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"piece_id": map[string]any{"type": "string"}}, "required": []string{"piece_id"}, "additionalProperties": false}},
		{"type": "function", "name": "use_prop", "description": "按玩家明确指令使用一张库存道具。launch 起飞卡需要选择自己一架仍在机库的飞机，并立即从机库移动到起飞位，不掷骰；piece_id 必须来自当前状态的prop_targets。boost 加速卡、luck 幸运骰、bomb 炸弹、shield 护盾、boomerang 回旋镖（对手后退3格）、rescue 救援卡（自己的飞机撤回起飞位）、escort 团队护航（全部自己的公共航线飞机加盾）。炸弹、护盾、回旋镖和救援卡需在prop_targets中明确选择piece_id；其他道具不填写piece_id。幸运骰使用后立即真实掷两次取较大值，其余道具按实际规则处理。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"prop_id": map[string]any{"type": "string", "enum": []string{"launch", "boost", "luck", "bomb", "shield", "boomerang", "rescue", "escort"}}, "piece_id": map[string]any{"type": "string", "description": "起飞卡必须选择自己机库飞机；炸弹、护盾、回旋镖或救援卡目标必须来自prop_targets"}}, "required": []string{"prop_id"}, "additionalProperties": false}},
	}
}

func roomGame(r room.GameSession) (game.State, string, bool) {
	view := r.Snapshot()
	var state game.State
	if json.Unmarshal(view.State, &state) != nil {
		return game.State{}, view.Status, false
	}
	return state, view.Status, true
}

func (b *Bridge) executeTool(r room.GameSession, call functionCall) string {
	args := map[string]string{}
	if call.Arguments != "" && json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return `{"error":"工具参数无效"}`
	}
	if call.Name == "roll_dice" || call.Name == "move_piece" || call.Name == "use_prop" || call.Name == "coach_move" || call.Name == "confirm_coach" {
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
