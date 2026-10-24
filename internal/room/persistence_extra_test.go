package room

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func openTestStore(t *testing.T, retention time.Duration) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "games.db"), retention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestOpenStoreRejectsInvalidLocations(t *testing.T) {
	if _, err := OpenStore("", time.Hour); err == nil {
		t.Fatal("empty save path accepted")
	}
	if _, err := OpenStore(t.TempDir(), time.Hour); err == nil {
		t.Fatal("directory save path accepted")
	}
	store := openTestStore(t, 0)
	if store.retention != DefaultRetention {
		t.Fatalf("zero retention not defaulted: %v", store.retention)
	}
}

func TestValidRecordRejectsEveryMalformedField(t *testing.T) {
	store := openTestStore(t, time.Hour)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	host := r.SnapshotFor("host")
	if _, err = store.Watch("fan", r.ID, host.Audience.Token, "粉丝"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PostChat("fan", r.ID, ChatInput{ID: "chat-1", Kind: "emoji", Emoji: "👏"}); err != nil {
		t.Fatal(err)
	}
	base := r.record()
	if !validRecord(base) {
		t.Fatal("record produced by the store is invalid")
	}

	cases := map[string]func(*roomRecord){
		"schema":              func(p *roomRecord) { p.Schema++ },
		"room id":             func(p *roomRecord) { p.ID = "" },
		"owner":               func(p *roomRecord) { p.Owner = "" },
		"state":               func(p *roomRecord) { p.State = nil },
		"too few names":       func(p *roomRecord) { p.Options.Names = p.Options.Names[:1] },
		"too many names":      func(p *roomRecord) { p.Options.Names = []string{"一", "二", "三", "四", "五"} },
		"unknown status":      func(p *roomRecord) { p.Status = "archived" },
		"invalid game state":  func(p *roomRecord) { p.State.ID = "" },
		"names/player count":  func(p *roomRecord) { p.Options.Names = append(p.Options.Names, "丙") },
		"negative seq":        func(p *roomRecord) { p.Seq = -1 },
		"missing updated":     func(p *roomRecord) { p.Updated = time.Time{} },
		"missing saved time":  func(p *roomRecord) { p.SavedAt = time.Time{} },
		"owner not a member":  func(p *roomRecord) { delete(p.Members, "host") },
		"owner seat range":    func(p *roomRecord) { p.Members["host"] = savedMember{Index: 9, Ready: true} },
		"owner is a bot":      func(p *roomRecord) { p.State.Players[0].AI = true },
		"member seat range":   func(p *roomRecord) { p.Members["ghost"] = savedMember{Index: 9} },
		"duplicate seat":      func(p *roomRecord) { p.Members["ghost"] = savedMember{Index: 0} },
		"member without name": func(p *roomRecord) { delete(p.Names, "host") },
		"delegation reason": func(p *roomRecord) {
			p.Members["host"] = savedMember{Index: 0, Delegated: true, DelegationReason: "because"}
		},
		"delegation version": func(p *roomRecord) {
			p.Members["host"] = savedMember{Index: 0, Delegated: true, DelegationReason: "manual", DelegationVersion: -1}
		},
		"delegation too new": func(p *roomRecord) {
			p.Members["host"] = savedMember{Index: 0, Delegated: true, DelegationReason: "offline", DelegationVersion: p.State.Version + 1}
		},
		"reason without delegation":  func(p *roomRecord) { p.Members["host"] = savedMember{Index: 0, DelegationReason: "manual"} },
		"version without delegation": func(p *roomRecord) { p.Members["host"] = savedMember{Index: 0, DelegationVersion: 2} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rec := base
			rec.Members = map[string]savedMember{}
			for key, m := range base.Members {
				rec.Members[key] = m
			}
			rec.Names = map[string]string{}
			for key, m := range base.Names {
				rec.Names[key] = m
			}
			rec.Options.Names = append([]string(nil), base.Options.Names...)
			state := *base.State
			state.Players = append([]game.Player(nil), base.State.Players...)
			rec.State = &state
			mutate(&rec)
			if validRecord(rec) {
				t.Fatalf("accepted malformed record: %s", name)
			}
		})
	}
}

func TestReadRecordSanitisesWatchersAndChat(t *testing.T) {
	store := openTestStore(t, time.Hour)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	rec := r.record()
	rec.Spectators = map[string]savedSpectator{
		"fan":       {Name: "粉丝", PublicID: "wdeadbeef0001", Blocked: []string{"player-0", "not-an-id"}},
		"host":      {Name: "房主", PublicID: "wdeadbeef0002"},
		"":          {Name: "空键", PublicID: "wdeadbeef0003"},
		"blank":     {Name: "", PublicID: "wdeadbeef0004"},
		"no-public": {Name: "无编号"},
	}
	rec.Chat = []ChatMessage{
		{ID: "ok", FromID: "player-0", Name: "房主", Role: "host", Kind: "text", Text: "加油"},
		{ID: "bad-id", FromID: "hax", Name: "房主", Role: "host", Kind: "text", Text: "加油"},
		{ID: "bad-emoji", FromID: "player-0", Name: "房主", Role: "host", Kind: "emoji", Emoji: "🍕"},
	}
	rec.Members = map[string]savedMember{"host": {Index: 0, Ready: true, Delegated: true, DelegationReason: "manual"}}
	fresh := &Room{}
	fresh.readRecord(rec)
	if len(fresh.spectators) != 1 {
		t.Fatalf("watchers not sanitised: %+v", fresh.spectators)
	}
	if got := fresh.spectators["fan"]; got.Name != "粉丝" || len(got.Blocked) != 1 || got.Blocked[0] != "player-0" {
		t.Fatalf("spectator sanitisation: %+v", got)
	}
	if len(fresh.chat) != 1 || fresh.chat[0].ID != "ok" {
		t.Fatalf("chat not sanitised: %+v", fresh.chat)
	}
	// Old records without a delegation version inherit the current game version.
	if m := fresh.members["host"]; m.delegationVersion != rec.State.Version {
		t.Fatalf("delegation version not upgraded: %d", m.delegationVersion)
	}
	// Voice settings are copied verbatim; the loader repairs defaults on restore.
	rec.VoiceSettings = VoiceSettings{WakeEnabled: true, WakeWord: "小灵同学"}
	fresh.readRecord(rec)
	if !fresh.voiceSettings.WakeEnabled || fresh.voiceSettings.WakeWord != "小灵同学" {
		t.Fatalf("voice settings not restored: %+v", fresh.voiceSettings)
	}
}

func TestLoadQuarantinesCorruptAndDropsExpiredRecords(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}})
	if err != nil {
		t.Fatal(err)
	}
	// Expire one real record and add one corrupt payload with a bad checksum.
	rec := r.record()
	rec.Updated = time.Now().Add(-48 * time.Hour)
	if err = store.storage.save(rec, nil, saveChange{}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.storage.db.Exec("INSERT INTO rooms(id,payload,checksum) VALUES('corrupt','{\"schema\":1}','deadbeef')"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(filepath.Join(dir, "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.rooms) != 0 {
		t.Fatalf("expired or corrupt rooms survived load: %d", len(reopened.rooms))
	}
	var quarantined int
	if err = reopened.storage.db.QueryRow("SELECT COUNT(*) FROM quarantine").Scan(&quarantined); err != nil || quarantined != 1 {
		t.Fatalf("quarantine rows: %d %v", quarantined, err)
	}
}

func TestDiscardGuardsAndSaveFailureRollback(t *testing.T) {
	store := openTestStore(t, time.Hour)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Discard("stranger", r.ID); err == nil || err.(*Error).Code != "HOST_ONLY" {
		t.Fatalf("stranger discarded the room: %v", err)
	}
	if err = store.Discard("host", "missing"); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatalf("missing room discard: %v", err)
	}
	if err = store.Discard("host", r.ID); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if store.Get("host", r.ID) != nil {
		t.Fatal("discarded room still reachable")
	}

	// A broken database must surface as SAVE_UNAVAILABLE and roll the mutation back.
	store2 := openTestStore(t, time.Hour)
	r2, err := store2.Create("host", game.Options{Names: []string{"房主", "朋友"}})
	if err != nil {
		t.Fatal(err)
	}
	before := r2.Snapshot()
	var state game.State
	_ = json.Unmarshal(before.State, &state)
	if err = store2.storage.db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = r2.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll"}, "host")
	if err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("save failure not reported: %v", err)
	}
	if after := r2.Snapshot(); after.Status != before.Status || after.Seq != before.Seq {
		t.Fatal("failed save was not rolled back")
	}
	if err = store2.Discard("host", r2.ID); err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("discard with broken storage: %v", err)
	}
}

func TestRunRetiresExpiredRooms(t *testing.T) {
	store := openTestStore(t, time.Hour)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.updated = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		store.Run(done)
	}()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("expired room was never retired")
		case <-time.After(100 * time.Millisecond):
			store.mu.Lock()
			_, still := store.rooms[r.ID]
			store.mu.Unlock()
			if !still {
				close(done)
				<-finished
				var count int
				if err := store.storage.db.QueryRow("SELECT COUNT(*) FROM rooms WHERE id=?", r.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("expired record kept in storage: %d %v", count, err)
				}
				return
			}
		}
	}
}

func TestRollbackHelpersTolerateMissingPoints(t *testing.T) {
	inMemory := New(game.Options{Names: []string{"甲", "乙"}})
	inMemory.restore(nil)
	var none error
	inMemory.rollback(nil, &none)
	if inMemory.beforeMutation() != nil {
		t.Fatal("in-memory room has no durable state to snapshot")
	}

	store := openTestStore(t, time.Hour)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}})
	if err != nil {
		t.Fatal(err)
	}
	point := r.beforeMutation()
	if point == nil {
		t.Fatal("saved room should produce a rollback point")
	}
	r.voice = "listening"
	r.restore(point)
	if r.voice != "disconnected" {
		t.Fatalf("restore did not reset runtime state: %q", r.voice)
	}
	r.voice = "thinking"
	var failing error = &Error{Code: "SAVE_UNAVAILABLE", Message: "save failed"}
	r.rollback(point, &failing)
	if r.voice != "disconnected" {
		t.Fatalf("rollback with an error did not restore state: %q", r.voice)
	}
	r.rollback(point, &none)
	if r.voice != "disconnected" {
		t.Fatal("rollback without an error must leave state untouched")
	}
}

func TestFamilyFailsClosedWithoutStorage(t *testing.T) {
	store := NewStore()
	if _, err := store.Family("host"); err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("family without storage: %v", err)
	}
	if _, err := store.SaveProfile("host", Profile{Name: "小明", Avatar: "bear"}); err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("save profile without storage: %v", err)
	}
	if err := store.DeleteProfile("host", "id"); err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("delete profile without storage: %v", err)
	}
	if err := store.ClearHistory("host"); err == nil || err.(*Error).Code != "SAVE_UNAVAILABLE" {
		t.Fatalf("clear history without storage: %v", err)
	}
	if _, err := store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, ProfileIDs: []string{"missing"}}); err == nil {
		t.Fatal("profile lookup without storage succeeded")
	}
}

func TestFamilyOptionsValidatesSeatsAndProfiles(t *testing.T) {
	store := openTestStore(t, time.Hour)
	first, err := store.SaveProfile("host", Profile{Name: "小明", Avatar: "bear"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveProfile("host", Profile{Name: "小红", Avatar: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	// Too many profile ids for the seats.
	if _, err = store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, ProfileIDs: []string{"a", "b", "c"}}); err == nil {
		t.Fatal("extra profile ids accepted")
	}
	if _, err = store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, Avatars: []string{"bear", "cat", "deer"}}); err == nil {
		t.Fatal("extra avatars accepted")
	}
	// Unknown avatar for a seat.
	if _, err = store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, Avatars: []string{"http://x"}}); err == nil {
		t.Fatal("remote avatar accepted")
	}
	// The same profile cannot take two seats.
	if _, err = store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, ProfileIDs: []string{first.ID, first.ID}}); err == nil || err.(*Error).Code != "INVALID_PROFILE" {
		t.Fatalf("duplicate profile: %v", err)
	}
	// Duplicate names are rejected even when only one profile is used.
	if _, err = store.FamilyOptions("host", game.Options{Names: []string{"同名", "同名"}}); err == nil || err.(*Error).Code != "INVALID_NAME" {
		t.Fatalf("duplicate seat names: %v", err)
	}
	opts, err := store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, ProfileIDs: []string{first.ID, second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Names[0] != "小明" || opts.Names[1] != "小红" || opts.Avatars[0] != "bear" || opts.ProfileIDs[1] != second.ID {
		t.Fatalf("profiles not applied: %+v", opts)
	}
	// Network rooms only let the host seat carry a saved profile.
	network, err := store.FamilyOptions("host", game.Options{Names: []string{"甲", "乙"}, Network: true, ProfileIDs: []string{first.ID, second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if network.ProfileIDs[1] != "" || network.Avatars[1] != "" {
		t.Fatalf("guest seat kept a host profile: %+v", network)
	}
}

func TestSaveProfileLimitsAndNames(t *testing.T) {
	store := openTestStore(t, time.Hour)
	for _, name := range []string{"", strings.Repeat("长", 11), "带\u0007控制"} {
		if _, err := store.SaveProfile("host", Profile{Name: name, Avatar: "bear"}); err == nil || err.(*Error).Code != "INVALID_PROFILE" {
			t.Fatalf("name %q accepted: %v", name, err)
		}
	}
	if _, err := store.SaveProfile("host", Profile{Name: "小明", Avatar: "unknown"}); err == nil {
		t.Fatal("unknown avatar accepted")
	}
	for i := 0; i < 12; i++ {
		if _, err := store.SaveProfile("host", Profile{Name: "选手" + string(rune('A'+i)), Avatar: "bear"}); err != nil {
			t.Fatalf("profile %d: %v", i, err)
		}
	}
	if _, err := store.SaveProfile("host", Profile{Name: "多余", Avatar: "bear"}); err == nil || err.(*Error).Code != "PROFILE_LIMIT" {
		t.Fatalf("profile limit: %v", err)
	}
	if _, err := store.SaveProfile("host", Profile{ID: "missing", Name: "小明", Avatar: "bear"}); err == nil || err.(*Error).Code != "PROFILE_NOT_FOUND" {
		t.Fatalf("update unknown profile: %v", err)
	}
}
