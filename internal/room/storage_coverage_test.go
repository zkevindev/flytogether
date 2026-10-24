package room

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func storageSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("SQL %s: %v", query, err)
	}
}

// Compare the actual durable bytes, not just the room's in-memory rollback.
func storageDurableRows(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range []string{"rooms", "commands", "sessions", "quarantine", "family_history"} {
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1,2")
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			data, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			out[table] = append(out[table], string(data))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func storageRoom(t *testing.T, s *Store, owner string) *Room {
	t.Helper()
	r, err := s.Create(owner, game.Options{Names: []string{"甲", "乙"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStorageCoverageSaveTransactionFaults(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		change      saveChange
	}{
		{"delete_replaced_room", "BEFORE DELETE ON rooms", saveChange{DeleteRoom: "old"}},
		{"upsert_room", "BEFORE INSERT ON rooms", saveChange{DeleteRoom: "old"}},
		{"reset_commands", "BEFORE DELETE ON commands", saveChange{DeleteRoom: "old", ResetCommands: true}},
		{"insert_command", "BEFORE INSERT ON commands", saveChange{DeleteRoom: "old", ResetCommands: true}},
		{"link_session", "BEFORE INSERT ON sessions", saveChange{DeleteRoom: "old", ResetCommands: true, Session: "host"}},
		{"forget_session", "BEFORE DELETE ON sessions WHEN OLD.id='host'", saveChange{ResetCommands: true, Session: "host", Forget: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t, time.Hour)
			r := storageRoom(t, s, "host")
			old := storageRoom(t, s, "other")
			p := r.record()
			prior := Command{ID: "prior", Actor: "host", Action: "roll"}
			if err := s.storage.save(p, &prior, saveChange{}); err != nil {
				t.Fatal(err)
			}
			change := tc.change
			if change.DeleteRoom != "" {
				change.DeleteRoom = old.ID
			}
			before := storageDurableRows(t, s.storage.db)
			storageSQL(t, s.storage.db, "CREATE TRIGGER fault "+tc.event+" BEGIN SELECT RAISE(ABORT,'targeted storage fault'); END")
			p.Name = "only committed on retry"
			cmd := Command{ID: "retry", Actor: "host", Action: "roll"}
			if err := s.storage.save(p, &cmd, change); err == nil || !strings.Contains(err.Error(), "targeted storage fault") {
				t.Fatalf("injected failure missing: %v", err)
			}
			if after := storageDurableRows(t, s.storage.db); !reflect.DeepEqual(before, after) {
				t.Fatalf("partial transaction persisted\nbefore=%v\nafter=%v", before, after)
			}
			storageSQL(t, s.storage.db, "DROP TRIGGER fault")
			if err := s.storage.save(p, &cmd, change); err != nil {
				t.Fatalf("retry after recovery: %v", err)
			}
			var payload []byte
			if err := s.storage.db.QueryRow("SELECT payload FROM rooms WHERE id=?", r.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var saved roomRecord
			if err := json.Unmarshal(payload, &saved); err != nil || saved.Name != p.Name {
				t.Fatalf("retry room not saved: %s %v", payload, err)
			}
			var count int
			if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM commands WHERE room_id=? AND id='retry'", r.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("retry receipt missing: %d %v", count, err)
			}
			if change.ResetCommands {
				if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM commands WHERE room_id=? AND id='prior'", r.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("old command not reset: %d %v", count, err)
				}
			}
			if change.Forget {
				if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id='host'").Scan(&count); err != nil || count != 0 {
					t.Fatalf("session not forgotten: %d %v", count, err)
				}
			}
			if change.DeleteRoom != "" {
				if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM rooms WHERE id=?", old.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("replaced room survived retry: %d %v", count, err)
				}
			}
		})
	}
}

// Deferred foreign keys fail at COMMIT, after all statements have succeeded.
func TestStorageCoverageDeferredCommitFailure(t *testing.T) {
	s := openTestStore(t, time.Hour)
	r := storageRoom(t, s, "host")
	p := familyProfile(t, s, "host", "原名", "bear")
	storageSQL(t, s.storage.db, "CREATE TABLE commit_parent(id TEXT PRIMARY KEY)")
	storageSQL(t, s.storage.db, "CREATE TABLE commit_child(id TEXT REFERENCES commit_parent(id) DEFERRABLE INITIALLY DEFERRED)")
	for _, target := range []string{"room", "profile"} {
		t.Run(target, func(t *testing.T) {
			before := storageDurableRows(t, s.storage.db)
			event := "AFTER INSERT ON commands"
			if target == "profile" {
				event = "AFTER UPDATE ON family_profiles"
			}
			storageSQL(t, s.storage.db, "CREATE TRIGGER fault "+event+" BEGIN INSERT INTO commit_child VALUES('absent'); END")
			if target == "room" {
				rec := r.record()
				rec.Name = "must roll back at commit"
				if err := s.storage.save(rec, &Command{ID: "commit-fault", Actor: "host"}, saveChange{}); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
					t.Fatalf("deferred commit error not returned: %v", err)
				}
			} else {
				_, err := s.SaveProfile("host", Profile{ID: p.ID, Name: "未提交", Avatar: "cat"})
				errorCode(t, err, "SAVE_UNAVAILABLE")
				if got, err := s.profile("host", p.ID); err != nil || got != p {
					t.Fatalf("failed commit changed profile: %+v %v", got, err)
				}
			}
			if !reflect.DeepEqual(before, storageDurableRows(t, s.storage.db)) {
				t.Fatal("commit failure left partial durable changes")
			}
			var count int
			if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM commit_child").Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed commit left trigger writes: %d %v", count, err)
			}
			storageSQL(t, s.storage.db, "DROP TRIGGER fault")
		})
	}
	if _, err := s.SaveProfile("host", Profile{ID: p.ID, Name: "恢复", Avatar: "cat"}); err != nil {
		t.Fatalf("connection still broken after commit rollback: %v", err)
	}
}

func TestStorageCoverageQuarantineIsAtomic(t *testing.T) {
	for _, event := range []string{"BEFORE INSERT ON quarantine", "BEFORE DELETE ON rooms"} {
		t.Run(event, func(t *testing.T) {
			s := openTestStore(t, time.Hour)
			r := storageRoom(t, s, "host")
			bad := []byte(`{"broken":`)
			storageSQL(t, s.storage.db, "UPDATE rooms SET payload=? WHERE id=?", bad, r.ID)
			before := storageDurableRows(t, s.storage.db)
			storageSQL(t, s.storage.db, "CREATE TRIGGER fault "+event+" BEGIN SELECT RAISE(ABORT,'quarantine fault'); END")
			fresh := NewStore()
			fresh.storage, fresh.retention = s.storage, time.Hour
			if err := fresh.load(); err == nil || !strings.Contains(err.Error(), "quarantine fault") {
				t.Fatalf("quarantine failure hidden: %v", err)
			}
			if len(fresh.rooms) != 0 || !reflect.DeepEqual(before, storageDurableRows(t, s.storage.db)) {
				t.Fatal("failed quarantine deleted data or published corrupt room")
			}
			storageSQL(t, s.storage.db, "DROP TRIGGER fault")
			if err := fresh.load(); err != nil {
				t.Fatal(err)
			}
			var payload []byte
			if err := s.storage.db.QueryRow("SELECT payload FROM quarantine WHERE room_id=?", r.ID).Scan(&payload); err != nil || !bytes.Equal(payload, bad) {
				t.Fatalf("quarantine did not preserve exact corrupt evidence: %q %v", payload, err)
			}
			var count int
			if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE room_id=?", r.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("corrupt room session not cascaded: %d %v", count, err)
			}
		})
	}
}

func TestStorageCoverageBadCommandsAreIsolatedOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r := storageRoom(t, s, "host")
	valid := savedDo(t, r, "host", "roll", nil)
	cases := map[string][]byte{
		"invalid-json": []byte(`{`),
		"wrong-id":     []byte(`{"input":{"id":"different"},"actor":"host"}`),
		"voice-token":  []byte(`{"input":{"id":"voice-token","token":"stale-grant"},"actor":"host"}`),
	}
	for id, data := range cases {
		storageSQL(t, s.storage.db, "INSERT INTO commands(room_id,id,payload) VALUES(?,?,?)", r.ID, id, data)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next := openTestSaves(t, path)
	restored := next.Get("host", r.ID)
	if restored == nil {
		t.Fatal("one corrupt command quarantined an otherwise valid room")
	}
	before := restored.Snapshot()
	for id := range cases {
		if !restored.cache[id].Unavailable {
			t.Fatalf("bad command %s not marked unavailable", id)
		}
		_, err := restored.DoAs(Command{ID: id, GameID: current(restored).ID, Version: current(restored).Version, Action: "roll"}, "host")
		errorCode(t, err, "RETRY_UNAVAILABLE")
	}
	if _, err := restored.DoAs(valid, "host"); err != nil {
		t.Fatalf("healthy idempotency receipt lost: %v", err)
	}
	if after := restored.Snapshot(); !bytes.Equal(before.State, after.State) || before.Seq != after.Seq {
		t.Fatal("retries mutated recovered game")
	}
	for id, want := range cases {
		var got []byte
		if err := next.storage.db.QueryRow("SELECT payload FROM commands WHERE room_id=? AND id=?", r.ID, id).Scan(&got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("corrupt command evidence changed: %s %v", got, err)
		}
	}
}

func TestStorageCoverageOpenFailuresPreserveEvidence(t *testing.T) {
	t.Run("not_a_database", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "damaged.db")
		original := []byte("this is not a SQLite database; preserve me\x00")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		if s, err := OpenStore(path, time.Hour); err == nil || s != nil {
			t.Fatalf("corrupt database opened: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(original, after) {
			t.Fatalf("corrupt database was overwritten: %q %v", after, err)
		}
	})
	t.Run("parent_is_file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if s, err := OpenStore(filepath.Join(path, "games.db"), time.Hour); err == nil || s != nil {
			t.Fatalf("invalid parent accepted: %v", err)
		}
	})
	t.Run("migration_transaction", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "legacy.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		storageSQL(t, db, "CREATE TABLE diagnostic_matches (game_id TEXT PRIMARY KEY)")
		storageSQL(t, db, "INSERT INTO diagnostic_matches VALUES('evidence')")
		storageSQL(t, db, "PRAGMA user_version=2")
		if s, err := OpenStore(path, time.Hour); err == nil || s != nil {
			t.Fatalf("malformed legacy schema accepted: %v", err)
		}
		var version, created int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
			t.Fatalf("failed migration changed version: %d %v", version, err)
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='rooms'").Scan(&created); err != nil || created != 0 {
			t.Fatalf("failed migration partially created tables: %d %v", created, err)
		}
		var evidence string
		if err := db.QueryRow("SELECT game_id FROM diagnostic_matches").Scan(&evidence); err != nil || evidence != "evidence" {
			t.Fatalf("migration destroyed legacy data: %q %v", evidence, err)
		}
	})
	t.Run("load_cannot_persist_safe_pause", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "games.db")
		s := openTestSaves(t, path)
		r := storageRoom(t, s, "host")
		before := storageDurableRows(t, s.storage.db)
		storageSQL(t, s.storage.db, "CREATE TRIGGER fault BEFORE UPDATE ON rooms BEGIN SELECT RAISE(ABORT,'pause save fault'); END")
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if next, err := OpenStore(path, time.Hour); err == nil || next != nil || !strings.Contains(err.Error(), "pause save fault") {
			t.Fatalf("unsafe startup succeeded: %v", err)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if !reflect.DeepEqual(before, storageDurableRows(t, db)) {
			t.Fatal("failed startup changed saved board")
		}
		storageSQL(t, db, "DROP TRIGGER fault")
		next := openTestSaves(t, path)
		if restored := next.Get("host", r.ID); restored == nil || restored.Snapshot().Status != "paused" {
			t.Fatal("repaired database did not recover safely paused")
		}
	})
}

func TestStorageCoverageLoadSQLFailures(t *testing.T) {
	for _, tc := range []struct {
		name, setup string
	}{
		{"rooms_query", "DROP TABLE rooms"},
		{"rooms_scan", "CREATE VIEW bad_rooms AS SELECT NULL AS id, payload, checksum FROM rooms; ALTER TABLE rooms RENAME TO original_rooms; CREATE VIEW rooms AS SELECT * FROM bad_rooms"},
		{"commands_query", "DROP TABLE commands"},
		{"commands_scan", "INSERT INTO commands(room_id,id,payload) SELECT id,'bad',x'00' FROM rooms; ALTER TABLE commands RENAME TO original_commands; CREATE VIEW commands AS SELECT room_id,NULL AS id,payload FROM original_commands"},
		{"sessions_query", "DROP TABLE sessions"},
		{"sessions_scan", "ALTER TABLE sessions RENAME TO original_sessions; CREATE VIEW sessions AS SELECT NULL AS id,room_id FROM original_sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t, time.Hour)
			storageRoom(t, s, "host")
			storageSQL(t, s.storage.db, tc.setup)
			fresh := NewStore()
			fresh.storage, fresh.retention = s.storage, time.Hour
			if err := fresh.load(); err == nil {
				t.Fatal("damaged SQL schema silently accepted")
			}
			if len(fresh.sessions) != 0 {
				t.Fatal("failed restore granted session access")
			}
		})
	}
	t.Run("expired_delete", func(t *testing.T) {
		s := openTestStore(t, time.Hour)
		r := storageRoom(t, s, "host")
		p := r.record()
		p.Updated = time.Now().Add(-2 * time.Hour)
		if err := s.storage.save(p, nil, saveChange{}); err != nil {
			t.Fatal(err)
		}
		before := storageDurableRows(t, s.storage.db)
		storageSQL(t, s.storage.db, "CREATE TRIGGER fault BEFORE DELETE ON rooms BEGIN SELECT RAISE(ABORT,'retention fault'); END")
		fresh := NewStore()
		fresh.storage, fresh.retention = s.storage, time.Hour
		if err := fresh.load(); err == nil || !strings.Contains(err.Error(), "retention fault") {
			t.Fatalf("retention failure hidden: %v", err)
		}
		if len(fresh.rooms) != 0 || !reflect.DeepEqual(before, storageDurableRows(t, s.storage.db)) {
			t.Fatal("failed retention modified durable data or restored expired room")
		}
		storageSQL(t, s.storage.db, "DROP TRIGGER fault")
		if err := fresh.load(); err != nil || len(fresh.rooms) != 0 {
			t.Fatalf("retention recovery failed: %v", err)
		}
	})
}

func TestStorageCoverageMalformedRecordsDoNotPoisonHealthyRoom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	healthy := storageRoom(t, s, "host")
	for _, kind := range []string{"json", "checksum", "id", "schema"} {
		p := healthy.record()
		p.ID = "bad-" + kind
		if kind == "id" {
			p.ID = "mismatched-id"
		}
		if kind == "schema" {
			p.Schema++
		}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if kind == "json" {
			data = []byte("{")
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])
		if kind == "checksum" {
			checksum = "invalid"
		}
		storageSQL(t, s.storage.db, "INSERT INTO rooms(id,payload,checksum) VALUES(?,?,?)", "bad-"+kind, data, checksum)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next := openTestSaves(t, path)
	if len(next.rooms) != 1 || next.Get("host", healthy.ID) == nil {
		t.Fatal("invalid records prevented healthy room recovery")
	}
	var count int
	if err := next.storage.db.QueryRow("SELECT COUNT(*) FROM quarantine").Scan(&count); err != nil || count != 4 {
		t.Fatalf("not all distinct corruption classes isolated: %d %v", count, err)
	}
}

func TestStorageCoverageFamilyMutationFaultsRecover(t *testing.T) {
	for _, op := range []string{"insert", "update", "delete", "clear"} {
		t.Run(op, func(t *testing.T) {
			s := openTestStore(t, time.Hour)
			p := familyProfile(t, s, "host", "原名", "bear")
			storageSQL(t, s.storage.db, "INSERT INTO family_history VALUES('host','finished','2026-01-01',?)", []byte(`{"game_id":"finished","players":[]}`))
			var event string
			switch op {
			case "insert":
				event = "BEFORE INSERT ON family_profiles"
			case "update":
				event = "BEFORE UPDATE ON family_profiles"
			case "delete":
				event = "BEFORE DELETE ON family_profiles"
			case "clear":
				event = "BEFORE UPDATE ON family_history"
			}
			storageSQL(t, s.storage.db, "CREATE TRIGGER fault "+event+" BEGIN SELECT RAISE(ABORT,'family fault'); END")
			mutate := func() error {
				switch op {
				case "insert":
					_, err := s.SaveProfile("host", Profile{Name: "新名", Avatar: "cat"})
					return err
				case "update":
					_, err := s.SaveProfile("host", Profile{ID: p.ID, Name: "新名", Avatar: "cat"})
					return err
				case "delete":
					return s.DeleteProfile("host", p.ID)
				default:
					return s.ClearHistory("host")
				}
			}
			before := familyRead(t, s, "host")
			errorCode(t, mutate(), "SAVE_UNAVAILABLE")
			if after := familyRead(t, s, "host"); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed family mutation persisted: %+v", after)
			}
			storageSQL(t, s.storage.db, "DROP TRIGGER fault")
			if err := mutate(); err != nil {
				t.Fatalf("family retry failed: %v", err)
			}
			after := familyRead(t, s, "host")
			switch op {
			case "insert":
				if len(after.Profiles) != 2 {
					t.Fatal("profile creation did not recover")
				}
			case "update":
				if len(after.Profiles) != 1 || after.Profiles[0].Name != "新名" || after.Profiles[0].Avatar != "cat" {
					t.Fatal("profile update did not recover")
				}
			case "delete":
				if len(after.Profiles) != 0 {
					t.Fatal("profile deletion did not recover")
				}
			case "clear":
				var count int
				if err := s.storage.db.QueryRow("SELECT COUNT(*) FROM family_history WHERE session='host' AND payload IS NULL").Scan(&count); err != nil || count != 1 || after.Total != 0 {
					t.Fatalf("history receipt not retained: %d %+v %v", count, after, err)
				}
			}
		})
	}
}

func TestStorageCoverageFamilyCorruptHistoryAndSQLFailures(t *testing.T) {
	t.Run("corrupt_history_is_scoped_and_clearable", func(t *testing.T) {
		s := openTestStore(t, time.Hour)
		p := familyProfile(t, s, "host", "小明", "bear")
		storageSQL(t, s.storage.db, "INSERT INTO family_history VALUES('host','broken','2026-01-01',?)", []byte("{"))
		storageSQL(t, s.storage.db, "INSERT INTO family_history VALUES('other','healthy','2026-01-01',?)", []byte(`{"game_id":"healthy"}`))
		f, err := s.Family("host")
		errorCode(t, err, "SAVE_UNAVAILABLE")
		if len(f.Matches) != 0 || len(f.Profiles) != 1 || f.Profiles[0] != p {
			t.Fatalf("corrupt history returned fabricated match or lost profiles: %+v", f)
		}
		if other := familyRead(t, s, "other"); other.Total != 1 || other.Matches[0].GameID != "healthy" {
			t.Fatal("another family's corrupt history poisoned healthy history")
		}
		var data []byte
		if err := s.storage.db.QueryRow("SELECT payload FROM family_history WHERE session='host'").Scan(&data); err != nil || string(data) != "{" {
			t.Fatalf("read mutated corrupt evidence: %q %v", data, err)
		}
		if err := s.ClearHistory("host"); err != nil {
			t.Fatal(err)
		}
		if repaired := familyRead(t, s, "host"); repaired.Total != 0 || len(repaired.Matches) != 0 {
			t.Fatal("clearing corrupt history did not restore family access")
		}
	})
	for _, tc := range []struct{ name, setup string }{
		{"missing_profiles", "DROP TABLE family_profiles"},
		{"profile_scan", "ALTER TABLE family_profiles RENAME TO original_profiles; CREATE VIEW family_profiles AS SELECT session,id,NULL AS name,avatar FROM original_profiles"},
		{"missing_history", "DROP TABLE family_history"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t, time.Hour)
			p := familyProfile(t, s, "host", "小明", "bear")
			storageSQL(t, s.storage.db, tc.setup)
			_, err := s.Family("host")
			errorCode(t, err, "SAVE_UNAVAILABLE")
			if tc.name == "missing_profiles" {
				_, err = s.SaveProfile("host", Profile{Name: "新名", Avatar: "cat"})
				errorCode(t, err, "SAVE_UNAVAILABLE")
				_, err = s.profile("host", p.ID)
				errorCode(t, err, "SAVE_UNAVAILABLE")
			}
		})
	}
	t.Run("closed_database", func(t *testing.T) {
		s := openTestStore(t, time.Hour)
		p := familyProfile(t, s, "host", "小明", "bear")
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := s.SaveProfile("host", Profile{Name: "新名", Avatar: "cat"})
		errorCode(t, err, "SAVE_UNAVAILABLE")
		_, err = s.profile("host", p.ID)
		errorCode(t, err, "SAVE_UNAVAILABLE")
		_, err = s.Family("host")
		errorCode(t, err, "SAVE_UNAVAILABLE")
		errorCode(t, s.DeleteProfile("host", p.ID), "SAVE_UNAVAILABLE")
		errorCode(t, s.ClearHistory("host"), "SAVE_UNAVAILABLE")
		if err := s.quarantine("lost", []byte("evidence")); err == nil {
			t.Fatal("quarantine accepted a closed database")
		}
	})
}
