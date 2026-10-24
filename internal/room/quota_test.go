package room

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestVoiceBudgetAtomicPersistentAndSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	s, err := OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.ReserveVoice(context.Background(), "realtime", 3)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrVoiceBudget) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("accepted %d upstream sessions", accepted.Load())
	}
	if err = s.ReserveVoice(context.Background(), "tts", 1); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(path, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []string{"realtime", "tts"} {
		if err = s.ReserveVoice(context.Background(), kind, 1); !errors.Is(err, ErrVoiceBudget) {
			t.Fatalf("restart reset %s quota: %v", kind, err)
		}
	}
	if _, err = s.storage.db.Exec("UPDATE voice_usage SET day='2000-01-01'"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveVoice(context.Background(), "realtime", 1); err != nil {
		t.Fatalf("new UTC day did not reset quota: %v", err)
	}
}
