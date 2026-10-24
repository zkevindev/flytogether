package room

import (
	"context"
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func hostCommand(r *Room, actor, action, target string) (View, error) {
	s := roomStateForHost(r)
	return r.DoAs(Command{ID: game.ID(), GameID: s.ID, Version: s.Version, Action: action, TargetPlayerID: target}, actor)
}
func roomStateForHost(r *Room) game.State {
	var s game.State
	_ = json.Unmarshal(r.Snapshot().State, &s)
	return s
}
func hostRoom(t *testing.T, store *Store) (*Room, func(), func()) {
	t.Helper()
	r, e := store.Create("h", game.Options{Names: []string{"房主", "朋友", "机器人"}, Network: true, AI: true, Props: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Join("g", r.ID, r.SnapshotFor("h").Network.Invite, "朋友"); e != nil {
		t.Fatal(e)
	}
	_, h := r.SubscribeFor("h")
	_, g := r.SubscribeFor("g")
	t.Cleanup(h)
	t.Cleanup(g)
	return r, h, g
}
func TestHostTransferKeepsBoardVoiceAndCommandIdempotency(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	enableDiagnostic(t, s, true)
	r, _, _ := hostRoom(t, s)
	if _, e := r.SetReady("g", true); e != nil {
		t.Fatal(e)
	}
	if _, e := networkCommand(t, r, "h", "start"); e != nil {
		t.Fatal(e)
	}
	voice, e := r.VoiceFor("h")
	if e != nil {
		t.Fatal(e)
	}
	defer voice.Close()
	if e = voice.BeginSpeech(); e != nil {
		t.Fatal(e)
	}
	token := voice.CurrentActionToken()
	before := roomStateForHost(r)
	c := Command{ID: game.ID(), GameID: before.ID, Version: before.Version, Action: "transfer_host", TargetPlayerID: "player-1"}
	out, e := r.DoAs(c, "h")
	if e != nil {
		t.Fatal(e)
	}
	if out.Me.Host || !r.SnapshotFor("g").Me.Host || out.Network.HostPlayerID != "player-1" || out.HostChange.To != "朋友" {
		t.Fatal("wrong role after transfer", out)
	}
	if !reflect.DeepEqual(before, roomStateForHost(r)) || token != voice.CurrentActionToken() {
		t.Fatal("management transfer disturbed board or voice grant")
	}
	seq := out.Seq
	out, e = r.DoAs(c, "h")
	if e != nil || out.Seq != seq {
		t.Fatal("old host retry not idempotent", e)
	}
	if _, e = hostCommand(r, "h", "pause", ""); e == nil {
		t.Fatal("old host kept management rights")
	}
	if _, e = voice.Tool("roll_dice", map[string]string{"action_token": token}); e != nil {
		t.Fatal("seat authority incorrectly tied to host", e)
	}
	if _, e = hostCommand(r, "g", "pause", ""); e != nil {
		t.Fatal("new host cannot pause", e)
	}
	events, e := s.DiagnosticTimeline(context.Background(), before.ID, DiagnosticQuery{Kind: "command", Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, v := range events.Events {
		if v.Action == "transfer_host" {
			n++
			if v.Text == "" {
				t.Fatal("missing host diagnostic")
			}
		}
	}
	if n != 1 {
		t.Fatal("host transfer audit duplicated", n)
	}
}
func TestHostTargetsAndPermissions(t *testing.T) {
	r, h, g := hostRoom(t, NewStore())
	_ = h
	for _, test := range []struct{ actor, action, target, code string }{{"stranger", "transfer_host", "player-1", "NOT_AUTHORIZED"}, {"g", "transfer_host", "player-0", "HOST_ONLY"}, {"h", "transfer_host", "player-0", "INVALID_HOST_TARGET"}, {"h", "transfer_host", "player-2", "INVALID_HOST_TARGET"}, {"h", "transfer_host", "missing", "INVALID_HOST_TARGET"}, {"g", "claim_host", "", "HOST_ONLINE"}} {
		_, e := hostCommand(r, test.actor, test.action, test.target)
		errorCode(t, e, test.code)
	}
	g()
	_, e := hostCommand(r, "h", "transfer_host", "player-1")
	errorCode(t, e, "INVALID_HOST_TARGET")
	_, g = r.SubscribeFor("g")
	defer g()
	before := roomStateForHost(r)
	if _, e = hostCommand(r, "h", "transfer_host", "player-1"); e != nil {
		t.Fatal(e)
	}
	if !r.SnapshotFor("g").Network.Seats[1].Ready || !reflect.DeepEqual(before, roomStateForHost(r)) {
		t.Fatal("waiting transfer changed board or left new host unready")
	}
	if _, e = hostCommand(r, "g", "end", ""); e != nil {
		t.Fatal(e)
	}
	_, e = hostCommand(r, "g", "transfer_host", "player-0")
	errorCode(t, e, "INVALID_TRANSITION")
	single := New(game.Options{Names: []string{"一", "二"}})
	_, e = hostCommand(single, "", "claim_host", "")
	errorCode(t, e, "INVALID_ACTION")
}
func TestHostAbsenceGraceTabsReconnectAndExplicitClaim(t *testing.T) {
	s := NewStore()
	s.SetHostReclaimDelay(30 * time.Second)
	r, h, _ := hostRoom(t, s)
	_, secondTab := r.SubscribeFor("h")
	h()
	if !r.SnapshotFor("g").Network.HostOnline {
		t.Fatal("one closed tab marked host offline")
	}
	_, e := hostCommand(r, "g", "claim_host", "")
	errorCode(t, e, "HOST_ONLINE")
	secondTab()
	view := r.SnapshotFor("g")
	if view.Network.HostClaimAt == nil || view.Network.CanClaimHost || view.Network.HostClaimWaitSeconds < 29 {
		t.Fatal("no reconnect grace", view.Network)
	}
	_, e = hostCommand(r, "g", "claim_host", "")
	errorCode(t, e, "HOST_GRACE_PERIOD")
	_, returning := r.SubscribeFor("h")
	if r.SnapshotFor("g").Network.HostClaimAt != nil {
		t.Fatal("host reconnect did not reset grace")
	}
	returning()
	r.mu.Lock()
	r.hostOfflineSince = time.Now().Add(-31 * time.Second)
	r.mu.Unlock()
	r.refreshHostPresence()
	if !r.SnapshotFor("g").Network.CanClaimHost || r.SnapshotFor("g").Me.Host {
		t.Fatal("claim eligibility must not automatically transfer")
	}
	before := roomStateForHost(r)
	if _, e = hostCommand(r, "g", "claim_host", ""); e != nil {
		t.Fatal(e)
	}
	if !r.SnapshotFor("g").Me.Host || !reflect.DeepEqual(before, roomStateForHost(r)) {
		t.Fatal("claim changed board or role missing")
	}
	_, back := r.SubscribeFor("h")
	defer back()
	if r.SnapshotFor("h").Me.Host || r.SnapshotFor("h").Me.PlayerID != "player-0" {
		t.Fatal("former host reconnect stole role or seat")
	}
}
func TestHostClaimRaceOnlyOneWinner(t *testing.T) {
	s := NewStore()
	r, e := s.Create("h", game.Options{Names: []string{"房主", "朋友甲", "朋友乙"}, Network: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, person := range []string{"a", "b"} {
		if _, e = s.Join(person, r.ID, r.SnapshotFor("h").Network.Invite, person); e != nil {
			t.Fatal(e)
		}
		_, close := r.SubscribeFor(person)
		defer close()
	}
	r.mu.Lock()
	r.hostOfflineSince = time.Now().Add(-2 * time.Minute)
	r.mu.Unlock()
	r.refreshHostPresence()
	state := roomStateForHost(r)
	var wg sync.WaitGroup
	answers := make(chan error, 2)
	for _, actor := range []string{"a", "b"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			_, e := r.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "claim_host"}, actor)
			answers <- e
		}(actor)
	}
	wg.Wait()
	close(answers)
	success := 0
	for e := range answers {
		if e == nil {
			success++
		} else {
			errorCode(t, e, "HOST_ONLINE")
		}
	}
	if success != 1 {
		t.Fatal("multiple claims committed", success)
	}
}
func TestTransferredHostSurvivesMigrationRestartAndFailedSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _, _ := hostRoom(t, s)
	// Existing schema4 databases must open without losing their rooms before transfer.
	if _, e := s.storage.db.Exec("PRAGMA user_version=4"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s = openTestSaves(t, path)
	r = s.Get("h", r.ID)
	if r == nil {
		t.Fatal("migration lost room")
	}
	_, closeGuest := r.SubscribeFor("g")
	defer closeGuest()
	c := Command{ID: game.ID(), GameID: roomStateForHost(r).ID, Version: roomStateForHost(r).Version, Action: "transfer_host", TargetPlayerID: "player-1"}
	s.storage.db.Exec("PRAGMA query_only=ON")
	_, e := r.DoAs(c, "h")
	errorCode(t, e, "SAVE_UNAVAILABLE")
	if !r.SnapshotFor("h").Me.Host || r.SnapshotFor("h").HostChange != nil {
		t.Fatal("failed save leaked transfer")
	}
	s.storage.db.Exec("PRAGMA query_only=OFF")
	if _, e = r.DoAs(c, "h"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s = openTestSaves(t, path)
	r = s.Get("g", r.ID)
	if r == nil || !r.SnapshotFor("g").Me.Host || r.SnapshotFor("g").Network.HostPlayerID != "player-1" || r.SnapshotFor("h").Me.Host {
		t.Fatal("non-first-seat host rejected after restart")
	}
	seq := r.Snapshot().Seq
	if _, e = r.DoAs(c, "h"); e != nil || r.Snapshot().Seq != seq {
		t.Fatal("transfer retry after restart changed roles", e)
	}
	if r.SnapshotFor("g").Network.HostClaimAt != nil {
		t.Fatal("restart reused old absence timer")
	}
}
