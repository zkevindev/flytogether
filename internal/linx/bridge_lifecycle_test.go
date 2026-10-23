package linx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

// Use actual browser/gateway/provider WebSockets, with a controlled TLS provider
// to deliver tool results after cancellation or a new match, deterministically.
func TestBridgeLateToolsStayBoundToTheirOriginalTurn(t *testing.T) {
	for _, scenario := range []string{"abort-and-next-turn", "new-match"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			requests := make(chan map[string]any, 64)
			peers := make(chan *websocket.Conn, 1)
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
				if err != nil {
					return
				}
				defer c.CloseNow()
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
					if event["type"] == "session.update" {
						if writeEvent(ctx, c, map[string]any{"type": "session.updated"}) != nil {
							return
						}
						peers <- c
					} else {
						select {
						case requests <- event:
						case <-ctx.Done():
							return
						}
					}
				}
			}))
			defer provider.Close()
			originalClient := http.DefaultClient
			http.DefaultClient = provider.Client()
			defer func() { http.DefaultClient = originalClient }()
			r := room.New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
			if _, err := r.SetVoiceSettings(room.VoiceSettings{WakeEnabled: false, WakeWord: "小灵同学"}); err != nil {
				t.Fatal(err)
			}
			bridge := &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", APIKey: "test-only", Model: "test"}}
			finished := make(chan struct{})
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, err := websocket.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				defer close(finished)
				_ = bridge.Serve(req.Context(), c, r)
			}))
			defer gateway.Close()
			browser, _, err := websocket.Dial(ctx, strings.Replace(gateway.URL, "http://", "ws://", 1), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				browser.CloseNow()
				cancel()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("gateway did not finish")
				}
			}()
			var up *websocket.Conn
			select {
			case up = <-peers:
			case <-ctx.Done():
				t.Fatal("provider handshake timed out")
			}
			readUntil(t, ctx, browser, "hello")
			waitRequest(t, ctx, requests, "response.create")
			send := func(event map[string]any) {
				t.Helper()
				if err := writeEvent(ctx, up, event); err != nil {
					t.Fatal(err)
				}
			}
			response := func(kind, id string) map[string]any {
				return map[string]any{"type": kind, "response_id": id, "response": map[string]any{"id": id, "status": "completed"}}
			}
			// Finish the initial host line before the player starts speaking.
			send(response("response.created", "host"))
			send(response("response.done", "host"))
			readUntil(t, ctx, browser, "response.done")
			send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "first-input", "transcript": "讲一个故事"})
			readUntil(t, ctx, browser, "stt")
			waitRequest(t, ctx, requests, "response.create")
			send(response("response.created", "old-response"))
			readUntil(t, ctx, browser, "response.created")
			if err := writeEvent(ctx, browser, map[string]any{"type": "abort"}); err != nil {
				t.Fatal(err)
			}
			readUntil(t, ctx, browser, "wake_state")
			waitRequest(t, ctx, requests, "response.cancel")
			var before game.State
			if err := json.Unmarshal(r.Snapshot().State, &before); err != nil {
				t.Fatal(err)
			}
			if scenario == "new-match" {
				if _, err := r.Restart(game.Options{Names: []string{"孩子", "爸爸"}, Mode: "quick"}); err != nil {
					t.Fatal(err)
				}
			}
			// A fresh ASR event creates a different action grant. In the first case it
			// commits one real random roll; in the second case it is a read-only query.
			text := "掷骰子"
			if scenario == "new-match" {
				text = "讲一个故事"
			}
			send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "next-input", "transcript": text})
			readUntil(t, ctx, browser, "stt")
			waitRequest(t, ctx, requests, "response.create")
			send(response("response.created", "new-response"))
			readUntil(t, ctx, browser, "response.created")
			send(map[string]any{"type": "response.output_item.done", "response_id": "old-response", "item": map[string]any{"type": "function_call", "name": "roll_dice", "call_id": "late-roll", "arguments": "{}"}})
			send(response("response.done", "old-response"))
			// This completion is a barrier: the preceding stale frames were processed.
			send(response("response.done", "new-response"))
			readUntil(t, ctx, browser, "response.done")
			var after game.State
			if err := json.Unmarshal(r.Snapshot().State, &after); err != nil {
				t.Fatal(err)
			}
			if scenario == "new-match" {
				if after.ID == before.ID || after.Version != 1 || after.Dice != 0 {
					t.Fatalf("late tool changed new match: %+v", after)
				}
			} else if after.ID != before.ID || after.Version != before.Version+1 {
				t.Fatalf("late tool executed on fresh turn: before %d, after %d", before.Version, after.Version)
			}
		})
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, event map[string]any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, data)
}
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, kind string) map[string]any {
	t.Helper()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		var event map[string]any
		if json.Unmarshal(data, &event) == nil && event["type"] == kind {
			return event
		}
	}
}
func waitRequest(t *testing.T, ctx context.Context, requests <-chan map[string]any, kind string) {
	t.Helper()
	for {
		select {
		case event := <-requests:
			if event["type"] == kind {
				return
			}
		case <-ctx.Done():
			t.Fatalf("waiting for provider request %s", kind)
		}
	}
}
