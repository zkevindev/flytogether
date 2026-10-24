package room

import (
	"context"
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func delegationRoom(t *testing.T, s *Store) (*Room, func(), func()) {
	t.Helper()
	r, h, g := hostRoom(t, s)
	if _, e := r.SetReady("g", true); e != nil {
		t.Fatal(e)
	}
	if _, e := networkCommand(t, r, "h", "start"); e != nil {
		t.Fatal(e)
	}
	return r, h, g
}
func delegateCommand(t *testing.T, r *Room, actor, action string) Command {
	t.Helper()
	s := roomStateForHost(r)
	c := Command{ID: game.ID(), GameID: s.ID, Version: s.Version, Action: action}
	if _, e := r.DoAs(c, actor); e != nil {
		t.Fatal(action, e)
	}
	return c
}
func TestDelegationContinuesExistingDieAndRevokesOldVoice(t *testing.T) {
	r, _, _ := delegationRoom(t, NewStore())
	r.mu.Lock()
	r.state.Players[0].Pieces[0].Progress = 12
	r.state.Players[0].Pieces[1].Progress = 35
	if e := r.state.Roll(3); e != nil {
		t.Fatal(e)
	}
	r.mu.Unlock()
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
	delegateCommand(t, r, "h", "delegate")
	now := roomStateForHost(r)
	if now.Dice != before.Dice || now.Phase != "move" || !reflect.DeepEqual(now.Legal, before.Legal) || !reflect.DeepEqual(now.Players, before.Players) {
		t.Fatal("handover changed existing dice/moves or player identity")
	}
	if !r.SnapshotFor("g").Network.Seats[0].Delegated || now.Players[0].AI {
		t.Fatal("not a human delegation")
	}
	if _, e = r.VoiceFor("h"); e == nil {
		t.Fatal("delegated seat opened microphone")
	}
	if _, e = voice.Tool("move_piece", map[string]string{"action_token": token, "piece_id": before.Legal[0].PieceID}); e == nil {
		t.Fatal("late tool executed")
	}
	r.AI()
	after := roomStateForHost(r)
	if after.Players[0].Stats.Rolls != before.Players[0].Stats.Rolls || after.Players[0].Stats.PropsUsed != 0 || reflect.DeepEqual(after.Players[0].Pieces, before.Players[0].Pieces) {
		t.Fatal("autopilot rerolled/used card/did not move")
	}
	if !strings.Contains(after.History[len(after.History)-1].Text, "【托管】") {
		t.Fatal("unidentified delegated action")
	}
	delegateCommand(t, r, "h", "reclaim")
	stable := roomStateForHost(r)
	r.AI()
	if !reflect.DeepEqual(stable, roomStateForHost(r)) {
		t.Fatal("scheduler acted after reclaim")
	}
	if _, e = voice.Tool("get_game_state", nil); e == nil {
		t.Fatal("old lease read new speech grant")
	}
}
func TestDelegationDefaultsGraceReconnectAndAllOffline(t *testing.T) {
	s := NewStore()
	s.SetDelegationDelay(5 * time.Second)
	r, h, g := delegationRoom(t, s)
	h()
	r.refreshDelegationAt(time.Now().Add(time.Hour))
	if r.SnapshotFor("g").Network.Seats[0].Delegated {
		t.Fatal("default offline takeover")
	}
	_, h = r.SubscribeFor("h")
	delegateCommand(t, r, "h", "auto_delegate_on")
	_, tab := r.SubscribeFor("h")
	h()
	r.refreshDelegationAt(time.Now().Add(time.Hour))
	if r.SnapshotFor("g").Network.Seats[0].DelegateWaitSeconds != 0 {
		t.Fatal("one tab still online")
	}
	tab()
	r.mu.Lock()
	start := r.members["h"].offlineSince
	r.mu.Unlock()
	if start.IsZero() {
		t.Fatal("grace not started")
	}
	r.refreshDelegationAt(start.Add(5*time.Second - time.Millisecond))
	if r.SnapshotFor("g").Network.Seats[0].Delegated {
		t.Fatal("early takeover")
	}
	_, h = r.SubscribeFor("h")
	r.refreshDelegationAt(start.Add(time.Hour))
	if r.SnapshotFor("g").Network.Seats[0].Delegated {
		t.Fatal("reconnect did not cancel grace")
	}
	h()
	r.mu.Lock()
	start = r.members["h"].offlineSince
	r.mu.Unlock()
	g()
	r.refreshDelegationAt(start.Add(time.Hour))
	if r.SnapshotFor("g").Network.Seats[0].Delegated {
		t.Fatal("all offline still delegates")
	}
	_, g = r.SubscribeFor("g")
	defer g()
	r.mu.Lock()
	start = r.members["h"].offlineSince
	r.mu.Unlock()
	r.refreshDelegationAt(start.Add(5 * time.Second))
	seat := r.SnapshotFor("g").Network.Seats[0]
	if !seat.Delegated || seat.DelegationReason != "offline" {
		t.Fatal("opted-in offline seat not delegated")
	}
	_, h = r.SubscribeFor("h")
	defer h()
	if !r.SnapshotFor("h").Network.Seats[0].Delegated {
		t.Fatal("reconnect silently gave control back")
	}
	delegateCommand(t, r, "h", "auto_delegate_off")
	if !r.SnapshotFor("h").Network.Seats[0].Delegated {
		t.Fatal("turning off future consent changed present control")
	}
	delegateCommand(t, r, "h", "reclaim")
}
func TestDelegationPermissionsPauseReplayAndNoCards(t *testing.T) {
	r, _, _ := delegationRoom(t, NewStore())
	for _, c := range []Command{{Action: "delegate", TargetPlayerID: "player-1"}, {Action: "delegate", PieceID: "p1-0"}} {
		s := roomStateForHost(r)
		c.ID = game.ID()
		c.GameID = s.ID
		c.Version = s.Version
		if _, e := r.DoAs(c, "h"); e == nil {
			t.Fatal("targeted someone else")
		}
	}
	if _, e := hostCommand(r, "stranger", "delegate", ""); e == nil {
		t.Fatal("stranger")
	}
	delegateCommand(t, r, "g", "delegate")
	delegateCommand(t, r, "h", "delegate")
	props := roomStateForHost(r).Players[0].Props
	r.AI()
	if !reflect.DeepEqual(props, roomStateForHost(r).Players[0].Props) {
		t.Fatal("AI spent human props")
	}
	delegateCommand(t, r, "h", "auto_delegate_on")
	delegateCommand(t, r, "h", "pause")
	before := roomStateForHost(r)
	r.AI()
	r.refreshDelegationAt(time.Now().Add(time.Hour))
	if !reflect.DeepEqual(before, roomStateForHost(r)) {
		t.Fatal("actions while paused")
	}
	delegateCommand(t, r, "h", "resume")
	delegateCommand(t, r, "h", "end")
	if _, e := hostCommand(r, "h", "reclaim", ""); e == nil {
		t.Fatal("reclaim ended game")
	}
	delegateCommand(t, r, "h", "replay")
	for _, seat := range r.SnapshotFor("h").Network.Seats {
		if seat.Delegated || seat.AutoDelegate {
			t.Fatal("new game retained delegation/consent")
		}
	}
	single := New(game.Options{Names: []string{"a", "b"}})
	if _, e := hostCommand(single, "", "delegate", ""); e == nil {
		t.Fatal("same-screen delegated")
	}
}
func TestDelegationPersistenceFailureAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	enableDiagnostic(t, s, true)
	r, _, _ := delegationRoom(t, s)
	delegateCommand(t, r, "h", "auto_delegate_on")
	before := r.SnapshotFor("h")
	_, _ = s.storage.db.Exec("PRAGMA query_only=ON")
	if _, e := hostCommand(r, "h", "delegate", ""); e == nil {
		t.Fatal("no disk commit")
	}
	if string(before.State) != string(r.Snapshot().State) || r.SnapshotFor("h").Network.Seats[0].Delegated {
		t.Fatal("failed delegation did not roll back")
	}
	_, _ = s.storage.db.Exec("PRAGMA query_only=OFF")
	c := delegateCommand(t, r, "h", "delegate")
	saved := r.SnapshotFor("h")
	s.Close()
	next := openTestSaves(t, path)
	enableDiagnostic(t, next, true)
	rr := next.Get("h", r.ID)
	if rr == nil {
		t.Fatal("restore lost membership")
	}
	if rr.Snapshot().Status != "paused" || !rr.SnapshotFor("h").Network.Seats[0].Delegated || !rr.SnapshotFor("h").Network.Seats[0].AutoDelegate {
		t.Fatal("control/consent not restored safely")
	}
	rr.AI()
	if string(rr.Snapshot().State) != string(saved.State) {
		t.Fatal("restart secretly acted")
	}
	if _, e := rr.DoAs(c, "h"); e != nil || string(rr.Snapshot().State) != string(saved.State) {
		t.Fatal("retry changed control or board", e)
	}
	_, h := rr.SubscribeFor("h")
	defer h()
	delegateCommand(t, rr, "h", "reclaim")
	delegateCommand(t, rr, "h", "resume")
	events, e := next.DiagnosticTimeline(context.Background(), roomStateForHost(rr).ID, DiagnosticQuery{Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, event := range events.Events {
		found = found || event.Kind == "game" && strings.Contains(event.Text, "托管")
	}
	if !found {
		t.Fatal("delegation absent from diagnostics")
	}
}
func TestDelegationReclaimRacesSchedulerWithoutDoubleRoll(t *testing.T) {
	for n := 0; n < 20; n++ {
		r, _, _ := delegationRoom(t, NewStore())
		delegateCommand(t, r, "h", "delegate")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); r.AI() }()
		go func() { defer wg.Done(); _, _ = hostCommand(r, "h", "reclaim", "") }()
		wg.Wait()
		if r.SnapshotFor("h").Network.Seats[0].Delegated {
			delegateCommand(t, r, "h", "reclaim")
		}
		stable := roomStateForHost(r)
		if stable.Players[0].Stats.Rolls > 1 {
			t.Fatal("race rolled twice")
		}
		r.AI()
		if !reflect.DeepEqual(stable, roomStateForHost(r)) {
			t.Fatal("scheduler acted after reclaim")
		}
	}
}
func TestSchema5UpgradeDefaultsDelegationOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _, _ := delegationRoom(t, s)
	id := r.ID
	if _, e := s.storage.db.Exec("PRAGMA user_version=5"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	next := openTestSaves(t, path)
	restored := next.Get("h", id)
	if restored == nil {
		t.Fatal("old save quarantined")
	}
	var version int
	_ = next.storage.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 6 {
		t.Fatal("schema not upgraded")
	}
	b, _ := json.Marshal(restored.SnapshotFor("h").Network.Seats)
	if strings.Contains(string(b), "delegated") {
		t.Fatal("old save opted into automation")
	}
}

func TestDelegationFailedAutomaticCommitPreservesGrace(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	s.SetDelegationDelay(5 * time.Second)
	r, h, _ := delegationRoom(t, s)
	delegateCommand(t, r, "h", "auto_delegate_on")
	h()
	r.mu.Lock()
	start := r.members["h"].offlineSince
	r.mu.Unlock()
	before := r.Snapshot()
	_, _ = s.storage.db.Exec("PRAGMA query_only=ON")
	r.refreshDelegationAt(start.Add(6 * time.Second))
	if r.SnapshotFor("g").Network.Seats[0].Delegated || string(r.Snapshot().State) != string(before.State) {
		t.Fatal("uncommitted offline control escaped")
	}
	r.mu.Lock()
	kept := r.members["h"].offlineSince
	r.mu.Unlock()
	if !kept.Equal(start) {
		t.Fatal("rollback restarted grace")
	}
	_, _ = s.storage.db.Exec("PRAGMA query_only=OFF")
	r.refreshDelegationAt(start.Add(6 * time.Second))
	if !r.SnapshotFor("g").Network.Seats[0].Delegated {
		t.Fatal("commit did not recover")
	}
}
func TestDelegationRejectsOldClickAndReorderedConsent(t *testing.T) {
	r, _, _ := delegationRoom(t, NewStore())
	old := roomStateForHost(r)
	click := Command{ID: game.ID(), GameID: old.ID, Version: old.Version, Action: "roll"}
	delegateCommand(t, r, "h", "delegate")
	delegateCommand(t, r, "h", "reclaim")
	if _, e := r.DoAs(click, "h"); e == nil {
		t.Fatal("old pre-delegation click accepted after reclaim")
	}
	old = roomStateForHost(r)
	consent := Command{ID: game.ID(), GameID: old.ID, Version: old.Version, Action: "auto_delegate_on"}
	delegateCommand(t, r, "h", "auto_delegate_on")
	delegateCommand(t, r, "h", "auto_delegate_off")
	if _, e := r.DoAs(consent, "h"); e == nil {
		t.Fatal("delayed old consent re-enabled delegation")
	}
	if r.SnapshotFor("h").Network.Seats[0].AutoDelegate {
		t.Fatal("consent reversed")
	}
}
func TestDelegatedHumansFinishNaturalGameWithoutSpendingProps(t *testing.T) {
	r, _, _ := delegationRoom(t, NewStore())
	before := roomStateForHost(r)
	delegateCommand(t, r, "h", "delegate")
	delegateCommand(t, r, "g", "delegate")
	for i := 0; i < 10000 && roomStateForHost(r).Phase != "finished"; i++ {
		r.AI()
	}
	after := roomStateForHost(r)
	if after.Phase != "finished" {
		t.Fatal("delegated game never finished")
	}
	for i := 0; i < 2; i++ {
		if !reflect.DeepEqual(before.Players[i].Props, after.Players[i].Props) || after.Players[i].Stats.PropsUsed != 0 || after.Players[i].AI {
			t.Fatal("human inventory or identity changed")
		}
	}
}
