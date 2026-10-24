package linx

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
)

func TestTurnAnnouncementNamesCurrentPlayerAndBonusTurn(t *testing.T) {
	state := game.New(game.Options{Names: []string{"小朋友", "爸爸"}, Mode: "quick"})
	first, line := TurnAnnouncement(*state)
	if first == "" || line != "轮到小朋友掷骰子啦！" {
		t.Fatalf("unexpected initial announcement: %s", line)
	}
	if err := state.Roll(6); err != nil {
		t.Fatal(err)
	}
	if key, _ := TurnAnnouncement(*state); key != "" {
		t.Fatal("announced dice while choosing a plane")
	}
	if err := state.Move(state.Legal[0].PieceID); err != nil {
		t.Fatal(err)
	}
	key, line := TurnAnnouncement(*state)
	if key == first || !strings.Contains(line, "小朋友") {
		t.Fatal("six bonus did not produce a distinct roll announcement")
	}
	state.Phase = "finished"
	state.Winner = state.Players[1].ID
	if key, line = TurnAnnouncement(*state); key != state.ID+":winner" || !strings.Contains(line, "爸爸") {
		t.Fatal("winner announcement used the wrong player")
	}
	coop := game.New(game.Options{Names: []string{"孩子", "爸爸"}, Mode: "coop"})
	coop.Phase, coop.Winner = "finished", "team"
	if key, line = TurnAnnouncement(*coop); key != coop.ID+":winner" || !strings.Contains(line, "星河救援成功") {
		t.Fatalf("co-op result was not narrated: %q", line)
	}
}

func TestSpeakerProducesAudioWithoutMicrophoneOrGameToolsAndCachesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var connections atomic.Int32
	expected := []byte{1, 0, 2, 0, 3, 0}
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing provider authorization")
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = writeEvent(ctx, c, map[string]any{"type": "session.created"})
		update := readUntil(t, ctx, c, "session.update")
		session := update["session"].(map[string]any)
		if tools := session["tools"].([]any); len(tools) != 0 || session["tool_choice"] != "none" {
			t.Error("speaker enabled game tools")
		}
		_ = writeEvent(ctx, c, map[string]any{"type": "session.updated"})
		response := readUntil(t, ctx, c, "response.create")["response"].(map[string]any)
		if response["tool_choice"] != "none" || !strings.Contains(response["instructions"].(string), "轮到小朋友") {
			t.Error("invalid speech-only request")
		}
		time.Sleep(100 * time.Millisecond) // allow simultaneous listeners to join the same synthesis
		_ = writeEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString(expected)})
		_ = writeEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
	}))
	defer provider.Close()
	original := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = original }()
	speaker := &Speaker{Config: Config{URL: strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", APIKey: "test-only", Model: "test"}}
	var listeners sync.WaitGroup
	for i := 0; i < 4; i++ {
		listeners.Add(1)
		go func() {
			defer listeners.Done()
			audio, err := speaker.Say(ctx, "轮到小朋友掷骰子啦！")
			if err != nil || string(audio) != string(expected) {
				t.Errorf("shared synthesis: %v", err)
			}
		}()
	}
	listeners.Wait()
	for i := 0; i < 2; i++ {
		audio, err := speaker.Say(ctx, "轮到小朋友掷骰子啦！")
		if err != nil || string(audio) != string(expected) {
			t.Fatalf("unexpected speech result: %v", err)
		}
	}
	if connections.Load() != 1 {
		t.Fatal("cached narration opened another upstream session")
	}
}
