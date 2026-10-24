package game

import (
	"strings"
	"testing"
)

func validState(t *testing.T) *State {
	t.Helper()
	return New(Options{Names: []string{"甲", "乙"}, Mode: "quick"})
}

func TestValidateAcceptsFreshAndRolledMatches(t *testing.T) {
	s := validState(t)
	if err := s.Validate(); err != nil {
		t.Fatalf("fresh match rejected: %v", err)
	}
	if s.Preset != "custom" {
		t.Fatalf("empty preset not normalised: %q", s.Preset)
	}
	if err := s.Roll(6); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("move phase rejected: %v", err)
	}
	if len(s.Legal) < 2 {
		t.Fatalf("expected launch choices, got %+v", s.Legal)
	}
}

func TestValidateRejectsInconsistentPresets(t *testing.T) {
	for name, mutate := range map[string]func(*State){
		"unknown preset":      func(s *State) { s.Preset = "chaos" },
		"gentle with props":   func(s *State) { s.Preset, s.Mode, s.PropsEnabled = "gentle", "quick", false },
		"gentle classic mode": func(s *State) { s.Preset, s.Mode, s.PropsEnabled = "gentle", "classic", true },
		"classic with props":  func(s *State) { s.Preset, s.Mode, s.PropsEnabled = "classic", "classic", true },
		"classic quick mode":  func(s *State) { s.Preset, s.Mode, s.PropsEnabled = "classic", "quick", false },
	} {
		t.Run(name, func(t *testing.T) {
			s := validState(t)
			mutate(s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "preset") {
				t.Fatalf("accepted %s: %v", name, err)
			}
		})
	}
}

func TestValidateRejectsMalformedIdentityPlayersAndBoard(t *testing.T) {
	for name, mutate := range map[string]func(*State){
		"missing game id":      func(s *State) { s.ID = "" },
		"one player":           func(s *State) { s.Players = s.Players[:1] },
		"current out of range": func(s *State) { s.Current = len(s.Players) },
		"negative current":     func(s *State) { s.Current = -1 },
		"zero version":         func(s *State) { s.Version = 0 },
		"zero turn":            func(s *State) { s.Turn = 0 },
		"unknown board":        func(s *State) { s.Board = BoardSpec{ID: "moon"} },
		"altered board":        func(s *State) { s.Board.RingLength++ },
		"unknown mode":         func(s *State) { s.Mode = "tournament" },
		"player id":            func(s *State) { s.Players[1].ID = "player-9" },
		"piece count":          func(s *State) { s.Players[0].Pieces = s.Players[0].Pieces[:1] },
		"empty name":           func(s *State) { s.Players[0].Name = "" },
		"bad avatar":           func(s *State) { s.Players[0].Avatar = "https://example.test/me.png" },
		"piece id":             func(s *State) { s.Players[0].Pieces[0].ID = "p9-0" },
		"piece progress":       func(s *State) { s.Players[0].Pieces[0].Progress = s.Board.Finish + 1 },
		"piece below hangar":   func(s *State) { s.Players[0].Pieces[0].Progress = -2 },
	} {
		t.Run(name, func(t *testing.T) {
			s := validState(t)
			mutate(s)
			if err := s.Validate(); err == nil {
				t.Fatalf("accepted malformed state: %+v", s)
			}
		})
	}
}

func TestValidatePhases(t *testing.T) {
	s := validState(t)
	s.Phase = "move"
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "dice") {
		t.Fatalf("move without dice: %v", err)
	}
	s.Phase = "move"
	s.Dice = 7
	if err := s.Validate(); err == nil {
		t.Fatal("accepted an impossible die")
	}
	// Only one plane may legally move, so the recorded "choice" phase is corrupt.
	s.Dice = 3
	s.Players[0].Pieces[0].Progress = 50
	s.Players[0].Pieces[1].Progress = -1
	s.Current = 0
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "choice") {
		t.Fatalf("move phase without a real choice: %v", err)
	}
	s.Phase = "finished"
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "winner") {
		t.Fatalf("finished without a winner: %v", err)
	}
	s.Winner = s.Players[0].ID
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "winner") {
		t.Fatalf("winner without a finished plane: %v", err)
	}
	s.Players[0].Pieces[0].Progress = s.Board.Finish
	if err := s.Validate(); err != nil {
		t.Fatalf("quick match with one finished plane rejected: %v", err)
	}
	classic := New(Options{Names: []string{"甲", "乙"}, Mode: "classic"})
	classic.Phase = "finished"
	classic.Winner = classic.Players[0].ID
	classic.Players[0].Pieces[0].Progress = classic.Board.Finish
	if err := classic.Validate(); err == nil || !strings.Contains(err.Error(), "winner") {
		t.Fatal("classic winner must finish every plane")
	}
	for i := range classic.Players[0].Pieces {
		classic.Players[0].Pieces[i].Progress = classic.Board.Finish
	}
	if err := classic.Validate(); err != nil {
		t.Fatalf("classic winner with every plane home rejected: %v", err)
	}
	s = classic
	s.Phase = "counting"
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "phase") {
		t.Fatalf("unknown phase: %v", err)
	}
}
