package room

import (
	"encoding/json"
	"fmt"
	"github.com/zkevindev/flytogether/internal/game"
	"path/filepath"
	"testing"
)

func familyProfile(t *testing.T, s *Store, actor, name, avatar string) Profile {
	t.Helper()
	p, err := s.SaveProfile(actor, Profile{Name: name, Avatar: avatar})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func familyRead(t *testing.T, s *Store, actor string) Family {
	t.Helper()
	f, err := s.Family(actor)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Rule fixture creates two legal choices; the final move still goes through DoAs and SQLite.
func familyNearFinish(t *testing.T, r *Room) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = "playing"
	r.state.Players[0].Pieces[0].Progress = r.state.Board.Finish - 2
	r.state.Players[0].Pieces[1].Progress = 10
	if err := r.state.Roll(2); err != nil {
		t.Fatal(err)
	}
	if r.state.Phase != "move" {
		t.Fatal("fixture requires a choice")
	}
	if _, err := r.finish(nil, saveChange{}); err != nil {
		t.Fatal(err)
	}
}
func TestFamilyResultAtomicImmutableIdempotentAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	a, b := familyProfile(t, s, "home", "孩子", "bear"), familyProfile(t, s, "home", "爸爸", "panda")
	r, err := s.Create("home", game.Options{Names: []string{"临时一", "临时二"}, ProfileIDs: []string{a.ID, b.ID}, Props: true})
	if err != nil {
		t.Fatal(err)
	}
	if current(r).Players[0].Name != "孩子" || current(r).Players[0].Avatar != "bear" {
		t.Fatal("profile not resolved")
	}
	familyNearFinish(t, r)
	before := r.Snapshot()
	g := current(r)
	c := Command{ID: game.ID(), GameID: g.ID, Version: g.Version, Action: "move", PieceID: g.Players[0].Pieces[0].ID}
	_, err = s.storage.db.Exec("CREATE TRIGGER fail_result BEFORE INSERT ON family_history BEGIN SELECT RAISE(ABORT,'test'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.DoAs(c, "home"); err == nil {
		t.Fatal("result failure accepted")
	}
	if string(before.State) != string(r.Snapshot().State) || len(familyRead(t, s, "home").Matches) != 0 {
		t.Fatal("partial final move committed")
	}
	_, _ = s.storage.db.Exec("DROP TRIGGER fail_result")
	if _, err = r.DoAs(c, "home"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DoAs(c, "home"); err != nil {
		t.Fatal(err)
	}
	f := familyRead(t, s, "home")
	if f.Total != 1 || len(f.Matches) != 1 || !f.Matches[0].Players[0].Won || f.Matches[0].Players[0].Stats.Arrivals != 1 || f.Matches[0].Players[1].ProfileID != b.ID {
		t.Fatal("wrong result", f)
	}
	data, _ := json.Marshal(f.Matches[0])
	a.Name, a.Avatar = "改名孩子", "fox"
	if _, err = s.SaveProfile("home", a); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DoAs(Command{ID: game.ID(), GameID: g.ID, Version: current(r).Version, Action: "end"}, "home"); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(familyRead(t, s, "home").Matches[0])
	if string(data) != string(after) {
		t.Fatal("history rewritten")
	}
	savedDo(t, r, "home", "replay", nil)
	if familyRead(t, s, "home").Total != 1 {
		t.Fatal("replay created record")
	}
	if err = s.DeleteProfile("home", b.ID); err != nil {
		t.Fatal(err)
	}
	// Room deletion and retention do not cascade into the independent history.
	if err = s.Discard("home", r.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTestSaves(t, path)
	if f = familyRead(t, s, "home"); f.Total != 1 || len(f.Profiles) != 1 || f.Matches[0].Players[1].Name != "爸爸" {
		t.Fatal("history/profile persistence")
	}
	// Retrying a save of a completed legacy room must not resurrect a cleared record.
	if err = s.ClearHistory("home"); err != nil {
		t.Fatal(err)
	}
	p := roomRecord{Schema: saveSchema, ID: r.ID, Owner: "home", Options: r.options, Name: "old", Status: "ended", State: &g}
	p.State.Phase = "finished"
	p.State.Winner = p.State.Players[0].ID
	p.State.Players[0].Pieces[0].Progress = p.State.Board.Finish
	if err = s.storage.save(p, nil, saveChange{}); err != nil {
		t.Fatal(err)
	}
	if familyRead(t, s, "home").Total != 0 {
		t.Fatal("cleared history resurrected")
	}
}
func TestFamilyNetworkOwnershipSnapshotsAndExclusions(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	a := familyProfile(t, s, "host", "房主", "bear")
	b := familyProfile(t, s, "guest", "来客", "rabbit")
	if _, err := s.SaveProfile("guest", a); err == nil {
		t.Fatal("edited foreign profile")
	}
	_ = s.DeleteProfile("guest", a.ID)
	if len(familyRead(t, s, "host").Profiles) != 1 {
		t.Fatal("deleted foreign profile")
	}
	if _, err := s.Create("guest", game.Options{Names: []string{"一", "二"}, ProfileIDs: []string{a.ID}}); err == nil {
		t.Fatal("used foreign profile")
	}
	r, err := s.Create("host", game.Options{Names: []string{"主人", "空席", "机器人"}, ProfileIDs: []string{a.ID}, Network: true, AI: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.JoinProfile("guest", r.ID, r.invite, "ignored", b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if r.SnapshotFor("guest").ProfileIDs[0] != "" || r.SnapshotFor("host").ProfileIDs[1] != "" {
		t.Fatal("leaked profile binding")
	}
	familyNearFinish(t, r)
	savedDo(t, r, "host", "move", func(c *Command) { c.PieceID = "p0-0" })
	hf, gf := familyRead(t, s, "host"), familyRead(t, s, "guest")
	if hf.Total != 1 || gf.Total != 1 || hf.Matches[0].Players[1].ProfileID != "" || gf.Matches[0].Players[0].ProfileID != "" || gf.Matches[0].Players[1].ProfileID != b.ID {
		t.Fatal("network result not scoped")
	}
	_ = s.ClearHistory("host")
	if familyRead(t, s, "guest").Total != 1 {
		t.Fatal("cleared guest results")
	}
	savedDo(t, r, "host", "replay", nil)
	if current(r).Players[1].Avatar != "rabbit" || current(r).Players[1].Name != "来客" {
		t.Fatal("guest avatar lost on replay")
	}
	for _, demo := range []bool{true, false} {
		actor := game.ID()
		r, err = s.Create(actor, game.Options{Names: []string{"一", "二"}, Demo: demo})
		if err != nil {
			t.Fatal(err)
		}
		if demo {
			familyNearFinish(t, r)
			savedDo(t, r, actor, "move", func(c *Command) { c.PieceID = "p0-0" })
		} else {
			savedDo(t, r, actor, "end", nil)
		}
		if familyRead(t, s, actor).Total != 0 {
			t.Fatal("demo/manual end counted")
		}
	}
}
func TestFamilyValidationAndLimits(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	for _, p := range []Profile{{Name: "a", Avatar: "https://example.test/p.png"}, {Name: "\n", Avatar: "bear"}, {Name: "too-long-nickname", Avatar: "bear"}} {
		if _, err := s.SaveProfile("a", p); err == nil {
			t.Fatal("bad profile", p)
		}
	}
	for i := 0; i < 12; i++ {
		familyProfile(t, s, "a", "同名", "cat")
	}
	if _, err := s.SaveProfile("a", Profile{Name: "十三", Avatar: "bear"}); err == nil {
		t.Fatal("limit not enforced")
	}
	a := familyRead(t, s, "a").Profiles[0]
	if _, err := s.FamilyOptions("a", game.Options{Names: []string{"一", "二"}, ProfileIDs: []string{a.ID, a.ID}}); err == nil {
		t.Fatal("duplicate profile")
	}
}

func TestFamilyV2MigrationAndRecentWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, err := s.Create("legacy", game.Options{Names: []string{"旧玩家一", "旧玩家二"}})
	if err != nil {
		t.Fatal(err)
	}
	before := current(r)
	for _, query := range []string{"DROP TABLE family_profiles", "DROP TABLE family_history", "PRAGMA user_version=2"} {
		if _, err = s.storage.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s = openTestSaves(t, path)
	restored := s.Get("legacy", r.ID)
	if restored == nil || restored.Snapshot().Status != "paused" || current(restored).ID != before.ID {
		t.Fatal("v2 room migration lost state")
	}
	familyProfile(t, s, "legacy", "新家人", "cat")
	// Independent result fixtures verify the display window, not a lifetime leaderboard.
	for i := 0; i < 51; i++ {
		result := MatchResult{GameID: fmt.Sprintf("game-%02d", i), Players: []ResultPlayer{{Name: "旧玩家一", Won: true}}}
		payload, _ := json.Marshal(result)
		if _, err = s.storage.db.Exec("INSERT INTO family_history(session,game_id,completed_at,payload) VALUES(?,?,?,?)", "legacy", result.GameID, fmt.Sprintf("2026-10-24T10:%02d:00Z", i), payload); err != nil {
			t.Fatal(err)
		}
	}
	f := familyRead(t, s, "legacy")
	if f.Total != 51 || len(f.Matches) != 50 || f.Matches[0].GameID != "game-50" || f.Matches[49].GameID != "game-01" {
		t.Fatal("wrong recent history window")
	}
}
