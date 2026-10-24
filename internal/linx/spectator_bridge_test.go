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

// Real WebSocket boundaries with a controlled provider expose completion and
// tool-continuation bugs that cannot be found by testing the read tool alone.
func TestSpectatorBridgeCompletesPrivateTurnsAndRejectsLateTools(t *testing.T) {
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
				_ = writeEvent(ctx, c, map[string]any{"type": "session.updated", "session": e["session"]})
				if !announced {
					peers <- c
					announced = true
				}
			}
			select {
			case requests <- e:
			case <-ctx.Done():
				return
			}
		}
	}))
	defer provider.Close()
	old := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = old }()
	store := room.NewStore()
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true, Props: true})
	if err != nil {
		t.Fatal(err)
	}
	host := r.SnapshotFor("host")
	if _, err = store.Watch("fan", r.ID, host.Audience.Token, "奶奶"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PostChat("fan", r.ID, room.ChatInput{ID: "private-chat", Kind: "text", Text: "不要放进助手的房间消息"}); err != nil {
		t.Fatal(err)
	}
	voice, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	initial := string(r.Snapshot().State)
	bridge := &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https:", "wss:", 1) + "/v1/realtime", APIKey: "test-only", Model: "test"}}
	done := make(chan error, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		done <- bridge.Serve(req.Context(), c, voice)
	}))
	defer gateway.Close()
	browser, _, err := websocket.Dial(ctx, strings.Replace(gateway.URL, "http:", "ws:", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.CloseNow()
	up := <-peers
	send := func(e map[string]any) {
		t.Helper()
		if err := writeEvent(ctx, up, e); err != nil {
			t.Fatal(err)
		}
	}
	readUntil(t, ctx, browser, "hello")
	setup := spectatorRequest(t, ctx, requests, "session.update")["session"].(map[string]any)
	tools := setup["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "get_game_state" {
		t.Fatal("spectator tool list", tools)
	}
	input := func(id, text string) {
		send(map[string]any{"type": "input_audio_buffer.speech_started"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id, "transcript": text})
		readUntil(t, ctx, browser, "stt")
		update := spectatorRequest(t, ctx, requests, "session.update")
		encoded, _ := json.Marshal(update)
		if strings.Contains(string(encoded), "不要放进助手的房间消息") || strings.Contains(string(encoded), host.Network.Invite) || !strings.Contains(string(encoded), "standings") {
			t.Fatal("missing or private facts", string(encoded))
		}
		spectatorRequest(t, ctx, requests, "response.create")
	}
	created := func(id string) {
		send(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
		readUntil(t, ctx, browser, "response.created")
	}
	complete := func(id string) {
		send(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
	}
	input("one", "介绍一下当前局势和规则")
	created("read-one")
	send(map[string]any{"type": "response.output_item.done", "response_id": "read-one", "item": map[string]any{"type": "function_call", "name": "get_game_state", "call_id": "read", "arguments": "{}"}})
	complete("read-one")
	output := spectatorRequest(t, ctx, requests, "conversation.item.create")
	encoded, _ := json.Marshal(output)
	if !strings.Contains(string(encoded), "standings") || strings.Contains(string(encoded), "action_token") {
		t.Fatal("read result", string(encoded))
	}
	spectatorRequest(t, ctx, requests, "response.create")
	created("answer-one")
	send(map[string]any{"type": "response.audio.delta", "response_id": "answer-one", "delta": "AAAAAA=="})
	readUntil(t, ctx, browser, "response.audio.delta")
	complete("answer-one")
	readUntil(t, ctx, browser, "response.done")
	// A second normal question must complete even though spectators own no game grant.
	input("two", "解释一下游戏规则")
	created("answer-two")
	complete("answer-two")
	readUntil(t, ctx, browser, "response.done")
	input("three", "介绍一下道具")
	created("attempted-write")
	send(map[string]any{"type": "response.output_item.done", "response_id": "attempted-write", "item": map[string]any{"type": "function_call", "name": "roll_dice", "call_id": "write", "arguments": "{}"}})
	complete("attempted-write")
	output = spectatorRequest(t, ctx, requests, "conversation.item.create")
	encoded, _ = json.Marshal(output)
	if !strings.Contains(string(encoded), "error") || !strings.Contains(string(encoded), "观众不能操作") {
		t.Fatal("write not rejected", string(encoded))
	}
	spectatorRequest(t, ctx, requests, "response.create")
	created("refusal")
	complete("refusal")
	readUntil(t, ctx, browser, "response.done")
	if string(r.Snapshot().State) != initial || r.Snapshot().Voice != "disconnected" {
		t.Fatal("spectator changed board or intercom")
	}
	// Revocation must close an idle connection without waiting for another utterance.
	off := false
	if _, err = store.ConfigureAudience("host", r.ID, &off, false, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil || !strings.Contains(err.Error(), "关闭") {
			t.Fatal("revocation did not close voice", err)
		}
	case <-ctx.Done():
		t.Fatal("revoked spectator connection stayed alive")
	}
	if bridge.active != 0 || bridge.spectators != 0 {
		t.Fatal("voice slot leaked")
	}
}

func spectatorRequest(t *testing.T, ctx context.Context, requests <-chan map[string]any, kind string) map[string]any {
	t.Helper()
	for {
		select {
		case event := <-requests:
			if event["type"] == kind {
				return event
			}
		case <-ctx.Done():
			t.Fatalf("provider did not receive %s", kind)
			return nil
		}
	}
}
