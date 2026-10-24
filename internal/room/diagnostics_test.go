package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func enableDiagnostic(t *testing.T, s *Store, text bool) {
	t.Helper()
	if e := s.ConfigureDiagnostics(DiagnosticConfig{Enabled: true, Transcripts: text, Retention: 24 * time.Hour}); e != nil {
		t.Fatal(e)
	}
}
func TestDiagnosticLifecycleAtomicReplayAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	enableDiagnostic(t, s, true)
	r, e := s.Create("secret-cookie", game.Options{Names: []string{"孩子", "家长"}, Props: true})
	if e != nil {
		t.Fatal(e)
	}
	original := current(r).ID
	savedDo(t, r, "secret-cookie", "prop", func(c *Command) { c.PropID = "launch" })
	roll := savedDo(t, r, "secret-cookie", "roll", nil)
	timeline, e := s.DiagnosticTimeline(context.Background(), original, DiagnosticQuery{Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	n := len(timeline.Events)
	if _, e = r.DoAs(roll, "secret-cookie"); e != nil {
		t.Fatal(e)
	}
	again, _ := s.DiagnosticTimeline(context.Background(), original, DiagnosticQuery{Limit: 100})
	if len(again.Events) != n {
		t.Fatal("retry duplicated diagnostic events")
	}
	prior := current(r).Version
	realDice := current(r).Dice
	if _, e = s.storage.db.Exec(`CREATE TRIGGER abort_diagnostic BEFORE INSERT ON diagnostic_events BEGIN SELECT RAISE(ABORT,'blocked'); END`); e != nil {
		t.Fatal(e)
	}
	c := Command{ID: game.ID(), GameID: original, Version: prior, Action: "move", PieceID: current(r).Legal[0].PieceID}
	if _, e = r.DoAs(c, "secret-cookie"); e == nil || current(r).Version != prior {
		t.Fatal("diagnostic failure did not roll back authoritative state")
	}
	s.storage.db.Exec("DROP TRIGGER abort_diagnostic")
	savedDo(t, r, "secret-cookie", "end", nil)
	if _, e = r.RestartAs(game.Options{Names: []string{"孩子", "家长"}, Props: true}, "secret-cookie"); e != nil {
		t.Fatal(e)
	}
	second := current(r).ID
	old, e := s.DiagnosticMatch(context.Background(), original)
	if e != nil || old.Status != "ended" || old.State.Dice != realDice {
		t.Fatal("old match unavailable", e)
	}
	list, e := s.DiagnosticMatches(context.Background(), DiagnosticQuery{Search: r.ID, Limit: 1})
	if e != nil || list.Total != 2 || len(list.Matches) != 1 {
		t.Fatal("match list/pagination", e, list)
	}
	b, _ := json.Marshal(old)
	if strings.Contains(string(b), "secret-cookie") || strings.Contains(string(b), "action_token") || strings.Contains(string(b), "invite") {
		t.Fatal("private fields leaked")
	}
	s.Close()
	s = openTestSaves(t, path)
	enableDiagnostic(t, s, true)
	if _, e = s.DiagnosticMatch(context.Background(), original); e != nil {
		t.Fatal("restart lost old game")
	}
	if _, e = s.DiagnosticMatch(context.Background(), second); e != nil {
		t.Fatal("restart lost current game")
	}
	if e = s.Discard("secret-cookie", r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DiagnosticMatch(context.Background(), original); e != nil {
		t.Fatal("room delete cascaded diagnosis")
	}
}
func TestDiagnosticLegacyBaselineTextRetentionAndPagination(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	r, e := s.Create("browser", game.Options{Names: []string{"孩子", "家长"}, Props: true})
	if e != nil {
		t.Fatal(e)
	}
	savedDo(t, r, "browser", "roll", nil)
	g := current(r)
	enableDiagnostic(t, s, true)
	baseline, _ := s.DiagnosticMatch(context.Background(), g.ID)
	if baseline.FirstVersion != g.Version {
		t.Fatal("invented earlier start")
	}
	savedDo(t, r, "browser", "pause", nil)
	entries, _ := s.DiagnosticTimeline(context.Background(), g.ID, DiagnosticQuery{Limit: 100})
	for _, entry := range entries.Events {
		if entry.Kind == "game" && entry.Action == "roll" {
			t.Fatal("legacy feed presented as newly observed roll")
		}
	}
	for i := 0; i < 5; i++ {
		s.Diagnostic(DiagnosticEvent{RoomID: r.ID, GameID: g.ID, Kind: "user_text", Source: "realtime", Text: "api_key=verysecret 密码：123456 你好"})
	}
	page, e := s.DiagnosticTimeline(context.Background(), g.ID, DiagnosticQuery{Kind: "user_text", Limit: 2})
	if e != nil || len(page.Events) != 2 || page.NextBefore == 0 {
		t.Fatal("event cursor", e)
	}
	next, _ := s.DiagnosticTimeline(context.Background(), g.ID, DiagnosticQuery{Kind: "user_text", Limit: 2, Before: page.NextBefore})
	if len(next.Events) != 2 || next.Events[0].ID >= page.Events[1].ID {
		t.Fatal("cursor did not advance")
	}
	if strings.Contains(page.Events[0].Text, "verysecret") || strings.Contains(page.Events[0].Text, "123456") {
		t.Fatal("credential masking")
	}
	enableDiagnostic(t, s, false)
	s.Diagnostic(DiagnosticEvent{RoomID: r.ID, GameID: g.ID, Kind: "assistant_text", Source: "realtime", Text: "private text"})
	redacted, _ := s.DiagnosticTimeline(context.Background(), g.ID, DiagnosticQuery{Kind: "assistant_text"})
	if redacted.Events[0].Text != "" {
		t.Fatal("disabled text persisted")
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	s.storage.db.Exec("UPDATE diagnostic_events SET at=?", old)
	active, _ := s.DiagnosticTimeline(context.Background(), g.ID, DiagnosticQuery{})
	if len(active.Events) != 0 {
		t.Fatal("expired text returned before prune")
	}
	if e = s.PruneDiagnostics(context.Background()); e != nil {
		t.Fatal(e)
	}
	var count int
	s.storage.db.QueryRow("SELECT count(*) FROM diagnostic_events").Scan(&count)
	if count != 0 {
		t.Fatal("old text not pruned")
	}
	s.storage.db.Exec("UPDATE diagnostic_matches SET updated_at=?", old)
	if _, e = s.DiagnosticMatch(context.Background(), g.ID); e != sql.ErrNoRows {
		t.Fatal("expired match returned")
	}
	if e = s.PruneDiagnostics(context.Background()); e != nil {
		t.Fatal(e)
	}
	s.storage.db.QueryRow("SELECT count(*) FROM diagnostic_matches").Scan(&count)
	if count != 0 {
		t.Fatal("old matches not pruned")
	}
}
func TestDiagnosticV3MigrationAndFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _ := s.Create("browser", game.Options{Names: []string{"一", "二"}})
	id := current(r).ID
	for _, query := range []string{"DROP TABLE diagnostic_matches", "DROP TABLE diagnostic_events", "PRAGMA user_version=3"} {
		if _, e := s.storage.db.Exec(query); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s = openTestSaves(t, path)
	enableDiagnostic(t, s, true)
	if _, e := s.DiagnosticMatch(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	s.storage.db.Exec("PRAGMA query_only=ON")
	s.Diagnostic(DiagnosticEvent{GameID: id, Kind: "connection"})
	if s.DiagnosticStatus()["write_failures"] != uint64(1) {
		t.Fatal("transport failure hidden")
	}
	s.storage.db.Exec("PRAGMA query_only=OFF")
	if e := s.ConfigureDiagnostics(DiagnosticConfig{}); e != nil {
		t.Fatal(e)
	}
	s.Diagnostic(DiagnosticEvent{GameID: id, Kind: "user_text", Text: "disabled"})
	var n int
	s.storage.db.QueryRow("SELECT count(*) FROM diagnostic_events WHERE kind='user_text'").Scan(&n)
	if n != 0 {
		t.Fatal("disabled logging")
	}
}
