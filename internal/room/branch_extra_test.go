package room

import (
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestDelaySettersResetAndPropagate(t *testing.T) {
	store := NewStore()
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	store.SetDelegationDelay(0)
	if r.delegationWait() != DefaultDelegationDelay || store.delegationDelay != DefaultDelegationDelay {
		t.Fatalf("delegation reset: %v", r.delegationWait())
	}
	store.SetDelegationDelay(30 * time.Second)
	if r.delegationWait() != 30*time.Second {
		t.Fatalf("delegation update: %v", r.delegationWait())
	}
	store.SetHostReclaimDelay(0)
	if r.hostDelay() != DefaultHostReclaimDelay || store.hostReclaimDelay != DefaultHostReclaimDelay {
		t.Fatalf("host reset: %v", r.hostDelay())
	}
	store.SetHostReclaimDelay(20 * time.Second)
	if r.hostDelay() != 20*time.Second {
		t.Fatalf("host update: %v", r.hostDelay())
	}
	// Rooms built directly keep their defaults.
	bare := New(game.Options{Names: []string{"甲", "乙"}})
	if bare.delegationWait() != DefaultDelegationDelay || bare.hostDelay() != DefaultHostReclaimDelay {
		t.Fatal("defaults missing on a bare room")
	}
}

func TestRoomErrorCarriesMessage(t *testing.T) {
	err := fail("CODE", "说明").(*Error)
	if err.Error() != "说明" || err.Code != "CODE" {
		t.Fatalf("room error: %+v", err)
	}
}

func TestCoachProposalBranches(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	if err := r.propose("best"); err == nil || err.(*Error).Code != "NO_COACH_MOVE" {
		t.Fatalf("propose without a move: %v", err)
	}
	// A six always offers launch choices, so the move phase is deterministic.
	r.mu.Lock()
	if err := r.state.Roll(6); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.propose("cancel"); err != nil || r.coach != nil {
		t.Fatalf("cancel: %v %+v", err, r.coach)
	}
	if err := r.propose("bogus"); err == nil || err.(*Error).Code != "INVALID_COACH_CHOICE" {
		t.Fatalf("invalid choice: %v", err)
	}
	if err := r.propose("next"); err == nil || err.(*Error).Code != "NO_COACH_MOVE" {
		t.Fatalf("next without a proposal: %v", err)
	}
	if err := r.propose("best"); err != nil || r.coach == nil {
		t.Fatalf("best: %v %+v", err, r.coach)
	}
	first := r.coach.Move.PieceID
	if err := r.propose("next"); err != nil {
		t.Fatalf("next: %v", err)
	}
	if r.coach.Move.PieceID == first || r.coachPrevious != first {
		t.Fatalf("next did not rotate: %+v previous=%q", r.coach, r.coachPrevious)
	}
	if err := r.propose("previous"); err != nil || r.coach.Move.PieceID != first {
		t.Fatalf("previous: %v %+v", err, r.coach)
	}
	// Previous without history is refused.
	r.coachPrevious = ""
	if err := r.propose("previous"); err == nil || err.(*Error).Code != "NO_PREVIOUS_MOVE" {
		t.Fatalf("previous without history: %v", err)
	}
}

func TestRestartGuards(t *testing.T) {
	store := NewStore()
	net, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = net.RestartAs(game.Options{Names: []string{"甲", "乙"}}, "host"); err == nil || err.(*Error).Code != "NETWORK_SETTINGS" {
		t.Fatalf("network restart: %v", err)
	}
	same := New(game.Options{Names: []string{"甲", "乙"}})
	same.mu.Lock()
	same.busy = true
	same.mu.Unlock()
	if _, err = same.RestartAs(game.Options{Names: []string{"甲", "乙"}}, "host"); err == nil || err.(*Error).Code != "VOICE_BUSY" {
		t.Fatalf("busy restart: %v", err)
	}
	same.mu.Lock()
	same.busy = false
	same.mu.Unlock()
	if _, err = same.RestartAs(game.Options{Names: []string{"甲", "乙"}, RoomName: " 新航线 "}, "host"); err != nil {
		t.Fatalf("plain restart: %v", err)
	}
	if same.Snapshot().Name != "新航线" {
		t.Fatalf("room name not applied: %q", same.Snapshot().Name)
	}
}

func TestCommandRejectionsKeepTheMatchUntouched(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}})
	state := roomState(t, r)

	r.mu.Lock()
	r.cache["quarantined"] = cached{Unavailable: true}
	r.mu.Unlock()
	if _, err := r.DoAs(Command{ID: "quarantined", GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err == nil || err.(*Error).Code != "RETRY_UNAVAILABLE" {
		t.Fatalf("quarantined retry: %v", err)
	}
	if _, err := r.DoAs(Command{ID: strings.Repeat("x", 161), GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err == nil || err.(*Error).Code != "INVALID_COMMAND" {
		t.Fatalf("long command id: %v", err)
	}
	if _, err := r.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "replay"}, "host"); err == nil || err.(*Error).Code != "INVALID_TRANSITION" {
		t.Fatalf("replay while playing: %v", err)
	}
	if _, err := r.Do(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "start"}, true); err == nil || err.(*Error).Code != "INVALID_ACTION" {
		t.Fatalf("voice control: %v", err)
	}
	// A finished match refuses further board actions.
	r.mu.Lock()
	r.state.Phase = "finished"
	r.state.Winner = r.state.Players[0].ID
	r.state.Players[0].Pieces[0].Progress = r.state.Board.Finish
	r.mu.Unlock()
	if _, err := r.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err == nil || err.(*Error).Code != "GAME_FINISHED" {
		t.Fatalf("action after finish: %v", err)
	}
	// A busy intercom blocks click commands.
	r = New(game.Options{Names: []string{"甲", "乙"}})
	state = roomState(t, r)
	r.mu.Lock()
	r.busy = true
	r.mu.Unlock()
	if _, err := r.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err == nil || err.(*Error).Code != "VOICE_BUSY" {
		t.Fatalf("busy click: %v", err)
	}
	// The idempotency cache is bounded.
	r = New(game.Options{Names: []string{"甲", "乙"}})
	state = roomState(t, r)
	r.mu.Lock()
	for i := 0; i < 4096; i++ {
		r.cache[string(rune(i))] = cached{}
	}
	r.mu.Unlock()
	if _, err := r.DoAs(Command{ID: "overflow", GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err == nil || err.(*Error).Code != "ROOM_LIMIT" {
		t.Fatalf("command cache limit: %v", err)
	}
	// Voice commands must present the live grant.
	r = New(game.Options{Names: []string{"甲", "乙"}})
	state = roomState(t, r)
	if _, err := r.Do(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll", Token: "stale"}, true); err == nil || err.(*Error).Code != "STALE_ACTION" {
		t.Fatalf("voice grant mismatch: %v", err)
	}
}

func TestAITurnAdvancesTheMatchAndRespectsPresence(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "灵矽"}, AI: true})
	r.mu.Lock()
	r.state.Current = 1
	before := r.state.Version
	r.mu.Unlock()
	r.AI()
	if r.Snapshot().Seq == 0 {
		t.Fatal("AI turn did not advance the room")
	}
	if r.state.Version == before {
		t.Fatal("AI turn did not change the game")
	}
	// A delegated seat with no connected human waits instead of playing.
	net := New(game.Options{Names: []string{"甲", "乙"}, Network: true})
	net.owner, net.invite = "host", game.ID()
	net.members = map[string]member{"host": {index: 0, delegated: true}}
	net.status = "playing"
	net.state.Current = 0
	net.AI()
	if net.Snapshot().Seq != 0 {
		t.Fatal("delegated seat played while nobody was connected")
	}
}

func TestBroadcastDropsForStaleListeners(t *testing.T) {
	store := NewStore()
	r, err := store.Create("host", game.Options{Names: []string{"甲", "乙"}})
	if err != nil {
		t.Fatal(err)
	}
	// A listener that never reads keeps one buffered message; later publishes
	// replace it instead of blocking the room.
	hostCh, stopHost := r.SubscribeFor("host")
	<-hostCh
	strangerCh, stopStranger := r.SubscribeFor("stranger")
	for i := 0; i < 3; i++ {
		r.mu.Lock()
		r.publish()
		r.mu.Unlock()
	}
	if data := <-hostCh; len(data) == 0 {
		t.Fatal("member received no state")
	}
	select {
	case <-strangerCh:
		t.Fatal("non-member listener received room state")
	default:
	}
	stopHost()
	stopStranger()
}

func TestNetworkJoinAndLeaveGuards(t *testing.T) {
	store := NewStore()
	if _, err := store.JoinProfile("guest", "missing", "invite", "客人", "", ""); err == nil || err.(*Error).Code != "INVALID_INVITE" {
		t.Fatalf("missing room: %v", err)
	}
	if _, err := store.JoinProfile("guest", "missing", "invite", "客人", "", "https://example.test/a.png"); err == nil || err.(*Error).Code != "INVALID_PROFILE" {
		t.Fatalf("remote avatar: %v", err)
	}
	// Profiles are resolved inside the browser's own saved family first.
	persisted := openTestStore(t, time.Hour)
	if _, err := persisted.JoinProfile("guest", "missing", "invite", "客人", "missing-profile", ""); err == nil || err.(*Error).Code != "PROFILE_NOT_FOUND" {
		t.Fatalf("unknown profile: %v", err)
	}

	// Same-screen rooms have no invitation at all.
	same, _ := store.Create("host", game.Options{Names: []string{"甲", "乙"}})
	if _, err := store.Join("guest", same.ID, "", "客人"); err == nil || err.(*Error).Code != "INVALID_INVITE" {
		t.Fatalf("same-screen join: %v", err)
	}

	// Bot names are reserved even for humans.
	withBot, _ := store.Create("bot-host", game.Options{Names: []string{"房主", "灵矽机器人"}, Network: true, AI: true})
	invite := withBot.SnapshotFor("bot-host").Network.Invite
	if _, err := store.Join("guest", withBot.ID, invite, "灵矽机器人"); err == nil || err.(*Error).Code != "NAME_TAKEN" {
		t.Fatalf("bot name: %v", err)
	}
	// The room fills once every human seat is taken.
	filling, _ := store.Create("fill-host", game.Options{Names: []string{"房主", "朋友", "灵矽机器人"}, Network: true, AI: true})
	fillInvite := filling.SnapshotFor("fill-host").Network.Invite
	if _, err := store.Join("guest", filling.ID, fillInvite, "朋友"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Join("third", filling.ID, fillInvite, "路人"); err == nil || err.(*Error).Code != "ROOM_FULL" {
		t.Fatalf("full room: %v", err)
	}
	// Nobody can join or leave once the match started.
	filling.mu.Lock()
	filling.status = "playing"
	filling.mu.Unlock()
	if _, err := store.Join("late", filling.ID, fillInvite, "迟到"); err == nil || err.(*Error).Code != "GAME_STARTED" {
		t.Fatalf("join after start: %v", err)
	}
	if err := store.Leave("guest", filling.ID); err == nil || err.(*Error).Code != "GAME_STARTED" {
		t.Fatalf("leave after start: %v", err)
	}
	if err := store.Leave("stranger", filling.ID); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatalf("stranger leave: %v", err)
	}

	// Ready is restricted to waiting-room members.
	lobby, _ := store.Create("lobby-host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if _, err := lobby.SetReady("stranger", true); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatalf("stranger ready: %v", err)
	}
	lobby.mu.Lock()
	lobby.status = "playing"
	lobby.mu.Unlock()
	if _, err := lobby.SetReady("lobby-host", true); err == nil || err.(*Error).Code != "GAME_STARTED" {
		t.Fatalf("ready after start: %v", err)
	}
	if err := lobby.Leave("lobby-host"); err == nil || err.(*Error).Code != "HOST_ONLY" {
		t.Fatalf("host leave: %v", err)
	}

	// Starting requires every human seat to be occupied and ready.
	lobby2, _ := store.Create("host2", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	state := roomState(t, lobby2)
	if _, err := lobby2.DoAs(Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "start"}, "host2"); err == nil || err.(*Error).Code != "PLAYERS_NOT_READY" {
		t.Fatalf("start with an empty seat: %v", err)
	}
}

func TestVoiceStateFallsBackToDisconnected(t *testing.T) {
	r := New(game.Options{Names: []string{"甲", "乙"}, Waiting: true})
	r.VoiceState("speaking")
	if r.Snapshot().Voice != "disconnected" {
		t.Fatalf("voice state on a waiting room: %q", r.Snapshot().Voice)
	}
	listen := New(game.Options{Names: []string{"甲", "乙"}})
	listen.VoiceState("listening")
	if listen.Snapshot().Voice != "listening" {
		t.Fatalf("playing room voice state: %q", listen.Snapshot().Voice)
	}
	// BeginSpeech rejects finished matches.
	listen.mu.Lock()
	listen.state.Phase = "finished"
	listen.mu.Unlock()
	if err := listen.BeginSpeech(); err == nil || err.(*Error).Code != "GAME_NOT_ACTIVE" {
		t.Fatalf("speech after finish: %v", err)
	}
}
