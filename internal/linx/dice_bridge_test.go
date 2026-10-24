package linx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestDiceSpeechCommitsAndAcknowledgesBeforeNarration(t *testing.T) {
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
		_ = writeEvent(ctx, c, map[string]any{"type": "session.created"})
		announced := false
		updates := 0
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var e map[string]any
			if json.Unmarshal(data, &e) != nil {
				continue
			}
			if e["type"] == "session.update" {
				updates++
				if updates == 2 {
					requests <- e
					continue
				}
				_ = writeEvent(ctx, c, map[string]any{"type": "session.updated", "session": e["session"]})
				if !announced {
					peers <- c
					announced = true
				}
			} else {
				requests <- e
			}
		}
	}))
	defer provider.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = oldClient }()
	r := room.New(game.Options{Names: []string{"孩子", "家长"}, Props: true})
	state, _, _ := roomGame(r)
	if _, err := r.Do(room.Command{ID: "card", GameID: state.ID, Version: state.Version, Action: "prop", PropID: "launch"}, false); err != nil {
		t.Fatal(err)
	}
	bridge := &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https:", "wss:", 1) + "/v1/realtime", APIKey: "test-only", Model: "test"}}
	done := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		defer close(done)
		_ = bridge.Serve(req.Context(), c, r, CompanionPreferences{Style: "concise", Frequency: "quiet"})
	}))
	defer gateway.Close()
	browser, _, err := websocket.Dial(ctx, strings.Replace(gateway.URL, "http:", "ws:", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		browser.CloseNow()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("gateway did not stop")
		}
	}()
	var up *websocket.Conn
	select {
	case up = <-peers:
	case <-ctx.Done():
		t.Fatal("no provider")
	}
	readUntil(t, ctx, browser, "hello")
	send := func(e map[string]any) {
		t.Helper()
		if err := writeEvent(ctx, up, e); err != nil {
			t.Fatal(err)
		}
	}
	input := func(id, text string) {
		send(map[string]any{"type": "input_audio_buffer.speech_started"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id, "transcript": text})
	}
	complete := func(id string, unexpectedTool bool) {
		send(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
		readUntil(t, ctx, browser, "response.created")
		if unexpectedTool {
			send(map[string]any{"type": "response.output_item.done", "response_id": id, "item": map[string]any{"type": "function_call", "name": "move_piece", "call_id": "unexpected", "arguments": `{"piece_id":"p0-0"}`}})
		}
		send(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		readUntil(t, ctx, browser, "response.done")
	}
	input("exact", "好，掷骰子")
	ack := readUntil(t, ctx, browser, "game_action")
	data, _ := json.Marshal(ack["room"])
	var view room.View
	_ = json.Unmarshal(data, &view)
	var rolled game.State
	_ = json.Unmarshal(view.State, &rolled)
	if rolled.Version != 3 || rolled.Phase != "move" || rolled.Players[0].Stats.Rolls != 1 || len(rolled.DiceRolls) != 1 {
		t.Fatal("missing committed roll", rolled)
	}
	if !strings.Contains(ack["text"].(string), strconv.Itoa(rolled.Dice)) || !strings.Contains(ack["text"].(string), "请选择") {
		t.Fatal("not actual die/next step", ack["text"])
	}
	contextBound := false
	for {
		select {
		case e := <-requests:
			if e["type"] == "conversation.item.create" {
				item := e["item"].(map[string]any)
				encoded, _ := json.Marshal(item["content"])
				contextBound = item["role"] == "system" && strings.Contains(string(encoded), ack["text"].(string))
			}
			if e["type"] == "session.update" {
				send(map[string]any{"type": "session.updated", "session": map[string]any{"instructions": "old host cue"}})
				select {
				case early := <-requests:
					t.Fatalf("generation before matching acknowledgement: %v", early["type"])
				case <-time.After(100 * time.Millisecond):
				}
				send(map[string]any{"type": "session.updated", "session": e["session"]})
			}
			if e["type"] == "response.create" {
				if !contextBound {
					t.Fatal("reply must receive latest committed facts as its input before generation")
				}
				response := e["response"].(map[string]any)
				if response["tool_choice"] != "none" || !strings.Contains(response["instructions"].(string), ack["text"].(string)) {
					t.Fatal("narration not bound to receipt")
				}
				goto spoken
			}
		case <-ctx.Done():
			t.Fatal("no narration request")
		}
	}
spoken:
	send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "exact", "transcript": "好，掷骰子"})
	complete("roll", false)
	for i, text := range []string{"好，骰子", "这次真的掷骰子了吗？", "不要掷骰子"} {
		input("read-"+strconv.Itoa(i), text)
		ack = readUntil(t, ctx, browser, "game_action")
		if !strings.Contains(ack["text"].(string), "没有") {
			t.Fatal("missing no-action acknowledgement", ack)
		}
		waitRequest(t, ctx, requests, "response.create")
		complete("read-response-"+strconv.Itoa(i), true)
		now, _, _ := roomGame(r)
		if now.Version != 3 || now.Players[0].Stats.Rolls != 1 || now.Players[0].Pieces[0].Progress != -1 {
			t.Fatal("query or unexpected provider tool mutated state")
		}
	}
}
