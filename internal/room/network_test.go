package room

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"testing"
)

func roomState(t *testing.T, r *Room) game.State {
	t.Helper()
	var s game.State
	if err := json.Unmarshal(r.Snapshot().State, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func networkCommand(t *testing.T, r *Room, actor, action string) (View, error) {
	t.Helper()
	s := roomState(t, r)
	return r.DoAs(Command{ID: game.ID(), GameID: s.ID, Version: s.Version, Action: action}, actor)
}
func errorCode(t *testing.T, err error, code string) {
	t.Helper()
	if e, ok := err.(*Error); !ok || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestNetworkMembershipLifecycleAndReconnect(t *testing.T) {
	store := NewStore()
	r, _ := store.Create("host", game.Options{Names: []string{"房主", "空位"}, Network: true, Props: true})
	invite := r.SnapshotFor("host").Network.Invite
	if r.SnapshotFor("host").Me.PlayerID != "player-0" {
		t.Fatal("host seat")
	}
	if _, e := store.Join("stranger", r.ID, "wrong", "客人"); e == nil {
		t.Fatal("invalid invite")
	}
	if store.Get("guest", r.ID) != nil {
		t.Fatal("room id is not a credential")
	}
	_, e := networkCommand(t, r, "host", "start")
	errorCode(t, e, "PLAYERS_NOT_READY")
	if _, e = store.Join("guest", r.ID, invite, "客人"); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Join("third", r.ID, invite, "第三位"); e == nil {
		t.Fatal("full room")
	}
	_, closeHost := r.SubscribeFor("host")
	defer closeHost()
	_, closeGuest := r.SubscribeFor("guest")
	if _, e = r.SetReady("guest", true); e != nil {
		t.Fatal(e)
	}
	_, e = networkCommand(t, r, "guest", "start")
	errorCode(t, e, "HOST_ONLY")
	if _, e = networkCommand(t, r, "host", "start"); e != nil {
		t.Fatal(e)
	}
	_, e = networkCommand(t, r, "guest", "roll")
	errorCode(t, e, "NOT_YOUR_TURN")
	before := roomState(t, r)
	c := Command{ID: game.ID(), GameID: before.ID, Version: before.Version, Action: "roll"}
	first, e := r.DoAs(c, "host")
	if e != nil {
		t.Fatal(e)
	}
	retry, e := r.DoAs(c, "host")
	if e != nil || string(first.State) != string(retry.State) {
		t.Fatalf("retry: %v", e)
	}
	_, e = r.DoAs(c, "guest")
	errorCode(t, e, "COMMAND_CONFLICT")
	closeGuest()
	if r.SnapshotFor("host").Network.Seats[1].Online {
		t.Fatal("offline seat still online")
	}
	if !r.SnapshotFor("host").Network.Seats[1].Occupied {
		t.Fatal("seat lost on disconnect")
	}
	if _, e = store.Join("guest", r.ID, invite, ""); e != nil {
		t.Fatal("original cookie must rejoin started room", e)
	}
	if store.Get("guest", "") != r || r.SnapshotFor("guest").Me.PlayerID != "player-1" {
		t.Fatal("reconnect changed identity")
	}
	_, closeGuest2 := r.SubscribeFor("guest")
	defer closeGuest2()
	if !r.SnapshotFor("host").Network.Seats[1].Online {
		t.Fatal("reconnect offline")
	}
	_, e = networkCommand(t, r, "guest", "pause")
	errorCode(t, e, "HOST_ONLY")
	if _, e = networkCommand(t, r, "host", "end"); e != nil {
		t.Fatal(e)
	}
	if _, e = networkCommand(t, r, "host", "replay"); e != nil {
		t.Fatal(e)
	}
	state := roomState(t, r)
	if state.ID == before.ID || state.Players[1].Name != "客人" || r.SnapshotFor("host").Status != "waiting" {
		t.Fatal("replay lost room options/roster")
	}
	for _, p := range state.Players {
		if p.Stats != (game.MatchStats{}) {
			t.Fatal("stats not reset")
		}
	}
	if r.SnapshotFor("guest").Network.Seats[1].Ready {
		t.Fatal("guest must ready again")
	}
}
func TestNetworkVoiceCannotControlAnotherSeat(t *testing.T) {
	st := NewStore()
	r, _ := st.Create("h", game.Options{Names: []string{"甲", "乙"}, Network: true, Demo: true})
	_, _ = st.Join("g", r.ID, r.SnapshotFor("h").Network.Invite, "客")
	_, ch := r.SubscribeFor("h")
	defer ch()
	_, cg := r.SubscribeFor("g")
	defer cg()
	_, _ = r.SetReady("g", true)
	if _, e := networkCommand(t, r, "h", "start"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.VoiceFor("g"); e == nil {
		t.Fatal("guest voice on host turn")
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
	if _, e = voice.Tool("roll_dice", map[string]string{"action_token": token}); e != nil {
		t.Fatal(e)
	}
	r.mu.Lock()
	r.state.Current = 1
	r.voiceLease = ""
	r.busy = false
	r.mu.Unlock()
	guest, e := r.VoiceFor("g")
	if e != nil {
		t.Fatal(e)
	}
	defer guest.Close()
	if e = guest.BeginSpeech(); e != nil {
		t.Fatal(e)
	}
	voice.Close()
	if guest.CurrentActionToken() == "" {
		t.Fatal("stale socket cleared new voice grant")
	}
	_, e = voice.Tool("roll_dice", map[string]string{"action_token": guest.CurrentActionToken()})
	errorCode(t, e, "NOT_YOUR_TURN")
	if e = voice.BeginSpeech(); e == nil {
		t.Fatal("old socket acquired another seat grant")
	}
}
func TestNetworkLeaveAndNewRoomIsolation(t *testing.T) {
	st := NewStore()
	r, _ := st.Create("h", game.Options{Names: []string{"甲", "乙"}, Network: true})
	token := r.SnapshotFor("h").Network.Invite
	_, _ = st.Join("g", r.ID, token, "客")
	if e := st.Leave("g", r.ID); e != nil {
		t.Fatal(e)
	}
	if st.Get("g", r.ID) != nil {
		t.Fatal("left member authorized")
	}
	next, e := st.Create("g", game.Options{Names: []string{"一", "二"}})
	if e != nil || next == r {
		t.Fatal("left session stuck")
	}
	_, _ = st.Join("g", r.ID, token, "客")
	if _, e = st.CreateNew("h", game.Options{Names: []string{"新", "二"}}); e != nil {
		t.Fatal(e)
	}
	if st.Get("g", r.ID) != r || r.Snapshot().Status != "waiting" {
		t.Fatal("creating another room destroyed shared room")
	}
}

func TestReplayIsIdempotentAndPreservesSameScreenConfig(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "classic", Board: "long", Difficulty: "hard", Props: true, Demo: true})
	r.mu.Lock()
	r.state.Players[0].Stats.Rolls = 30
	r.status = "ended"
	r.mu.Unlock()
	s := roomState(t, r)
	c := Command{ID: game.ID(), GameID: s.ID, Version: s.Version, Action: "replay"}
	first, e := r.Do(c, false)
	if e != nil {
		t.Fatal(e)
	}
	second, e := r.Do(c, false)
	if e != nil || string(first.State) != string(second.State) {
		t.Fatal("duplicate replay created another match", e)
	}
	after := roomState(t, r)
	if after.ID == s.ID || after.Mode != "classic" || after.Board.ID != "long" || after.Difficulty != "hard" || after.Demo || !after.PropsEnabled || after.Players[0].Stats.Rolls != 0 {
		t.Fatal("replay options or reset")
	}
}
