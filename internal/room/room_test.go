package room

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func current(r *Room) game.State {
	var s game.State
	_ = json.Unmarshal(r.Snapshot().State, &s)
	return s
}

func TestCommandRetriesDoNotRollAgain(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	s := current(r)
	c := Command{ID: "retry-1", GameID: s.ID, Version: s.Version, Action: "roll"}
	first, err := r.Do(c, false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.Do(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.State) != string(next.State) {
		t.Fatal("retry changed result")
	}
	c.Action = "move"
	if _, err = r.Do(c, false); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}

func TestOnlyOneConcurrentCommandCanCommit(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	s := current(r)
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Do(Command{ID: game.ID(), GameID: s.ID, Version: s.Version, Action: "roll"}, false)
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 1 {
		t.Fatalf("committed %d commands", accepted)
	}
}

func TestVoiceGrantAllowsOneMutationAndClickCannotRace(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	raw, err := r.Tool("get_game_state", nil)
	if err != nil {
		t.Fatal(err)
	}
	token := raw.(map[string]any)["action_token"].(string)
	if token == "" {
		t.Fatal("missing voice token")
	}
	s := current(r)
	if _, err = r.Do(Command{ID: "click", GameID: s.ID, Version: s.Version, Action: "roll"}, false); err == nil {
		t.Fatal("click raced voice")
	}
	first, err := r.Tool("roll_dice", map[string]string{"action_token": token})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Tool("roll_dice", map[string]string{"action_token": token})
	if err != nil {
		t.Fatal("idempotent retry rejected")
	}
	after, _ := r.Tool("get_game_state", nil)
	if after.(map[string]any)["action_token"] != "" {
		t.Fatal("second mutation was authorized")
	}
	if first == nil {
		t.Fatal("missing result")
	}
	r.VoiceState("ready")
}

func TestReadOnlyVoiceSessionAllowsQuestionsButRejectsMutations(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	voice, err := r.VoiceForMode("browser", true)
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	if !voice.ReadOnly() {
		t.Fatal("read-only mode was not bound to the voice session")
	}
	if err = voice.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	busy := r.busy
	r.mu.Unlock()
	if busy || voice.CurrentActionToken() != "" {
		t.Fatal("read-only speech must not lock clicks or create a write grant")
	}
	if _, err = voice.Tool("get_game_state", nil); err != nil {
		t.Fatalf("question was rejected: %v", err)
	}
	before := current(r)
	for _, name := range []string{"roll_dice", "move_piece", "use_prop", "coach_move", "confirm_coach"} {
		if _, err = voice.Tool(name, map[string]string{"action_token": voice.CurrentActionToken()}); err == nil {
			t.Fatalf("read-only voice executed %s", name)
		} else {
			errorCode(t, err, "VOICE_READ_ONLY")
		}
	}
	if after := current(r); after.Version != before.Version || after.Phase != before.Phase || after.Dice != before.Dice {
		t.Fatalf("read-only voice mutated game: before=%+v after=%+v", before, after)
	}
	state := current(r)
	if _, err = r.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll"}, "browser"); err != nil {
		t.Fatalf("click was blocked during read-only voice: %v", err)
	}
}

func TestRestartRejectsOldGameAndOldVoiceToken(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	_ = r.BeginSpeech()
	raw, _ := r.Tool("get_game_state", nil)
	token := raw.(map[string]any)["action_token"].(string)
	old := current(r)
	r.VoiceState("ready")
	if _, err := r.Restart(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Do(Command{ID: "old-game", GameID: old.ID, Version: old.Version, Action: "roll"}, false); err == nil {
		t.Fatal("old game accepted")
	}
	if _, err := r.Tool("roll_dice", map[string]string{"action_token": token}); err == nil {
		t.Fatal("old token accepted")
	}
}
