package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func diagnosticCoverageStore(t *testing.T) *Store {
	t.Helper()
	s := openTestSaves(t, filepath.Join(t.TempDir(), "diagnostics.db"))
	enableDiagnostic(t, s, true)
	return s
}

func diagnosticCoverageExec(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.storage.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func diagnosticCoverageMatch(t *testing.T, s *Store, id, room, name, status string, at time.Time) {
	t.Helper()
	raw, err := json.Marshal(game.State{ID: id, Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	diagnosticCoverageExec(t, s, `INSERT INTO diagnostic_matches VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, room, name, status, 3, 4, 1, stamp, stamp, 3, raw)
}

func TestDiagnosticsCoverageWithoutStorage(t *testing.T) {
	s := NewStore()
	ctx := context.Background()
	if err := s.ConfigureDiagnostics(DiagnosticConfig{Enabled: true}); err == nil || !strings.Contains(err.Error(), "persistent storage") {
		t.Fatalf("configure: %v", err)
	}
	s.Diagnostic(DiagnosticEvent{Text: "not persisted"})
	if status := s.DiagnosticStatus(); len(status) != 1 || status["enabled"] != false {
		t.Fatalf("status: %#v", status)
	}
	if err := s.PruneDiagnostics(ctx); err != nil {
		t.Fatal(err)
	}
	if page, err := s.DiagnosticMatches(ctx, DiagnosticQuery{}); err == nil || page.Matches == nil || page.Total != 0 {
		t.Fatalf("matches: %+v %v", page, err)
	}
	if _, err := s.DiagnosticMatch(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("match: %v", err)
	}
	if page, err := s.DiagnosticTimeline(ctx, "missing", DiagnosticQuery{}); !errors.Is(err, sql.ErrNoRows) || page.Events == nil {
		t.Fatalf("timeline: %+v %v", page, err)
	}
}

func TestDiagnosticsCoverageDisabledAndDefaultRetention(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "disabled.db"))
	if got := s.storage.diagnosticConfig(); got != (DiagnosticConfig{}) {
		t.Fatalf("initial config: %+v", got)
	}
	if err := s.ConfigureDiagnostics(DiagnosticConfig{Transcripts: true, Retention: time.Minute}); err != nil {
		t.Fatal(err)
	}
	status := s.DiagnosticStatus()
	if status["enabled"] != false || status["transcripts"] != true || status["retention_hours"] != 168 || status["write_failures"] != uint64(0) {
		t.Fatalf("status: %#v", status)
	}
	s.Diagnostic(DiagnosticEvent{Kind: "connection", Text: "disabled"})
	var n int
	if err := s.storage.db.QueryRow("SELECT count(*) FROM diagnostic_events").Scan(&n); err != nil || n != 0 {
		t.Fatalf("disabled writes: %d %v", n, err)
	}
	// Neither disabled recording nor an absent game snapshot requires a transaction.
	if err := s.storage.saveDiagnostics(context.Background(), nil, roomRecord{State: &game.State{}}, nil, false); err != nil {
		t.Fatal(err)
	}
	enableDiagnostic(t, s, true)
	if err := s.storage.saveDiagnostics(context.Background(), nil, roomRecord{}, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticsCoverageMatchFiltersAndPaging(t *testing.T) {
	s := diagnosticCoverageStore(t)
	now := time.Now().Add(-time.Minute)
	diagnosticCoverageMatch(t, s, "game-a", "room-shared", "alpha", "active", now)
	diagnosticCoverageMatch(t, s, "game-b", "room-shared", "beta", "ended", now)
	diagnosticCoverageMatch(t, s, "game-c", "room-other", "alpha", "active", now)
	diagnosticCoverageMatch(t, s, "expired", "room-shared", "alpha", "active", now.Add(-48*time.Hour))
	cases := []struct {
		name  string
		q     DiagnosticQuery
		total int
		ids   string
	}{
		{"default limit negative offset", DiagnosticQuery{Limit: 51, Offset: -1}, 3, "game-c,game-b,game-a"},
		{"room search", DiagnosticQuery{Search: "room-shared", Limit: 1, Offset: 1}, 2, "game-a"},
		{"game search", DiagnosticQuery{Search: "game-b"}, 1, "game-b"},
		{"name and status", DiagnosticQuery{Search: "alpha", Status: "active"}, 2, "game-c,game-a"},
		{"status", DiagnosticQuery{Status: "ended"}, 1, "game-b"},
		{"past end", DiagnosticQuery{Offset: 10}, 3, ""},
		{"literal wildcard", DiagnosticQuery{Search: "%"}, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.DiagnosticMatches(context.Background(), tc.q)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, m := range page.Matches {
				ids = append(ids, m.GameID)
				if m.State != nil || !m.Network || m.Version != 3 || m.FirstVersion != 3 {
					t.Fatalf("metadata: %+v", m)
				}
			}
			if page.Total != tc.total || strings.Join(ids, ",") != tc.ids {
				t.Fatalf("page: %+v", page)
			}
		})
	}
	if _, err := s.DiagnosticMatch(context.Background(), "expired"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired: %v", err)
	}
}

func TestDiagnosticsCoverageTimelineFiltersAndCursor(t *testing.T) {
	s := diagnosticCoverageStore(t)
	diagnosticCoverageMatch(t, s, "match", "room", "name", "active", time.Now())
	for i, source := range []string{"click", "realtime", "tts", "voice", "spectator", "browser"} {
		s.Diagnostic(DiagnosticEvent{GameID: "match", RoomID: "room", Kind: "connection", Source: source, Code: timeKey(i), Text: source})
	}
	s.Diagnostic(DiagnosticEvent{GameID: "match", Kind: "connection", Source: "voice", At: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)})
	s.Diagnostic(DiagnosticEvent{GameID: "another", Kind: "connection", Source: "voice"})
	page, err := s.DiagnosticTimeline(context.Background(), "match", DiagnosticQuery{Kind: "voice", Limit: 2})
	if err != nil || len(page.Events) != 2 || page.Events[0].Source != "spectator" || page.Events[1].Source != "voice" || page.NextBefore != page.Events[1].ID {
		t.Fatalf("first: %+v %v", page, err)
	}
	next, err := s.DiagnosticTimeline(context.Background(), "match", DiagnosticQuery{Kind: "voice", Limit: 2, Before: page.NextBefore})
	if err != nil || len(next.Events) != 2 || next.Events[0].Source != "tts" || next.Events[1].Source != "realtime" || next.NextBefore != 0 {
		t.Fatalf("next: %+v %v", next, err)
	}
	for _, limit := range []int{0, 101} {
		all, err := s.DiagnosticTimeline(context.Background(), "match", DiagnosticQuery{Kind: "connection", Limit: limit})
		if err != nil || len(all.Events) != 6 || all.NextBefore != 0 {
			t.Fatalf("default limit: %+v %v", all, err)
		}
	}
	empty, err := s.DiagnosticTimeline(context.Background(), "match", DiagnosticQuery{Kind: "nonexistent"})
	if err != nil || len(empty.Events) != 0 || empty.Events == nil {
		t.Fatalf("empty: %+v %v", empty, err)
	}
}

func TestDiagnosticsCoverageCorruptSnapshotsAndColumns(t *testing.T) {
	t.Run("snapshot decoding and save rollback", func(t *testing.T) {
		s := diagnosticCoverageStore(t)
		r, err := s.Create("host", game.Options{Names: []string{"one", "two"}})
		if err != nil {
			t.Fatal(err)
		}
		id := current(r).ID
		diagnosticCoverageExec(t, s, "UPDATE diagnostic_matches SET snapshot=? WHERE game_id=?", []byte("{broken"), id)
		if _, err = s.DiagnosticMatch(context.Background(), id); err == nil {
			t.Fatal("corrupt snapshot accepted")
		}
		if page, err := s.DiagnosticTimeline(context.Background(), id, DiagnosticQuery{}); err == nil || len(page.Events) != 0 {
			t.Fatalf("corrupt timeline: %+v %v", page, err)
		}
		if err = s.ConfigureDiagnostics(DiagnosticConfig{Enabled: true, Retention: 24 * time.Hour}); err == nil {
			t.Fatal("baseline overwrote corrupt history")
		}
		var raw []byte
		if err = s.storage.db.QueryRow("SELECT snapshot FROM diagnostic_matches WHERE game_id=?", id).Scan(&raw); err != nil || string(raw) != "{broken" {
			t.Fatalf("failed baseline mutated snapshot: %q %v", raw, err)
		}
	})
	t.Run("match scan", func(t *testing.T) {
		s := diagnosticCoverageStore(t)
		diagnosticCoverageMatch(t, s, "bad", "room", "name", "active", time.Now())
		diagnosticCoverageExec(t, s, "UPDATE diagnostic_matches SET version='not-an-integer'")
		if page, err := s.DiagnosticMatches(context.Background(), DiagnosticQuery{}); err == nil || len(page.Matches) != 0 || page.Total != 1 {
			t.Fatalf("bad column: %+v %v", page, err)
		}
	})
	t.Run("event scan", func(t *testing.T) {
		s := diagnosticCoverageStore(t)
		diagnosticCoverageMatch(t, s, "bad", "room", "name", "active", time.Now())
		s.Diagnostic(DiagnosticEvent{GameID: "bad", Kind: "connection"})
		diagnosticCoverageExec(t, s, "UPDATE diagnostic_events SET duration_ms='not-an-integer'")
		if page, err := s.DiagnosticTimeline(context.Background(), "bad", DiagnosticQuery{}); err == nil || len(page.Events) != 0 {
			t.Fatalf("bad column: %+v %v", page, err)
		}
	})
	t.Run("missing event table", func(t *testing.T) {
		s := diagnosticCoverageStore(t)
		diagnosticCoverageMatch(t, s, "bad", "room", "name", "active", time.Now())
		diagnosticCoverageExec(t, s, "DROP TABLE diagnostic_events")
		if _, err := s.DiagnosticTimeline(context.Background(), "bad", DiagnosticQuery{}); err == nil {
			t.Fatal("missing event table ignored")
		}
	})
}

func TestDiagnosticsCoverageClosedDatabase(t *testing.T) {
	s := diagnosticCoverageStore(t)
	if err := s.storage.db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.ConfigureDiagnostics(DiagnosticConfig{Enabled: true}); err == nil {
		t.Fatal("configure accepted closed database")
	}
	if err := s.PruneDiagnostics(ctx); err == nil {
		t.Fatal("prune accepted closed database")
	}
	if _, err := s.DiagnosticMatches(ctx, DiagnosticQuery{}); err == nil {
		t.Fatal("list accepted closed database")
	}
	if _, err := s.DiagnosticMatch(ctx, "match"); err == nil {
		t.Fatal("match accepted closed database")
	}
	s.Diagnostic(DiagnosticEvent{Kind: "connection"})
	if got := s.DiagnosticStatus()["write_failures"]; got != uint64(1) {
		t.Fatalf("failure count: %v", got)
	}
}

func TestDiagnosticsCoveragePruneDefaultsAndAtomicFailure(t *testing.T) {
	t.Run("default retention", func(t *testing.T) {
		s := diagnosticCoverageStore(t)
		s.storage.diagnostics.Store(&DiagnosticConfig{Enabled: true})
		diagnosticCoverageMatch(t, s, "old", "room", "name", "active", time.Now().Add(-8*24*time.Hour))
		diagnosticCoverageMatch(t, s, "recent", "room", "name", "active", time.Now().Add(-2*24*time.Hour))
		for _, item := range []struct {
			id  string
			age time.Duration
		}{{"old", 8 * 24 * time.Hour}, {"recent", 2 * 24 * time.Hour}} {
			s.Diagnostic(DiagnosticEvent{GameID: item.id, Kind: "connection", At: time.Now().Add(-item.age).UTC().Format(time.RFC3339Nano)})
		}
		if err := s.PruneDiagnostics(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"diagnostic_matches", "diagnostic_events"} {
			var id string
			if err := s.storage.db.QueryRow("SELECT game_id FROM " + table).Scan(&id); err != nil || id != "recent" {
				t.Fatalf("%s survivor %q %v", table, id, err)
			}
			var n int
			if err := s.storage.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 1 {
				t.Fatalf("%s count %d %v", table, n, err)
			}
		}
	})
	for _, table := range []string{"diagnostic_events", "diagnostic_matches"} {
		t.Run(table+" delete fails", func(t *testing.T) {
			s := diagnosticCoverageStore(t)
			old := time.Now().Add(-48 * time.Hour)
			diagnosticCoverageMatch(t, s, "old", "room", "name", "active", old)
			s.Diagnostic(DiagnosticEvent{GameID: "old", Kind: "connection", At: old.UTC().Format(time.RFC3339Nano)})
			diagnosticCoverageExec(t, s, "CREATE TRIGGER block_prune BEFORE DELETE ON "+table+" BEGIN SELECT RAISE(ABORT,'prune blocked'); END")
			if err := s.PruneDiagnostics(context.Background()); err == nil || !strings.Contains(err.Error(), "prune blocked") {
				t.Fatalf("prune: %v", err)
			}
			for _, table := range []string{"diagnostic_events", "diagnostic_matches"} {
				var n int
				if err := s.storage.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 1 {
					t.Fatalf("rollback %s: %d %v", table, n, err)
				}
			}
		})
	}
}

func TestDiagnosticsCoverageTextBoundaries(t *testing.T) {
	input := strings.Repeat("界", 2001) + "\x00"
	got := DiagnosticText(input)
	if got != strings.Repeat("界", 2000)+"…[已截断]" {
		t.Fatalf("rune truncation: %d", len([]rune(got)))
	}
	if got := DiagnosticText("a\x00\x01b\n\tc"); got != "ab\n\tc" {
		t.Fatalf("control filtering: %q", got)
	}
	if diagnosticRef("") != "" || diagnosticRef("valid.id:1-a_b") != "valid.id:1-a_b" {
		t.Fatal("safe identifiers changed")
	}
	sum := sha256.Sum256([]byte("unsafe secret"))
	if got := diagnosticRef("unsafe secret"); got != "ref-"+hex.EncodeToString(sum[:8]) {
		t.Fatalf("opaque identifier: %q", got)
	}
}
