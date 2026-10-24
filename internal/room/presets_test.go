package room

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestPresetSurvivesRestartAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s, err := OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Create("owner", game.Options{Preset: "gentle", Names: []string{"甲", "乙"}, Board: "cross"})
	if err != nil {
		t.Fatal(err)
	}
	var schema int
	_ = s.storage.db.QueryRow("PRAGMA user_version").Scan(&schema)
	if schema != databaseSchema {
		t.Fatal("preset saves must block unsafe older executables")
	}
	id := r.ID
	before := r.Snapshot()
	s.Close()
	s, err = OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r = s.Get("owner", id)
	if r == nil || r.Snapshot().Status != "paused" {
		t.Fatal("gentle save not restored")
	}
	var state game.State
	_ = json.Unmarshal(r.Snapshot().State, &state)
	if state.Preset != "gentle" || state.Players[0].Props["launch"] != 2 {
		t.Fatal("lost preset or stock")
	}
	_, err = r.Do(Command{ID: "end", GameID: state.ID, Version: state.Version, Action: "end"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(r.Snapshot().State, &state)
	next, err := r.Do(Command{ID: "replay", GameID: state.ID, Version: state.Version, Action: "replay"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(next.State, &state)
	if state.Preset != "gentle" || state.RuleInfo.Name != "亲子轻松" || state.Players[0].Props["shield"] != 2 {
		t.Fatal("replay changed fixed rules")
	}
	if string(before.State) == string(next.State) {
		t.Fatal("replay did not create new game")
	}
}
