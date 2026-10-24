package linx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestBridgeDeadlineReleasesSlotAndNeverDialsOverBudget(t *testing.T) {
	// The controlled TLS provider accepts and then stays silent, including during handshake.
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	defer provider.Close()
	old := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = old }()
	b := &Bridge{Config: Config{URL: strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", APIKey: "test", Model: "test"}, MaxConnections: 1, SessionTimeout: 80 * time.Millisecond}
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	started := time.Now()
	err := b.Serve(context.Background(), nil, r)
	if err == nil || !strings.Contains(err.Error(), "时限") || time.Since(started) > time.Second {
		t.Fatalf("deadline failed: %v", err)
	}
	if b.active != 0 || r.Snapshot().Voice != "disconnected" {
		t.Fatal("deadline leaked active slot or room voice")
	}
	b.active = 1
	if err = b.Serve(context.Background(), nil, r); err == nil || !strings.Contains(err.Error(), "稍后") {
		t.Fatal("concurrency limit ignored")
	}
	b.active = 0
	denied := errors.New("test quota exhausted")
	b.Reserve = func(context.Context) error { return denied }
	if err = b.Serve(context.Background(), nil, r); !errors.Is(err, denied) || b.active != 0 {
		t.Fatal("quota error leaked slot")
	}
	s := &Speaker{Config: b.Config, Reserve: func(context.Context) error { return denied }, cache: map[string][]byte{"cached": {1, 2}}}
	if data, err := s.Say(context.Background(), "cached"); err != nil || len(data) != 2 {
		t.Fatal("cache incorrectly used daily quota")
	}
	if _, err = s.Say(context.Background(), "new"); !errors.Is(err, denied) || s.busy {
		t.Fatal("TTS quota failed or leaked busy slot")
	}
}

func TestSpectatorBudgetsAndDeadlineStaySeparateFromPlayer(t *testing.T) {
	store := room.NewStore()
	r, err := store.Create("host", game.Options{Names: []string{"房主", "朋友"}, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Watch("fan", r.ID, r.SnapshotFor("host").Audience.Token, "观众"); err != nil {
		t.Fatal(err)
	}
	v, err := r.VoiceForSpectator("fan")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	b := &Bridge{MaxConnections: 1, SpectatorMax: 1, active: 1, spectators: 1}
	if err = b.Serve(context.Background(), nil, v); err == nil || !strings.Contains(err.Error(), "观赛助手名额") || b.active != 1 || b.spectators != 1 {
		t.Fatal("spectator connection limit", err)
	}
	b.spectators = 0
	playerReservations := 0
	b.Reserve = func(context.Context) error { playerReservations++; return nil }
	b.ReserveSpectator = func(context.Context) error { return room.ErrVoiceBudget }
	if err = b.Serve(context.Background(), nil, v); err == nil || !strings.Contains(err.Error(), "额度") || b.spectators != 0 || b.active != 1 || playerReservations != 0 {
		t.Fatal("shared or leaked budget", err)
	}
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { <-req.Context().Done() }))
	defer provider.Close()
	old := http.DefaultClient
	http.DefaultClient = provider.Client()
	defer func() { http.DefaultClient = old }()
	b.Config = Config{URL: strings.Replace(provider.URL, "https:", "wss:", 1) + "/v1/realtime", APIKey: "test-only", Model: "test"}
	b.ReserveSpectator = nil
	b.SessionTimeout = time.Minute
	b.SpectatorTimeout = 80 * time.Millisecond
	started := time.Now()
	if err = b.Serve(context.Background(), nil, v); err == nil || !strings.Contains(err.Error(), "观赛助手已到时限") || time.Since(started) > time.Second {
		t.Fatal("spectator deadline", err)
	}
	if b.active != 1 || b.spectators != 0 {
		t.Fatal("spectator timeout released player slot or leaked its own")
	}
}
