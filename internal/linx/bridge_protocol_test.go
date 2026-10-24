package linx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestGameFactQuestionSelectsTheRelevantReadonlyTool(t *testing.T) {
	for _, tt := range []struct {
		text, want string
	}{
		{"请查一下本局星际救援任务卡的目标和当前进展", "get_mission_progress"},
		{"我们还差什么", "get_mission_progress"},
		{"这个主题是什么", "get_game_summary"},
		{"现在轮到谁掷骰子", "get_game_summary"},
		{"现在有哪些飞机可以走", "get_legal_actions"},
		{"起飞卡是什么", "get_game_rules"},
		{"为什么这步走不了", "get_legal_actions"},
		{"帮我使用起飞卡", ""},
		{"好，掷骰子", ""},
		{"随便聊聊天", ""},
	} {
		if got := gameFactTool(tt.text); got != tt.want {
			t.Errorf("gameFactTool(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// protocolHarness crosses both actual WebSocket boundaries. All handlers are
// joined before restoring the TLS client, including hijacked HTTP connections.
type protocolHarness struct {
	ctx               context.Context
	browser, provider *websocket.Conn
	requests          chan map[string]any
	finished          chan error
	bridge            *Bridge
	mu                sync.Mutex
	traces            []room.DiagnosticEvent
	holdAck           atomic.Bool
}

func newProtocolHarness(t *testing.T, r *room.Room, wake bool, frequency string, mcpPublicURL ...string) *protocolHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	h := &protocolHarness{ctx: ctx, requests: make(chan map[string]any, 128), finished: make(chan error, 1)}
	withMCP := len(mcpPublicURL) > 0 && mcpPublicURL[0] != ""
	if _, err := r.SetVoiceSettings(room.VoiceSettings{WakeEnabled: wake, WakeWord: "小灵同学"}); err != nil {
		t.Fatal(err)
	}
	peers := make(chan *websocket.Conn, 1)
	var workers sync.WaitGroup
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		workers.Add(1)
		defer workers.Done()
		c, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		peers <- c
		// Noise before both handshake acknowledgements must not enable audio early.
		_ = c.Write(ctx, websocket.MessageBinary, []byte{1, 2})
		_ = c.Write(ctx, websocket.MessageText, []byte("invalid-json"))
		_ = writeEvent(ctx, c, map[string]any{"type": "session.created"})
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			if event["type"] == "session.update" && !h.holdAck.Load() {
				_ = c.Write(ctx, websocket.MessageBinary, []byte{1, 2})
				_ = c.Write(ctx, websocket.MessageText, []byte("invalid-json"))
				if writeEvent(ctx, c, map[string]any{"type": "session.updated", "session": event["session"]}) != nil {
					return
				}
				if withMCP {
					if writeEvent(ctx, c, map[string]any{"type": "mcp_list_tools.completed"}) != nil {
						return
					}
				}
			}
			select {
			case h.requests <- event:
			case <-ctx.Done():
				return
			}
		}
	}))
	old := http.DefaultClient
	http.DefaultClient = provider.Client()
	h.bridge = &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https:", "wss:", 1) + "/v1/realtime", APIKey: "test", Model: "test"}, Observe: func(e room.DiagnosticEvent) { h.mu.Lock(); defer h.mu.Unlock(); h.traces = append(h.traces, e) }}
	if withMCP {
		h.bridge.MCP = NewReadonlyMCP(mcpPublicURL[0])
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		workers.Add(1)
		defer workers.Done()
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		h.finished <- h.bridge.Serve(ctx, c, r, CompanionPreferences{Style: "warm", Frequency: frequency})
	}))
	t.Cleanup(func() {
		cancel()
		if h.browser != nil {
			h.browser.CloseNow()
		}
		if h.provider != nil {
			h.provider.CloseNow()
		}
		gateway.Close()
		provider.Close()
		workers.Wait()
		http.DefaultClient = old
	})
	c, _, err := websocket.Dial(ctx, strings.Replace(gateway.URL, "http:", "ws:", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.browser = c
	select {
	case h.provider = <-peers:
	case <-ctx.Done():
		t.Fatal("provider connection timeout")
	}
	readUntil(t, ctx, c, "hello")
	readUntil(t, ctx, c, "wake_state")
	return h
}
func (h *protocolHarness) send(t *testing.T, event map[string]any) {
	t.Helper()
	if err := writeEvent(h.ctx, h.provider, event); err != nil {
		t.Fatal(err)
	}
}
func (h *protocolHarness) control(t *testing.T, event map[string]any) {
	t.Helper()
	if err := writeEvent(h.ctx, h.browser, event); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeProtocolWakeSleepAudioAndRecovery(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	h := newProtocolHarness(t, r, true, "quiet")
	pcm := []byte{1, 0, 2, 0}
	if err := h.browser.Write(h.ctx, websocket.MessageBinary, pcm); err != nil {
		t.Fatal(err)
	}
	appendEvent := spectatorRequest(t, h.ctx, h.requests, "input_audio_buffer.append")
	if appendEvent["audio"] != base64.StdEncoding.EncodeToString(pcm) {
		t.Fatalf("PCM changed: %v", appendEvent)
	}
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "noise", "transcript": "窗外有人说话"})
	deleted := spectatorRequest(t, h.ctx, h.requests, "conversation.item.delete")
	if deleted["item_id"] != "noise" {
		t.Fatalf("background speech retained: %v", deleted)
	}
	if event := readUntil(t, h.ctx, h.browser, "wake_state"); event["awake"] != false {
		t.Fatal("background speech woke assistant")
	}
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "awake", "transcript": "小灵同学讲个故事"})
	readUntil(t, h.ctx, h.browser, "stt")
	spectatorRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "answer"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.audio.delta", "response_id": "answer", "delta": "AQACAA=="})
	readUntil(t, h.ctx, h.browser, "response.audio.delta")
	if r.Snapshot().Voice != "speaking" {
		t.Fatal("audio did not set speaking state")
	}
	h.control(t, map[string]any{"type": "sleep"})
	spectatorRequest(t, h.ctx, h.requests, "response.cancel")
	if event := readUntil(t, h.ctx, h.browser, "wake_state"); event["awake"] != false {
		t.Fatal("sleep did not close wake window")
	}
	if r.CurrentActionToken() != "" {
		t.Fatal("sleep retained game grant")
	}
	h.control(t, map[string]any{"type": "companion_preferences", "style": "concise", "frequency": "quiet"})
	updated := readUntil(t, h.ctx, h.browser, "companion_updated")
	if updated["style"] != "concise" {
		t.Fatal(updated)
	}
	// A server-side wake setting change is picked up by the periodic check.
	if _, err := r.SetVoiceSettings(room.VoiceSettings{WakeWord: "新助手", WakeEnabled: false}); err != nil {
		t.Fatal(err)
	}
	event := readUntil(t, h.ctx, h.browser, "wake_state")
	if event["wake_word"] != "新助手" || event["wake_enabled"] != false {
		t.Fatalf("settings not refreshed: %v", event)
	}
	h.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
	readUntil(t, h.ctx, h.browser, "speech_started")
	h.send(t, map[string]any{"type": "input_audio_buffer.speech_stopped"})
	readUntil(t, h.ctx, h.browser, "speech_stopped")
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "recover", "transcript": "讲个故事"})
	readUntil(t, h.ctx, h.browser, "stt")
	spectatorRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "error"})
	if e := readUntil(t, h.ctx, h.browser, "error"); !strings.Contains(e["message"].(string), "重新说一次") {
		t.Fatal(e)
	}
	if r.CurrentActionToken() != "" {
		t.Fatal("provider error retained action grant")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	foundAudio, foundError := false, false
	for _, e := range h.traces {
		if e.SessionID == "" || e.GameID == "" || e.RoomID == "" {
			t.Fatalf("uncorrelated diagnostic: %+v", e)
		}
		foundAudio = foundAudio || e.Kind == "first_audio"
		foundError = foundError || e.Code == "upstream_error"
	}
	if !foundAudio || !foundError {
		t.Fatalf("missing audio/error diagnostics: %+v", h.traces)
	}
}

func TestBridgeProtocolRejectsInvalidBrowserFrames(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind websocket.MessageType
		data []byte
	}{
		{"oversized PCM", websocket.MessageBinary, make([]byte, 6401)},
		{"malformed control", websocket.MessageText, []byte("not-json")},
		{"invalid preferences", websocket.MessageText, []byte(`{"type":"companion_preferences","style":"unknown"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := room.New(game.Options{Names: []string{"甲", "乙"}})
			h := newProtocolHarness(t, r, false, "quiet")
			if err := h.browser.Write(h.ctx, tc.kind, tc.data); err != nil {
				t.Fatal(err)
			}
			select {
			case <-h.finished:
			case <-h.ctx.Done():
				t.Fatal("invalid frame did not terminate bridge")
			}
			if h.bridge.active != 0 || r.Snapshot().Voice != "disconnected" {
				t.Fatal("invalid frame leaked slot or voice state")
			}
		})
	}
}

func TestBridgeCompletesGameFunctionWhenNativeMCPCallFails(t *testing.T) {
	r := room.New(game.Options{Names: []string{"孩子", "家长"}, Demo: true})
	h := newProtocolHarness(t, r, false, "quiet", "https://flytogether.example")
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "mixed-tools", "transcript": "给我讲讲现在的星际救援任务"})
	readUntil(t, h.ctx, h.browser, "stt")
	update := spectatorRequest(t, h.ctx, h.requests, "session.update")
	encoded, _ := json.Marshal(update["session"])
	if !strings.Contains(string(encoded), `"type":"mcp"`) {
		t.Fatalf("Realtime session did not import the native MCP tool: %s", encoded)
	}
	waitRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mixed-response"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.output_item.done", "response_id": "mixed-response", "item": map[string]any{
		"type": "function_call", "name": "roll_dice", "call_id": "roll-from-mixed-response", "arguments": `{}`,
	}})
	h.send(t, map[string]any{"type": "response.mcp_call.in_progress", "response_id": "mixed-response", "item_id": "mission-query"})
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mixed-response", "status": "completed"}})

	var output map[string]any
	for output == nil {
		select {
		case request := <-h.requests:
			if request["type"] != "conversation.item.create" {
				continue
			}
			item, _ := request["item"].(map[string]any)
			if item["type"] == "function_call_output" && item["call_id"] == "roll-from-mixed-response" {
				output = item
			}
		case <-h.ctx.Done():
			t.Fatal("the game function result was lost when the Realtime response also invoked MCP")
		}
	}
	var rolled game.State
	if err := json.Unmarshal(r.Snapshot().State, &rolled); err != nil {
		t.Fatal(err)
	}
	if rolled.Version != 2 || rolled.Phase != "move" || rolled.Players[0].Stats.Rolls != 1 {
		t.Fatalf("mixed-response game function was not executed exactly once: %+v", rolled)
	}
	select {
	case request := <-h.requests:
		if request["type"] == "response.create" {
			t.Fatal("the model continued before the pending MCP call completed")
		}
	case <-time.After(100 * time.Millisecond):
	}

	h.send(t, map[string]any{"type": "response.mcp_call.failed", "response_id": "mixed-response", "item_id": "mission-query"})
	continuation := spectatorRequest(t, h.ctx, h.requests, "response.create")
	response := continuation["response"].(map[string]any)
	if response["tool_choice"] != "none" || !strings.Contains(response["instructions"].(string), "不要猜测棋局状态") {
		t.Fatalf("MCP failure continuation may fabricate a result: %#v", response)
	}
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mixed-failure-answer"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mixed-failure-answer", "status": "completed"}})
	readUntil(t, h.ctx, h.browser, "response.done")
}

func TestBridgeContinuesAfterNativeMCPCallCompletes(t *testing.T) {
	r := room.New(game.Options{Mode: "coop", Names: []string{"孩子", "家长"}, MissionID: "launch-formation"})
	h := newProtocolHarness(t, r, false, "quiet", "https://flytogether.example")
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "mission-question", "transcript": "讲个宇宙笑话"})
	readUntil(t, h.ctx, h.browser, "stt")
	spectatorRequest(t, h.ctx, h.requests, "session.update")
	waitRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mission-answer"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.mcp_call.in_progress", "response_id": "mission-answer", "item_id": "mission-progress"})
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mission-answer", "status": "completed"}})
	select {
	case request := <-h.requests:
		if request["type"] == "response.create" {
			t.Fatal("the model continued before the MCP tool returned its result")
		}
	case <-time.After(100 * time.Millisecond):
	}
	h.send(t, map[string]any{"type": "response.output_item.done", "response_id": "mission-answer", "item": map[string]any{"id": "mission-progress", "type": "mcp_call", "name": "get_mission_progress", "output": `{"status":"active","progress":0}`}})
	waitRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mission-answer-final"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mission-answer-final", "status": "completed"}})
	readUntil(t, h.ctx, h.browser, "response.done")
	if state := protocolState(t, r); state.Version != 1 || state.Mission == nil || state.Mission.Progress != 0 {
		t.Fatalf("native MCP question mutated mission/game state: %+v", state)
	}
}

func TestBridgeRoutesFactQuestionsThroughReadonlyMCP(t *testing.T) {
	r := room.New(game.Options{Mode: "coop", Names: []string{"孩子", "家长"}, MissionID: "launch-formation"})
	h := newProtocolHarness(t, r, false, "quiet", "https://flytogether.example")
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "mission-question", "transcript": "请查一下本局任务卡的目标和进度"})
	readUntil(t, h.ctx, h.browser, "stt")
	spectatorRequest(t, h.ctx, h.requests, "session.update")
	request := spectatorRequest(t, h.ctx, h.requests, "response.create")
	choice := request["response"].(map[string]any)["tool_choice"].(map[string]any)
	if choice["type"] != "mcp" || choice["server_label"] != gameMCPLabel || choice["name"] != "get_mission_progress" {
		t.Fatalf("fact question was not forced through read-only MCP: %#v", request)
	}
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mission-answer"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.mcp_call.in_progress", "response_id": "mission-answer", "item_id": "mission-progress"})
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mission-answer", "status": "completed"}})
	h.send(t, map[string]any{"type": "response.output_item.done", "item": map[string]any{"id": "mission-progress", "type": "mcp_call", "name": "get_mission_progress", "output": `{"status":"active","progress":0}`}})
	var status map[string]any
	for {
		status = readUntil(t, h.ctx, h.browser, "mcp_call_status")
		if status["status"] == "completed" {
			break
		}
	}
	continuation := spectatorRequest(t, h.ctx, h.requests, "response.create")
	if continuation["response"].(map[string]any)["tool_choice"] != "none" {
		t.Fatalf("MCP answer must not loop into another tool call: %#v", continuation)
	}
	h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": "mission-answer-final"}})
	readUntil(t, h.ctx, h.browser, "response.created")
	h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": "mission-answer-final", "status": "completed"}})
	readUntil(t, h.ctx, h.browser, "response.done")
	if state := protocolState(t, r); state.Version != 1 || state.Mission == nil || state.Mission.Progress != 0 {
		t.Fatalf("fact query changed the game state: %+v", state)
	}
}

func TestBridgeRoutesGameFactsToTheMatchingReadonlyMCPTool(t *testing.T) {
	for _, tc := range []struct {
		name, mode, question, want string
	}{
		{"turn and theme in quick game", "quick", "现在轮到谁掷骰子", "get_game_summary"},
		{"rules in classic game", "classic", "起飞卡是什么", "get_game_rules"},
		{"legal actions without rescue mission", "quick", "现在有哪些飞机可以走", "get_legal_actions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := room.New(game.Options{Mode: tc.mode, Names: []string{"孩子", "家长"}})
			h := newProtocolHarness(t, r, false, "quiet", "https://flytogether.example")
			h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "generic-fact", "transcript": tc.question})
			readUntil(t, h.ctx, h.browser, "stt")
			spectatorRequest(t, h.ctx, h.requests, "session.update")
			request := spectatorRequest(t, h.ctx, h.requests, "response.create")
			choice := request["response"].(map[string]any)["tool_choice"].(map[string]any)
			if choice["type"] != "mcp" || choice["server_label"] != gameMCPLabel || choice["name"] != tc.want {
				t.Fatalf("fact question must call %s, got %#v", tc.want, choice)
			}
			state := protocolState(t, r)
			if state.Version != 1 || state.Phase != "roll" {
				t.Fatalf("read-only fact query changed the %s game: %+v", tc.mode, state)
			}
		})
	}
}
