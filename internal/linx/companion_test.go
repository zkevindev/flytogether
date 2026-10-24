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

func TestCompanionUsesFactsAndSuppressesUnsolicitedLines(t *testing.T) {
	s := game.New(game.Options{Names: []string{"孩子", "家长"}, Props: true})
	p, _ := (CompanionPreferences{}).Normalized()
	key, line := p.Announcement(*s)
	if key == "" || line != "轮到孩子掷骰子啦！" {
		t.Fatal(line)
	}
	p.Style = "coach"
	_, line = p.Announcement(*s)
	if !strings.Contains(line, "六点") {
		t.Fatal("coach lacks actual rule tip")
	}
	p.Style = "concise"
	_, line = p.Announcement(*s)
	if line != "轮到孩子掷骰子。" {
		t.Fatal(line)
	}
	old := lastEventID(*s)
	_ = s.Roll(1)
	p.Frequency = "highlights"
	if key, _ = p.Announcement(*s); key != "" {
		t.Fatal("ordinary next turn was not suppressed")
	}
	if line, _ = p.Next(*s, old); line != "" {
		t.Fatal("ordinary realtime line not suppressed")
	}
	p.Frequency = "quiet"
	if p.Opening(*s) != "" {
		t.Fatal("quiet mode opened with unsolicited speech")
	}
	if key, _ = p.Announcement(*s); key != "" {
		t.Fatal("quiet TTS request")
	}
	if _, err := (CompanionPreferences{Style: "free prompt injection"}).Normalized(); err == nil {
		t.Fatal("invalid style accepted")
	}
}
func TestLiveCompanionChangesDoNotReconnectOrWriteGame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	requests := make(chan map[string]any, 64)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, e := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
		if e != nil {
			return
		}
		defer c.CloseNow()
		_ = writeEvent(ctx, c, map[string]any{"type": "session.created"})
		for {
			_, data, e := c.Read(ctx)
			if e != nil {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			if event["type"] == "session.update" {
				_ = writeEvent(ctx, c, map[string]any{"type": "session.updated", "session": event["session"]})
			}
			select {
			case requests <- event:
			case <-ctx.Done():
				return
			}
		}
	}))
	defer provider.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = oldClient }()
	r := room.New(game.Options{Names: []string{"孩子", "家长"}})
	initial := r.Snapshot().State
	bridge := Bridge{Config: Config{URL: strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", Model: "test", APIKey: "test"}}
	done := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, e := websocket.Accept(w, req, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		defer close(done)
		_ = bridge.Serve(req.Context(), c, r, CompanionPreferences{Style: "coach", Frequency: "quiet"})
	}))
	defer gateway.Close()
	c, _, e := websocket.Dial(ctx, strings.Replace(gateway.URL, "http://", "ws://", 1), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { c.CloseNow(); cancel(); <-done }()
	readUntil(t, ctx, c, "hello")
	time.Sleep(100 * time.Millisecond)
	for len(requests) > 0 {
		event := <-requests
		if event["type"] == "response.create" {
			t.Fatal("quiet connection spontaneously spoke")
		}
	}
	_ = writeEvent(ctx, c, map[string]any{"type": "companion_preferences", "style": "concise", "frequency": "highlights"})
	ack := readUntil(t, ctx, c, "companion_updated")
	if ack["style"] != "concise" {
		t.Fatal(ack)
	}
	if string(r.Snapshot().State) != string(initial) {
		t.Fatal("preference changed game")
	}
}

func TestCompanionAndVoiceUseGentleFiveRule(t *testing.T) {
	s := game.New(game.Options{Names: []string{"孩子", "家长"}, Preset: "gentle"})
	if !strings.Contains(companionTip(*s), "五点或六点") {
		t.Fatal("coach taught wrong preset")
	}
	_ = s.Roll(5)
	a, ok := resolveVoiceAction(*s, "让小熊号起飞")
	if !ok || a.PieceID != s.Players[0].Pieces[0].ID || a.Notice != "" {
		t.Fatal("voice five launch not legal")
	}
}
