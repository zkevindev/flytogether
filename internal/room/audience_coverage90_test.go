package room

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func audience90Code(t *testing.T, err error, code string) {
	t.Helper()
	var got *Error
	if !errors.As(err, &got) || got.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestAudienceCoverage90SanitizeChat(t *testing.T) {
	base := ChatMessage{ID: "ok", FromID: "player-0", Name: "房主", Role: "host", Kind: "text", Text: "  加油  ", Emoji: "🔥"}
	cases := []struct {
		name   string
		mutate func(*ChatMessage)
	}{
		{"empty id", func(m *ChatMessage) { m.ID = "" }},
		{"long id", func(m *ChatMessage) { m.ID = strings.Repeat("x", 81) }},
		{"bad author", func(m *ChatMessage) { m.FromID = "w00000000000g" }},
		{"empty name", func(m *ChatMessage) { m.Name = "  " }},
		{"long name", func(m *ChatMessage) { m.Name = strings.Repeat("字", 11) }},
		{"control name", func(m *ChatMessage) { m.Name = "a\nb" }},
		{"bad role", func(m *ChatMessage) { m.Role = "admin" }},
		{"bad emoji", func(m *ChatMessage) { m.Kind = "emoji"; m.Emoji = "🍕" }},
		{"empty text", func(m *ChatMessage) { m.Text = "  " }},
		{"long text", func(m *ChatMessage) { m.Text = strings.Repeat("字", 41) }},
		{"control text", func(m *ChatMessage) { m.Text = "a\tb" }},
		{"markup text", func(m *ChatMessage) { m.Text = "a&b" }},
		{"bad kind", func(m *ChatMessage) { m.Kind = "command" }},
		{"bad target", func(m *ChatMessage) { m.TargetPlayerID = "w000000000001" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			if got := sanitizeChat([]ChatMessage{m}); len(got) != 0 {
				t.Fatalf("bad record accepted: %+v", got)
			}
		})
	}
	emoji := base
	emoji.ID = "emoji"
	emoji.Kind = "emoji"
	emoji.Role = "spectator"
	emoji.FromID = "w000000000001"
	emoji.TargetPlayerID = "player-1"
	player := base
	player.ID = "player"
	player.Role = "player"
	got := sanitizeChat([]ChatMessage{base, base, emoji, player})
	if len(got) != 3 || got[0].Text != "加油" || got[0].Emoji != "" || got[1].Text != "" || got[1].Emoji != "🔥" {
		t.Fatalf("normalization/dedup: %+v", got)
	}
	// Invalid occurrences must not reserve an ID and suppress a later valid record.
	bad := base
	bad.Kind = "invalid"
	if got := sanitizeChat([]ChatMessage{bad, base}); len(got) != 1 {
		t.Fatalf("invalid record reserved ID: %+v", got)
	}
	input := make([]ChatMessage, 0, 45)
	for i := 0; i < 45; i++ {
		m := base
		m.ID = fmt.Sprintf("m-%02d", i)
		input = append(input, m)
	}
	got = sanitizeChat(input)
	if len(got) != 40 || got[0].ID != "m-05" || got[39].ID != "m-44" {
		t.Fatalf("not the latest 40: %+v", got)
	}
	if input[0].Text != "  加油  " || input[0].Emoji != "🔥" {
		t.Fatal("sanitization mutated input")
	}
}

func TestAudienceCoverage90SanitizeIDs(t *testing.T) {
	invalid := []string{"", "player-4", "player-00", "player-a", "W000000000001", "w00000000000G", "w00000000000g", "w00000000001"}
	for _, id := range invalid {
		if validPublicID(id) {
			t.Fatalf("validPublicID(%q)", id)
		}
	}
	ids := append(append([]string(nil), invalid...), "player-0", "player-0", "player-3")
	for i := 0; i < 40; i++ {
		ids = append(ids, fmt.Sprintf("w%012x", i))
	}
	got := sanitizeIDs(ids)
	if len(got) != 32 || got[0] != "player-0" || got[1] != "player-3" || got[31] != "w00000000001d" {
		t.Fatalf("ID filtering/dedup/cap: %v", got)
	}
}

func audience90Persistent(t *testing.T) (*Store, *Room, string) {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "audience.db"), DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	r, err := s.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	token := r.SnapshotFor("host").Audience.Token
	if _, err = s.Watch("fan", r.ID, token, "粉丝"); err != nil {
		t.Fatal(err)
	}
	return s, r, token
}

func audience90Fault(t *testing.T, s *Store, on bool) {
	t.Helper()
	sql := "DROP TRIGGER audience90_fault"
	if on {
		sql = "CREATE TRIGGER audience90_fault BEFORE UPDATE ON rooms BEGIN SELECT RAISE(ABORT,'audience save fault'); END"
	}
	if _, err := s.storage.db.Exec(sql); err != nil {
		t.Fatal(err)
	}
}

func TestAudienceCoverage90SaveFailuresRollbackAndKeepLease(t *testing.T) {
	cases := []struct {
		name   string
		run    func(*Store, *Room, string) error
		verify func(*testing.T, *Store, *Room)
	}{
		{"watch new", func(s *Store, r *Room, token string) error {
			_, e := s.Watch("new", r.ID, token, "新观众")
			return e
		}, func(t *testing.T, s *Store, r *Room) {
			if r.HasSpectator("new") || s.AudienceRoom("new", "") != nil {
				t.Fatal("failed watch retained access")
			}
		}},
		{"watch returning", func(s *Store, r *Room, _ string) error { _, e := s.Watch("fan", r.ID, "ignored", "ignored"); return e }, nil},
		{"configure close rotate chat", func(s *Store, r *Room, _ string) error {
			off := false
			_, e := s.ConfigureAudience("host", r.ID, &off, true, &off)
			return e
		}, nil},
		{"post chat", func(s *Store, r *Room, _ string) error {
			_, e := s.PostChat("fan", r.ID, ChatInput{ID: "retry-chat", Kind: "text", Text: "保存后才出现"})
			return e
		}, nil},
		{"block spectator", func(_ *Store, r *Room, _ string) error { _, e := r.BlockChat("fan", "player-0", true); return e }, nil},
		{"block member", func(_ *Store, r *Room, _ string) error {
			_, e := r.BlockChat("host", audiencePublicID("fan"), true)
			return e
		}, nil},
		{"leave audience", func(s *Store, r *Room, _ string) error { return s.LeaveAudience("fan", r.ID) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, token := audience90Persistent(t)
			voice, err := r.VoiceForSpectator("fan")
			if err != nil {
				t.Fatal(err)
			}
			defer voice.Close()
			r.mu.Lock()
			before, _ := json.Marshal(r.record())
			joins := append([]time.Time(nil), r.joinAt...)
			r.mu.Unlock()
			audience90Fault(t, s, true)
			audience90Code(t, tc.run(s, r, token), "SAVE_UNAVAILABLE")
			r.mu.Lock()
			after, _ := json.Marshal(r.record())
			sameJoins := reflect.DeepEqual(joins, r.joinAt)
			chatTimes := len(r.chatAt["fan"])
			lease := r.spectatorLeases["fan"]
			r.mu.Unlock()
			if string(before) != string(after) {
				t.Fatalf("durable state changed after failed save\nbefore %s\nafter %s", before, after)
			}
			if !sameJoins || chatTimes != 0 {
				t.Fatal("failed operation consumed rate budget")
			}
			if lease != voice.lease || voice.BeginSpeech() != nil {
				t.Fatal("failed save revoked private voice lease")
			}
			if s.AudienceRoom("fan", "") != r {
				t.Fatal("failed save lost store audience mapping")
			}
			if tc.verify != nil {
				tc.verify(t, s, r)
			}
			audience90Fault(t, s, false)
			if err = tc.run(s, r, token); err != nil {
				t.Fatalf("retry after recovery: %v", err)
			}
		})
	}
}

func TestAudienceCoverage90ChatTruncationRateAndUnblock(t *testing.T) {
	s, r, _ := spectatorFixture(t)
	r.mu.Lock()
	for i := 0; i < 40; i++ {
		r.chat = append(r.chat, ChatMessage{ID: fmt.Sprintf("old-%02d", i), FromID: "player-0", Name: "房主", Role: "host", Kind: "text", Text: "过去"})
	}
	r.chatAt["fan"] = []time.Time{time.Now().Add(-time.Minute)}
	r.mu.Unlock()
	view, err := s.PostChat("fan", r.ID, ChatInput{ID: "new", Kind: "text", Text: "现在"})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Audience.Messages) != 40 || view.Audience.Messages[0].ID != "old-01" || view.Audience.Messages[39].ID != "new" {
		t.Fatal("post did not keep latest 40")
	}
	for i := 0; i < 4; i++ {
		if _, err = s.PostChat("fan", r.ID, ChatInput{ID: fmt.Sprintf("rate-%d", i), Kind: "emoji", Emoji: "✨"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.PostChat("fan", r.ID, ChatInput{ID: "rate-over", Kind: "text", Text: "过快"})
	audience90Code(t, err, "CHAT_RATE")
	// A retry at the limit is idempotent and must not consume another rate slot.
	if _, err = s.PostChat("fan", r.ID, ChatInput{ID: "new", Kind: "text", Text: "现在"}); err != nil {
		t.Fatal(err)
	}
	blocked, err := r.BlockChat("fan", "player-0", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Audience.Messages) != 5 {
		t.Fatalf("spectator block not private: %+v", blocked.Audience.Messages)
	}
	restored, err := r.BlockChat("fan", "player-0", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Audience.Messages) != 40 {
		t.Fatal("unblock failed to restore messages")
	}
	if _, err = r.BlockChat("host", "player-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err = r.BlockChat("host", "player-1", true); err != nil {
		t.Fatal(err)
	}
	if got := r.members["host"].blocked; len(got) != 1 {
		t.Fatalf("duplicate block: %v", got)
	}
	if _, err = r.BlockChat("host", "player-1", false); err != nil {
		t.Fatal(err)
	}
	if len(r.members["host"].blocked) != 0 {
		t.Fatal("member unblock failed")
	}
	_, err = r.BlockChat("stranger", "player-0", true)
	audience90Code(t, err, "NOT_AUTHORIZED")
	_, err = r.BlockChat("fan", "bad", true)
	audience90Code(t, err, "INVALID_CHAT")
}

func TestAudienceCoverage90VoiceRecentStandingsPrivacy(t *testing.T) {
	s, r, token := spectatorFixture(t)
	if _, err := s.PostChat("fan", r.ID, ChatInput{Kind: "text", Text: "private-chat-sentinel"}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.state.History = nil
	wantRecent := []string{}
	for i := 0; i < 12; i++ {
		text := fmt.Sprintf("event-%02d", i)
		r.state.History = append(r.state.History, game.Event{Text: text}, game.Event{})
		if i >= 4 {
			wantRecent = append(wantRecent, text)
		}
	}
	finish := r.state.Board.Finish
	r.state.Players[0].Pieces[0].Progress = finish
	r.state.Players[0].Pieces[1].Progress = finish
	r.options.ProfileIDs = []string{"private-profile-sentinel"}
	invite := r.invite
	gameID, version := r.state.ID, r.state.Version
	r.mu.Unlock()
	voice, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	for _, tool := range []string{"get_game_state", "get_legal_moves"} {
		t.Run(tool, func(t *testing.T) {
			result, err := voice.Tool(tool, nil)
			if err != nil {
				t.Fatal(err)
			}
			facts := result.(map[string]any)
			if !reflect.DeepEqual(facts["recent_events"], wantRecent) {
				t.Fatalf("recent events = %#v", facts["recent_events"])
			}
			standings := facts["standings"].([]map[string]any)
			if standings[0]["name"] != "房主" || standings[0]["arrived"] != 2 || standings[0]["furthest"] != finish || standings[1]["arrived"] != 0 || standings[1]["furthest"] != -1 {
				t.Fatalf("standings: %+v", standings)
			}
			if facts["game_id"] != gameID || facts["version"] != version {
				t.Fatal("facts not bound to current state")
			}
			public := facts["room"].(View)
			if public.Me != nil || public.ProfileIDs != nil || public.Coach != nil || public.CoachExpired || public.Voice != "disconnected" || public.Network.Invite != "" || public.Network.CanClaimHost || public.Network.HostClaimAt != nil || public.Network.HostClaimWaitSeconds != 0 || public.Audience.Token != "" || len(public.Audience.Messages) != 0 {
				t.Fatalf("private fields leaked: %+v", public)
			}
			blob, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{token, invite, "private-chat-sentinel", "private-profile-sentinel", "action_token"} {
				if strings.Contains(string(blob), secret) {
					t.Fatalf("tool leaked %q", secret)
				}
			}
		})
	}
	// Tool redaction must not mutate the ordinary spectator chat view.
	if len(r.SnapshotFor("fan").Audience.Messages) != 1 {
		t.Fatal("tool redaction erased public chat")
	}
}
