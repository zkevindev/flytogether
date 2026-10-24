package room

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func spectatorFixture(t *testing.T) (*Store, *Room, string) {
	t.Helper()
	store := NewStore()
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	host := r.SnapshotFor("host")
	if host.Audience == nil || host.Audience.Token == "" {
		t.Fatal("host has no watch token")
	}
	if _, err = store.Watch("fan", r.ID, host.Audience.Token, "粉丝"); err != nil {
		t.Fatal(err)
	}
	return store, r, host.Audience.Token
}

func TestSpectatorVoiceIsReadOnlyAndLeaseBound(t *testing.T) {
	store, r, token := spectatorFixture(t)
	_ = token
	voice, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	view := voice.Snapshot()
	if view.Audience == nil || view.Audience.Me == nil || view.Audience.Me.Name != "粉丝" {
		t.Fatalf("spectator snapshot: %+v", view.Audience)
	}
	settings := voice.VoiceSettings()
	if settings.WakeEnabled || settings.WakeWord == "" {
		t.Fatalf("spectator voice settings: %+v", settings)
	}
	if err = voice.BeginSpeech(); err != nil {
		t.Fatalf("begin speech: %v", err)
	}
	if voice.CurrentActionToken() != "" {
		t.Fatal("spectator received a game action token")
	}
	// Voice state changes stay private to the spectator session.
	voice.VoiceState("speaking")
	if r.Snapshot().Voice != "disconnected" {
		t.Fatal("spectator voice state leaked into the player intercom")
	}
	read, err := voice.Tool("get_game_state", nil)
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	blob, _ := json.Marshal(read)
	for _, forbidden := range []string{"action_token", token} {
		if strings.Contains(string(blob), forbidden) {
			t.Fatalf("read tool leaked %q", forbidden)
		}
	}
	if _, err = voice.Tool("move_piece", map[string]string{"piece_id": "p0-0"}); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatalf("spectator executed a move: %v", err)
	}
	if _, err = r.VoiceForSpectator("fan"); err == nil || err.(*Error).Code != "VOICE_BUSY" {
		t.Fatalf("second lease for the same spectator: %v", err)
	}
	// Rotating the invite only affects new watchers; people already watching
	// keep their read-only assistant.
	if _, err = store.ConfigureAudience("host", r.ID, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if err = voice.BeginSpeech(); err != nil {
		t.Fatalf("rotation revoked an existing spectator: %v", err)
	}
	if _, err = voice.Tool("get_game_state", nil); err != nil {
		t.Fatalf("rotation removed the read tool: %v", err)
	}
	voice.Close()
	if _, err = r.VoiceForSpectator("fan"); err != nil {
		t.Fatalf("lease not released: %v", err)
	}
}

func TestSpectatorToolOmitsChatAndClosesAfterManualEnd(t *testing.T) {
	store, r, _ := spectatorFixture(t)
	if _, err := store.PostChat("fan", r.ID, ChatInput{ID: "cheer-1", Kind: "text", Text: "只在互动里出现"}); err != nil {
		t.Fatal(err)
	}
	voice, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	read, err := voice.Tool("get_game_state", nil)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(read)
	if strings.Contains(string(blob), "只在互动里出现") || strings.Contains(string(blob), "action_token") {
		t.Fatalf("tool included chat or a grant: %s", blob)
	}
	if !strings.Contains(string(blob), "standings") || !strings.Contains(string(blob), "recent_events") {
		t.Fatalf("tool missing board facts: %s", blob)
	}
	r.mu.Lock()
	r.status = "ended"
	r.mu.Unlock()
	if err = voice.BeginSpeech(); err == nil || err.(*Error).Code != "GAME_NOT_ACTIVE" {
		t.Fatalf("ended match still accepted speech: %v", err)
	}
}

func TestSpectatorVoiceRejectsClosedChannelAndNewMatch(t *testing.T) {
	_, r, _ := spectatorFixture(t)
	closed := false
	if _, err := r.ConfigureAudience("host", &closed, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.VoiceForSpectator("fan"); err == nil || err.(*Error).Code != "SPECTATE_CLOSED" {
		t.Fatalf("closed audience voice: %v", err)
	}

	_, r2, _ := spectatorFixture(t)
	voice, err := r2.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	// A new match invalidates the lease that was bound to the previous game.
	r2.mu.Lock()
	r2.state = game.New(game.Options{Names: []string{"房主", "朋友"}, Network: true})
	r2.mu.Unlock()
	if err = voice.BeginSpeech(); err == nil || err.(*Error).Code != "STALE_ACTION" {
		t.Fatalf("lease survived a new match: %v", err)
	}
	// Read-only tools still answer about the new match; the lease only gates speech.
	if _, err = voice.Tool("get_legal_moves", nil); err != nil {
		t.Fatalf("read tool after a new match: %v", err)
	}
	voice.Close()

	// Ending the room closes spectator voice without touching the seats.
	other := NewStore()
	r3, err := other.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	r3.mu.Lock()
	r3.status = "ended"
	r3.mu.Unlock()
	if _, err = r3.VoiceForSpectator("fan"); err == nil || err.(*Error).Code != "SPECTATE_CLOSED" {
		t.Fatalf("ended room voice: %v", err)
	}
}

func TestAudienceNoticesReachOnlyTheIntendedWatchers(t *testing.T) {
	_, r, _ := spectatorFixture(t)
	fanCh, stopFan := r.SubscribeFor("fan")
	defer stopFan()
	hostCh, stopHost := r.SubscribeFor("host")
	defer stopHost()
	// Subscribing publishes one state message per listener; drain it first.
	<-fanCh
	<-hostCh

	r.mu.Lock()
	r.queueNotice("", "audience_closed", "房主已关闭观赛")
	r.mu.Unlock()
	var notice map[string]string
	if err := json.Unmarshal(<-fanCh, &notice); err != nil || notice["type"] != "audience_closed" {
		t.Fatalf("spectator notice: %v %+v", err, notice)
	}
	select {
	case data := <-hostCh:
		var state map[string]any
		if json.Unmarshal(data, &state) == nil && state["type"] == "audience_closed" {
			t.Fatal("player received the spectator-only notice")
		}
	case <-time.After(50 * time.Millisecond):
	}

	// A targeted notice skips other listeners.
	r.mu.Lock()
	r.queueNotice("fan", "error", "只发给粉丝")
	r.mu.Unlock()
	if err := json.Unmarshal(<-fanCh, &notice); err != nil || notice["message"] != "只发给粉丝" {
		t.Fatalf("targeted notice: %v %+v", err, notice)
	}

	// Leaving the audience notifies that watcher only and frees the access.
	if err := r.leaveAudience("fan"); err != nil {
		t.Fatal(err)
	}
	if r.HasAccess("fan") {
		t.Fatal("audience access survived leave")
	}
	if !r.HasAccess("host") {
		t.Fatal("host lost access")
	}
	if r.HasAccess("stranger") {
		t.Fatal("stranger has access")
	}
}

func TestAudienceLimitsFallBackToDefaults(t *testing.T) {
	store := NewStore()
	r, _ := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if r.spectatorCap() != 8 || r.joinLimit() != 20 {
		t.Fatalf("default limits: %d %d", r.spectatorCap(), r.joinLimit())
	}
	store.SetAudienceLimits(3, 7)
	if r.spectatorCap() != 3 || r.joinLimit() != 7 {
		t.Fatalf("configured limits: %d %d", r.spectatorCap(), r.joinLimit())
	}
	// Zero or negative values reset to the built-in defaults.
	store.SetAudienceLimits(0, -2)
	if r.spectatorCap() != 8 || r.joinLimit() != 20 {
		t.Fatalf("reset limits: %d %d", r.spectatorCap(), r.joinLimit())
	}
}

func TestVoiceQuotaEnforcesDailyBudgets(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/games.db", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, kind := range []string{"realtime", "tts", "spectator"} {
		if err := store.ReserveVoice(ctx, kind, 0); err == nil {
			t.Fatalf("%s accepted a zero limit", kind)
		}
	}
	if err := store.ReserveVoice(ctx, "", 3); err == nil {
		t.Fatal("empty quota kind accepted")
	}
	if err := store.ReserveVoice(ctx, "unknown", 5); err == nil {
		t.Fatal("unknown quota kind accepted")
	}
	if err := store.ReserveVoice(ctx, "tts", 1); err != nil {
		t.Fatalf("first tts reservation: %v", err)
	}
	if err := store.ReserveVoice(ctx, "tts", 1); !errors.Is(err, ErrVoiceBudget) {
		t.Fatalf("budget not enforced: %v", err)
	}
	// Budgets are per kind.
	if err := store.ReserveVoice(ctx, "spectator", 1); err != nil {
		t.Fatalf("spectator budget shared with tts: %v", err)
	}
	// A store without persistence refuses rather than silently allowing calls.
	if err := NewStore().ReserveVoice(ctx, "realtime", 10); err == nil {
		t.Fatal("in-memory store reserved voice quota")
	}
}
