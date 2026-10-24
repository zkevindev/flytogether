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

func newProtocolHarness(t *testing.T, r *room.Room, wake bool, frequency string) *protocolHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	h := &protocolHarness{ctx: ctx, requests: make(chan map[string]any, 128), finished: make(chan error, 1)}
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
