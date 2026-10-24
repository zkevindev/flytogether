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
