package linx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestBridgeHandshakeRejectsProviderErrorsAndEarlyDisconnects(t *testing.T) {
	for _, tc := range []struct {
		name                string
		afterCreated, close bool
		want                string
	}{
		{"rejected session", false, false, "拒绝"},
		{"rejected audio configuration", true, false, "没有接受"},
		{"closed before created", false, true, "已断开"},
		{"closed before configured", true, true, "初始化超时"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var workers sync.WaitGroup
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				workers.Add(1)
				defer workers.Done()
				c, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
				if err != nil {
					return
				}
				defer c.CloseNow()
				if tc.afterCreated {
					if err = writeEvent(ctx, c, map[string]any{"type": "session.created"}); err != nil {
						return
					}
					_, _, err = c.Read(ctx)
					if err != nil {
						return
					}
				}
				if tc.close {
					return
				}
				_ = writeEvent(ctx, c, map[string]any{"type": "error"})
			}))
			old := http.DefaultClient
			http.DefaultClient = provider.Client()
			b := &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https:", "wss:", 1) + "/v1/realtime", APIKey: "test", Model: "test"}}
			r := room.New(game.Options{Names: []string{"甲", "乙"}})
			done := make(chan error, 1)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				workers.Add(1)
				defer workers.Done()
				c, err := websocket.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				done <- b.Serve(ctx, c, r)
			}))
			var browser *websocket.Conn
			t.Cleanup(func() {
				cancel()
				if browser != nil {
					browser.CloseNow()
				}
				gateway.Close()
				provider.Close()
				workers.Wait()
				http.DefaultClient = old
			})
			var err error
			browser, _, err = websocket.Dial(ctx, strings.Replace(gateway.URL, "http:", "ws:", 1), nil)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("handshake failed to finish")
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			if b.active != 0 || r.Snapshot().Voice != "disconnected" {
				t.Fatal("failed handshake retained slot or voice")
			}
		})
	}
}

func TestBridgeEarlyValidationDoesNotLeakSlots(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	b := &Bridge{}
	if err := b.Serve(context.Background(), nil, r, CompanionPreferences{Style: "invalid"}); err == nil || b.active != 0 {
		t.Fatalf("preferences: %v", err)
	}
	if err := b.Serve(context.Background(), nil, r); err == nil || !strings.Contains(err.Error(), "配置无效") || b.active != 0 {
		t.Fatalf("endpoint: %v", err)
	}
	store := room.NewStore()
	watched, err := store.Create("host", game.Options{Names: []string{"甲", "乙"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Watch("fan", watched.ID, watched.SnapshotFor("host").Audience.Token, "观众"); err != nil {
		t.Fatal(err)
	}
	v, err := watched.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	b.ReserveSpectator = func(context.Context) error { return errors.New("private database detail") }
	if err = b.Serve(context.Background(), nil, v); err == nil || !strings.Contains(err.Error(), "暂时不可用") || strings.Contains(err.Error(), "database") {
		t.Fatalf("quota error disclosure: %v", err)
	}
	if b.spectators != 0 {
		t.Fatal("failed quota check retained slot")
	}
}
