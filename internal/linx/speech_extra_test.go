package linx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
)

func testSpeaker(url string) *Speaker {
	return &Speaker{Config: Config{URL: url, APIKey: "test-only", Model: "test"}}
}

// fakeSpeakerProvider answers one Speaker.Say call. behavior receives every
// message the speaker sends and decides which provider events to write back.
func fakeSpeakerProvider(t *testing.T, behavior func(ctx context.Context, c *websocket.Conn, event map[string]any)) (string, *atomic.Int32) {
	return fakeSpeakerProviderWithPrelude(t, nil, behavior)
}

// fakeSpeakerProviderWithPrelude also allows a test to send frames before the
// provider announces the created session, which is otherwise the first event.
func fakeSpeakerProviderWithPrelude(t *testing.T, prelude, behavior func(ctx context.Context, c *websocket.Conn, event map[string]any)) (string, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	var connections atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		if prelude != nil {
			prelude(ctx, c, nil)
		}
		if writeEvent(ctx, c, map[string]any{"type": "session.created"}) != nil {
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			behavior(ctx, c, event)
		}
	}))
	t.Cleanup(provider.Close)
	original := http.DefaultClient
	http.DefaultClient = provider.Client()
	t.Cleanup(func() { http.DefaultClient = original })
	return strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", &connections
}

// completeSynthesis is the happy provider flow used as a base by variant tests.
func completeSynthesis(audio []byte, variants ...func(ctx context.Context, c *websocket.Conn, event map[string]any)) func(context.Context, *websocket.Conn, map[string]any) {
	return func(ctx context.Context, c *websocket.Conn, event map[string]any) {
		for _, variant := range variants {
			variant(ctx, c, event)
		}
		switch event["type"] {
		case "session.update":
			_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
		case "response.create":
			_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString(audio)})
			_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
		}
	}
}

func TestSpeakerRejectsMisconfigurationAndTransportFailures(t *testing.T) {
	ctx := context.Background()
	if _, err := (&Speaker{}).Say(ctx, "轮到小朋友掷骰子啦！"); err == nil {
		t.Fatal("unconfigured speaker spoke")
	}
	valid := testSpeaker("wss://127.0.0.1:1/v1/realtime")
	if _, err := valid.Say(ctx, "   "); err == nil {
		t.Fatal("empty text synthesised")
	}
	if _, err := valid.Say(ctx, "你好"); err == nil || !strings.Contains(err.Error(), "连接") {
		t.Fatalf("unreachable provider: %v", err)
	}
	badURL := testSpeaker("https://provider.example.test/v1/realtime")
	if _, err := badURL.Say(ctx, "轮到小朋友掷骰子啦！"); err == nil || !strings.Contains(err.Error(), "配置无效") {
		t.Fatalf("invalid endpoint: %v", err)
	}
	// Quota errors are surfaced to the caller instead of opening a session.
	url, connections := fakeSpeakerProvider(t, completeSynthesis([]byte{1, 0}))
	quota := testSpeaker(url)
	quota.Reserve = func(context.Context) error { return ErrSpeechBusy }
	if _, err := quota.Say(ctx, "轮到小朋友掷骰子啦！"); !errors.Is(err, ErrSpeechBusy) {
		t.Fatalf("reserve error: %v", err)
	}
	if connections.Load() != 0 {
		t.Fatal("reservation failure still opened a provider session")
	}
	// A provider that hangs up before audio fails the call.
	hangup, _ := fakeSpeakerProvider(t, func(ctx context.Context, c *websocket.Conn, event map[string]any) {
		if event["type"] == "session.update" {
			_ = c.Close(websocket.StatusNormalClosure, "bye")
		}
	})
	if _, err := testSpeaker(hangup).Say(ctx, "轮到小朋友掷骰子啦！"); err == nil || !strings.Contains(err.Error(), "没有完成") {
		t.Fatalf("provider hangup: %v", err)
	}
}

func TestSpeakerCacheHitAndConcurrentWaiters(t *testing.T) {
	ctx := context.Background()
	s := testSpeaker("wss://provider.example.test/v1/realtime")
	s.cache = map[string][]byte{"轮到小朋友掷骰子啦！": {1, 0}}
	audio, err := s.Say(ctx, "轮到小朋友掷骰子啦！")
	if err != nil || len(audio) != 2 {
		t.Fatalf("cache hit: %v %v", err, audio)
	}

	// A waiter with an already-cancelled context leaves without calling the provider.
	s.mu.Lock()
	s.busy, s.pendingText, s.pendingDone = true, "同一句", make(chan struct{})
	s.mu.Unlock()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Say(cancelled, "同一句"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}

	// A waiter whose synthesis failed gets the busy error instead of empty audio.
	s.mu.Lock()
	done := make(chan struct{})
	close(done)
	s.busy, s.pendingText, s.pendingDone = true, "同一句", done
	s.mu.Unlock()
	if _, err = s.Say(ctx, "同一句"); !errors.Is(err, ErrSpeechBusy) {
		t.Fatalf("waiter after failed synthesis: %v", err)
	}

	// A different line never joins another synthesis.
	s.mu.Lock()
	s.busy, s.pendingText, s.pendingDone = true, "别的句子", make(chan struct{})
	s.mu.Unlock()
	if _, err = s.Say(ctx, "第三句"); !errors.Is(err, ErrSpeechBusy) {
		t.Fatalf("different line: %v", err)
	}
	if _, err = s.Say(ctx, "轮到小朋友掷骰子啦！"); err != nil {
		t.Fatalf("cached line should bypass the busy flag: %v", err)
	}
	s.mu.Lock()
	s.busy, s.pendingText = false, ""
	s.mu.Unlock()
}

func TestSpeakerHandlesMalformedProviderEvents(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		prelude  func(ctx context.Context, c *websocket.Conn, event map[string]any)
		behavior func(ctx context.Context, c *websocket.Conn, event map[string]any)
		wantErr  string
	}{
		"invalid frame ignored": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = c.Write(ctx, websocket.MessageText, []byte("not-json"))
					time.Sleep(10 * time.Millisecond)
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{1, 0})})
					_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				}
			},
		},
		"update before created": {
			prelude: func(ctx context.Context, c *websocket.Conn, _ map[string]any) {
				_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
			},
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{1, 0})})
					_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				}
			},
		},
		"audio before request": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{1, 0})})
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				}
			},
			wantErr: "没有返回完整音频",
		},
		"invalid audio": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": "%%%not-base64%%%"})
				}
			},
			wantErr: "音频无效",
		},
		"odd audio": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{1})})
					_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				}
			},
			wantErr: "没有返回完整音频",
		},
		"incomplete response": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				switch event["type"] {
				case "session.update":
					_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
				case "response.create":
					_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "cancelled"}})
				}
			},
			wantErr: "没有返回完整音频",
		},
		"provider error": {
			behavior: func(ctx context.Context, c *websocket.Conn, event map[string]any) {
				_ = writeEvent(ctx, c, map[string]any{"type": "error", "error": map[string]any{"message": "rejected"}})
			},
			wantErr: "暂时不可用",
		},
	} {
		t.Run(name, func(t *testing.T) {
			url, _ := fakeSpeakerProviderWithPrelude(t, tc.prelude, tc.behavior)
			audio, err := testSpeaker(url).Say(ctx, "轮到小朋友掷骰子啦！")
			if tc.wantErr == "" {
				if err != nil || len(audio) != 2 {
					t.Fatalf("expected success: %v %v", err, audio)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSpeakerEvictsTheOldestCachedLine(t *testing.T) {
	url, connections := fakeSpeakerProvider(t, completeSynthesis([]byte{3, 0}))
	s := testSpeaker(url)
	s.cache = map[string][]byte{}
	for i := 0; i < 32; i++ {
		line := "旧句子" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		s.cache[line] = []byte{1, 0}
		s.order = append(s.order, line)
	}
	audio, err := s.Say(context.Background(), "轮到小朋友掷骰子啦！")
	if err != nil || len(audio) != 2 {
		t.Fatalf("synthesis: %v %v", err, audio)
	}
	if len(s.order) != 32 || s.order[len(s.order)-1] != "轮到小朋友掷骰子啦！" {
		t.Fatalf("cache order: %d", len(s.order))
	}
	if _, ok := s.cache["旧句子A0"]; ok {
		t.Fatal("oldest cache entry was not evicted")
	}
	if connections.Load() != 1 {
		t.Fatalf("provider sessions: %d", connections.Load())
	}
}

func TestTurnAnnouncementHandlesEmptyAndUnresolvedStates(t *testing.T) {
	if key, line := TurnAnnouncement(game.State{}); key != "" || line != "" {
		t.Fatalf("empty players: %q %q", key, line)
	}
	state := game.New(game.Options{Names: []string{"甲", "乙"}})
	state.Phase = "finished"
	state.Winner = "nobody"
	if key, line := TurnAnnouncement(*state); key != "" || line != "" {
		t.Fatalf("finished without a matching winner: %q %q", key, line)
	}
	state.Phase = "move"
	if key, _ := TurnAnnouncement(*state); key != "" {
		t.Fatal("move phase announced a roll turn")
	}
}
