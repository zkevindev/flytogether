package room

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestAudienceDoesNotTakeSeatsOrAuthority(t *testing.T) {
	store := NewStore()
	store.SetAudienceLimits(2, 5)
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true, Props: true})
	if err != nil {
		t.Fatal(err)
	}
	host := r.SnapshotFor("host")
	if host.Audience == nil || !host.Audience.Enabled || host.Audience.Token == "" || host.Audience.Token == host.Network.Invite {
		t.Fatal("host watch credential missing or reused the seat invite")
	}
	token := host.Audience.Token
	if _, err = store.Watch("host", r.ID, token, "房主旁观"); err == nil || err.(*Error).Code != "SEAT_KEPT" {
		t.Fatal("player opening the watch link must keep the seat", err)
	}
	if r.SnapshotFor("host").Me.PlayerID != "player-0" {
		t.Fatal("seat downgraded")
	}
	if _, err = store.Watch("old", r.ID, "stale-token", "旧链接"); err == nil || err.(*Error).Code != "INVALID_WATCH" {
		t.Fatal("old link", err)
	}
	if _, err = store.Watch("a", r.ID, token, "奶奶"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Watch("b", r.ID, token, "爷爷"); err != nil {
		t.Fatal(err)
	}
	first := r.SnapshotFor("a")
	second := r.SnapshotFor("b")
	if _, err = store.Watch("c", r.ID, token, "叔叔"); err == nil || err.(*Error).Code != "AUDIENCE_FULL" {
		t.Fatal("capacity", err)
	}
	if got := r.SnapshotFor("host"); got.Audience.Count != 2 || len(got.Network.Seats) != 2 || got.Network.Seats[1].Occupied {
		t.Fatal("spectators occupied a seat or were counted wrong", got.Audience.Count)
	}
	raw, _ := json.Marshal(first)
	blob := string(raw)
	for _, secret := range []string{token, host.Network.Invite, "action_token", "profile_ids", "host-cookie"} {
		if strings.Contains(blob, secret) {
			t.Fatal("spectator snapshot leaked", secret)
		}
	}
	if first.Audience.Me.Role != "spectator" || second.Audience.Me.ID == first.Audience.Me.ID {
		t.Fatal("spectator identity")
	}
	before := roomState(t, r)
	_, err = r.DoAs(Command{ID: game.ID(), GameID: before.ID, Version: before.Version, Action: "roll"}, "a")
	if err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatal("spectator command", err)
	}
	if _, err = r.DoAs(Command{ID: game.ID(), GameID: before.ID, Version: before.Version, Action: "roll", TargetPlayerID: "player-0"}, "a"); err == nil {
		t.Fatal("forged player id upgraded spectator")
	}
	if roomState(t, r).Version != before.Version {
		t.Fatal("rejected command changed the match")
	}
	voice, err := r.VoiceForSpectator("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = voice.Tool("roll_dice", map[string]string{}); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" {
		t.Fatal("spectator tool write", err)
	}
	if _, err = voice.Tool("use_prop", map[string]string{"prop_id": "bomb"}); err == nil {
		t.Fatal("prop tool")
	}
	state, err := voice.Tool("get_game_state", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(state)
	if strings.Contains(string(encoded), "action_token") || strings.Contains(string(encoded), token) {
		t.Fatal("read tool leaked authority")
	}
	if r.Snapshot().Voice != "disconnected" || roomState(t, r).Version != before.Version {
		t.Fatal("spectator voice changed the player intercom or match")
	}
	other, err := r.VoiceForSpectator("b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.VoiceForSpectator("a"); err == nil || err.(*Error).Code != "VOICE_BUSY" {
		t.Fatal("same spectator reused a second realtime lease", err)
	}
	playerVoice, err := r.VoiceFor("host")
	if err == nil || playerVoice != nil {
		t.Fatal("player voice must stay gated by the turn, not stolen by spectators", err)
	}
	other.Close()
	voice.Close()

	off := false
	if _, err = store.ConfigureAudience("a", r.ID, &off, false, nil); err == nil || err.(*Error).Code != "NOT_AUTHORIZED" && err.(*Error).Code != "HOST_ONLY" {
		t.Fatal("spectator configured the room", err)
	}
	if _, err = store.ConfigureAudience("host", r.ID, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Watch("d", r.ID, token, "新观众"); err == nil || err.(*Error).Code != "INVALID_WATCH" {
		t.Fatal("rotated link still worked", err)
	}
	if store.AudienceRoom("a", r.ID) == nil {
		t.Fatal("rotation removed people already watching")
	}
	if _, err = store.ConfigureAudience("host", r.ID, &off, false, nil); err != nil {
		t.Fatal(err)
	}
	if store.AudienceRoom("a", r.ID) != nil || r.SnapshotFor("host").Audience.Enabled || roomState(t, r).Version != before.Version {
		t.Fatal("closing watch changed the match or left access behind")
	}
	fresh := r.SnapshotFor("host").Audience.Token
	if _, err = store.Watch("a", r.ID, fresh, "奶奶"); err == nil || err.(*Error).Code != "SPECTATE_CLOSED" {
		t.Fatal("closed room still accepted the current link", err)
	}
}

func TestAudienceChatIsSeparateFromTheMatch(t *testing.T) {
	store := NewStore()
	r, _ := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	token := r.SnapshotFor("host").Audience.Token
	if _, err := store.Join("guest", r.ID, r.SnapshotFor("host").Network.Invite, "朋友"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Watch("fan", r.ID, token, "小姨"); err != nil {
		t.Fatal(err)
	}
	before := roomState(t, r)
	view, err := store.PostChat("fan", r.ID, ChatInput{ID: "cheer-1", Kind: "emoji", Emoji: "👏", TargetPlayerID: "player-0"})
	if err != nil || len(view.Audience.Messages) != 1 || view.Audience.Messages[0].Role != "spectator" {
		t.Fatal("cheer", err)
	}
	again, err := store.PostChat("fan", r.ID, ChatInput{ID: "cheer-1", Kind: "emoji", Emoji: "👏", TargetPlayerID: "player-0"})
	if err != nil || len(again.Audience.Messages) != 1 {
		t.Fatal("duplicate cheer", err)
	}
	if _, err = store.PostChat("fan", r.ID, ChatInput{Kind: "text", Text: "<script>alert(1)</script>"}); err == nil {
		t.Fatal("markup accepted")
	}
	if _, err = store.PostChat("guest", r.ID, ChatInput{ID: "note", Kind: "text", Text: "漂亮"}); err != nil {
		t.Fatal(err)
	}
	if roomState(t, r).Version != before.Version || roomState(t, r).Dice != before.Dice {
		t.Fatal("chat changed the match")
	}
	blocked, err := r.BlockChat("host", view.Audience.Messages[0].FromID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range blocked.Audience.Messages {
		if message.FromID == view.Audience.Messages[0].FromID {
			t.Fatal("blocked author still visible to the blocker")
		}
	}
	if len(r.SnapshotFor("guest").Audience.Messages) < 2 {
		t.Fatal("block changed other people's view")
	}
	chatOff := false
	if _, err = store.ConfigureAudience("host", r.ID, nil, false, &chatOff); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PostChat("fan", r.ID, ChatInput{Kind: "emoji", Emoji: "💪"}); err == nil || err.(*Error).Code != "CHAT_DISABLED" {
		t.Fatal("spectator still spoke", err)
	}
	if _, err = store.PostChat("guest", r.ID, ChatInput{ID: "player-still", Kind: "text", Text: "继续"}); err != nil {
		t.Fatal("player chat should remain", err)
	}
	if _, err = networkCommand(t, r, "host", "end"); err != nil {
		t.Fatal(err)
	}
	if _, err = networkCommand(t, r, "host", "replay"); err != nil {
		t.Fatal(err)
	}
	if len(r.SnapshotFor("fan").Audience.Messages) != 0 || r.SnapshotFor("fan").Audience.Me == nil {
		t.Fatal("replay should clear chat and keep the watcher")
	}
	if roomState(t, r).ID == before.ID {
		t.Fatal("replay did not start a new game")
	}
}

func TestAudienceMembershipRestoresWithoutSeatInvite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.db")
	store, err := OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	token := r.SnapshotFor("host").Audience.Token
	if _, err = store.Watch("fan", r.ID, token, "奶奶"); err != nil {
		t.Fatal(err)
	}
	id := r.ID
	store.Close()
	next, err := OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	restored := next.AudienceRoom("fan", id)
	if restored == nil || restored.SnapshotFor("fan").Audience.Me.Name != "奶奶" {
		t.Fatal("watcher was not restored")
	}
	if strings.Contains(string(mustJSON(restored.SnapshotFor("fan"))), token) {
		t.Fatal("restored watcher can see the watch secret")
	}
	if next.Get("fan", "") != nil {
		t.Fatal("watcher was restored as a player")
	}
	if next.Get("host", id) == nil || next.Get("host", id).SnapshotFor("host").Network.Invite == "" {
		t.Fatal("host seat missing after restore")
	}
	if err = next.LeaveAudience("fan", id); err != nil {
		t.Fatal(err)
	}
	if next.AudienceRoom("fan", id) != nil || next.Get("host", id) == nil {
		t.Fatal("leaving watch changed the match")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestDiscardRevokesLiveSpectatorAndVoiceAccess(t *testing.T) {
	s, r, _ := spectatorFixture(t)
	v, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	ch, stop := r.SubscribeFor("fan")
	defer stop()
	<-ch
	if err = s.Discard("host", r.ID); err != nil {
		t.Fatal(err)
	}
	var notice map[string]any
	if err = json.Unmarshal(<-ch, &notice); err != nil || notice["type"] != "audience_closed" {
		t.Fatal("no deletion notice", notice, err)
	}
	if r.HasAccess("fan") || s.AudienceRoom("fan", r.ID) != nil {
		t.Fatal("discard retained audience access")
	}
	if err = v.BeginSpeech(); err == nil {
		t.Fatal("discard retained voice")
	}
	if _, err = v.Tool("get_game_state", nil); err == nil {
		t.Fatal("discard retained late tools")
	}
}
