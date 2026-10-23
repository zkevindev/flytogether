package room

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"testing"
)

func lifecycleCommand(r *Room, action string) (View, error) {
	return r.Do(Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: action}, false)
}

func TestRoomLifecyclePreservesBoardAndBlocksAllActors(t *testing.T) {
	r := New(game.Options{Names: []string{"孩子", "AI"}, AI: true, Props: true, Waiting: true, Difficulty: "hard", RoomName: "家庭一号"})
	if v := r.Snapshot(); v.Status != "waiting" || v.Name != "家庭一号" {
		t.Fatal("room did not enter named lobby")
	}
	before, _ := json.Marshal(r.state)
	if _, err := lifecycleCommand(r, "roll"); err == nil {
		t.Fatal("lobby accepted roll")
	}
	if err := r.BeginSpeech(); err == nil {
		t.Fatal("lobby accepted voice")
	}
	r.AI()
	after, _ := json.Marshal(r.state)
	if string(before) != string(after) {
		t.Fatal("waiting board changed")
	}
	if _, err := lifecycleCommand(r, "start"); err != nil {
		t.Fatal(err)
	}
	_ = r.state.Roll(6)
	if r.state.Phase != "move" {
		t.Fatal("test requires a pending piece choice")
	}
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	token := r.CurrentActionToken()
	gameID, die, phase := r.state.ID, r.state.Dice, r.state.Phase
	if _, err := lifecycleCommand(r, "pause"); err != nil {
		t.Fatal(err)
	}
	if r.CurrentActionToken() != "" || r.busy || r.Snapshot().Status != "paused" {
		t.Fatal("pause did not revoke voice grant")
	}
	if _, err := lifecycleCommand(r, "move"); err == nil {
		t.Fatal("pause allowed click movement")
	}
	if _, err := r.Tool("move_piece", map[string]string{"piece_id": "p0-0", "action_token": token}); err == nil {
		t.Fatal("pause allowed late voice movement")
	}
	r.AI()
	if r.state.ID != gameID || r.state.Dice != die || r.state.Phase != phase {
		t.Fatal("pause lost board or dice")
	}
	if _, err := lifecycleCommand(r, "resume"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Tool("move_piece", map[string]string{"piece_id": "p0-0", "action_token": token}); err == nil {
		t.Fatal("resume resurrected old voice grant")
	}
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	token = r.CurrentActionToken()
	if _, err := lifecycleCommand(r, "end"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Tool("move_piece", map[string]string{"piece_id": "p0-0", "action_token": token}); err == nil {
		t.Fatal("ended room accepted late voice")
	}
	if _, err := lifecycleCommand(r, "resume"); err == nil {
		t.Fatal("ended room resumed")
	}
	if _, err := r.Restart(game.Options{Names: []string{"孩子", "爸爸"}, Waiting: true, Props: true}); err != nil {
		t.Fatal(err)
	}
	if r.state.ID == gameID || r.status != "waiting" || r.state.Players[0].Props["bomb"] != 1 {
		t.Fatal("restart failed to create fresh prepared match")
	}
}

func TestLifecycleCommandsAreVersionedAndIdempotent(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Waiting: true})
	c := Command{ID: "start-once", GameID: r.state.ID, Version: r.state.Version, Action: "start"}
	if _, err := r.Do(c, false); err != nil {
		t.Fatal(err)
	}
	version := r.state.Version
	if _, err := r.Do(c, false); err != nil || r.state.Version != version {
		t.Fatal("duplicate start replayed")
	}
	c.ID = "stale-roll"
	c.Action = "roll"
	if _, err := r.Do(c, false); err == nil {
		t.Fatal("pre-start command accepted")
	}
	before, _ := json.Marshal(r.state)
	if _, err := lifecycleCommand(r, "resume"); err == nil {
		t.Fatal("playing room accepted resume")
	}
	after, _ := json.Marshal(r.state)
	if string(before) != string(after) {
		t.Fatal("invalid transition mutated game")
	}
}

func TestDifficultyChangesAIPropPolicy(t *testing.T) {
	for _, difficulty := range []string{"easy", "normal", "hard"} {
		r := New(game.Options{Names: []string{"甲", "AI"}, AI: true, Props: true, Demo: true, Difficulty: difficulty})
		r.state.Current = 1
		r.AI()
		switch difficulty {
		case "easy":
			if r.state.Players[1].Props["bomb"] != 1 || r.state.ActiveProp != "" {
				t.Fatal("easy AI used a prop")
			}
		case "normal":
			if r.state.ActiveProp != "boost" || r.state.Players[1].Props["bomb"] != 1 {
				t.Fatal("normal AI did not use support policy")
			}
		case "hard":
			if r.state.ActiveProp != "bomb" || r.state.Players[0].Pieces[1].Progress != -1 {
				t.Fatal("hard AI did not attack leading opponent")
			}
		}
		r.state.Current = 1
		if _, err := lifecycleCommand(r, "pause"); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(r.state)
		r.AI()
		after, _ := json.Marshal(r.state)
		if string(before) != string(after) {
			t.Fatalf("%s AI moved while paused", difficulty)
		}
	}
}

func TestCreatingNewRoomEndsAndReplacesOwnedRoom(t *testing.T) {
	s := NewStore()
	old, err := s.Create("session", game.Options{Names: []string{"甲", "乙"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = old.BeginSpeech()
	token := old.CurrentActionToken()
	next, err := s.CreateNew("session", game.Options{Names: []string{"孩子", "爸爸"}, RoomName: "新房间", Difficulty: "easy"})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == old.ID || next.status != "waiting" || old.status != "ended" || s.Get("session", old.ID) != nil || s.Get("session", next.ID) != next {
		t.Fatal("room ownership not replaced")
	}
	if _, err := old.Tool("roll_dice", map[string]string{"action_token": token}); err == nil {
		t.Fatal("old room's voice grant survived replacement")
	}
	if s.Get("stranger", next.ID) != nil {
		t.Fatal("new room exposed to another session")
	}
}

func TestPauseStillWorksIfAIJustAdvanced(t *testing.T) {
	r := New(game.Options{Names: []string{"孩子", "AI"}, AI: true})
	c := Command{ID: "pause-now", GameID: r.state.ID, Version: r.state.Version, Action: "pause"}
	r.state.Current = 1
	r.AI()
	if r.state.Version == c.Version {
		t.Fatal("test requires an AI update")
	}
	if _, err := r.Do(c, false); err != nil || r.status != "paused" {
		t.Fatal("AI update prevented pause")
	}
	before, _ := json.Marshal(r.state)
	r.AI()
	after, _ := json.Marshal(r.state)
	if string(before) != string(after) {
		t.Fatal("AI moved after pause")
	}
}
