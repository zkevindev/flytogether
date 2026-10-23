package room

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"testing"
)

func TestPropCommandIsIdempotentAndUsesVoiceGrant(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Props: true})
	var s game.State
	_ = json.Unmarshal(r.Snapshot().State, &s)
	c := Command{ID: "card-once", GameID: s.ID, Version: s.Version, Action: "prop", PropID: "launch"}
	first, err := r.Do(c, false)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := r.Do(c, false)
	if err != nil || string(first.State) != string(retry.State) {
		t.Fatal("retry consumed a second card")
	}
	r = New(game.Options{Names: []string{"甲", "乙"}, Props: true})
	_ = r.BeginSpeech()
	grant := r.CurrentActionToken()
	if _, err = r.Tool("use_prop", map[string]string{"prop_id": "launch", "action_token": grant}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Tool("roll_dice", map[string]string{"action_token": grant}); err == nil {
		t.Fatal("a voice utterance performed two writes")
	}
}
