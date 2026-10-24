package game

import (
	"math/rand"
	"testing"
)

func TestCoopEndsOnlyWhenEveryPlayerHasArrived(t *testing.T) {
	s := New(Options{Names: []string{"甲", "乙", "丙"}, Mode: "coop", Board: "cross"})
	if s.Mode != "coop" || s.RuleInfo.ID != "coop" || len(s.Players[0].Pieces) != 2 {
		t.Fatalf("unexpected co-op setup: mode=%s rules=%+v pieces=%d", s.Mode, s.RuleInfo, len(s.Players[0].Pieces))
	}
	for i := range s.Players {
		s.Players[i].Pieces[0].Progress = s.Board.Finish - 1
	}
	for i := range s.Players {
		player := s.Players[s.Current]
		s.apply(Move{PieceID: player.Pieces[0].ID, Label: player.Pieces[0].Label, To: s.Board.Finish, Finish: true})
		if i < len(s.Players)-1 && (s.Phase == "finished" || s.Winner != "") {
			t.Fatalf("team finished before player %d arrived", i+1)
		}
	}
	if s.Phase != "finished" || s.Winner != "team" {
		t.Fatalf("last arrival did not complete the team: phase=%s winner=%q", s.Phase, s.Winner)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid team victory rejected: %v", err)
	}
	s.Players[1].Pieces[0].Progress = -1
	if err := s.Validate(); err == nil {
		t.Fatal("team victory accepted when a player had no plane at the finish")
	}
}

func TestFamilyRules(t *testing.T) {
	tests := []struct {
		name              string
		from, die, to     int
		jump, fly, finish bool
	}{
		{"launch", -1, 6, 0, false, false, false},
		{"normal", 1, 2, 3, false, false, false},
		{"jump", 1, 4, 9, true, false, false},
		{"flight", 16, 1, 29, false, true, false},
		{"jump into flight", 12, 1, 29, true, true, false},
		{"home lane", 50, 3, 53, false, false, false},
		{"precise finish", 57, 1, 58, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Options{Mode: "quick", Names: []string{"孩子", "家长"}})
			s.Players[0].Pieces[0].Progress = tt.from
			if err := s.Roll(tt.die); err != nil {
				t.Fatal(err)
			}
			if s.Phase == "move" {
				if err := s.Move(s.Players[0].Pieces[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			m := s.LastMove
			if m == nil || m.To != tt.to || m.Jump != tt.jump || m.Fly != tt.fly || m.Finish != tt.finish {
				t.Fatalf("move=%+v", m)
			}
			if tt.finish && s.Winner != s.Players[0].ID {
				t.Fatal("winner missing")
			}
		})
	}
}

func TestOvershootAndUnavailableLaunch(t *testing.T) {
	for _, from := range []int{-1, 57} {
		s := New(Options{Mode: "quick", Names: []string{"甲", "乙"}})
		s.Players[0].Pieces[0].Progress = from
		if err := s.Roll(2); err != nil {
			t.Fatal(err)
		}
		if s.Current != 1 || s.Phase != "roll" || s.LastMove != nil {
			t.Fatalf("unavailable roll changed piece: %+v", s)
		}
	}
}

func TestFinalLandingCapturesWholeStack(t *testing.T) {
	s := New(Options{Mode: "quick", Names: []string{"甲", "乙"}})
	s.Players[0].Pieces[0].Progress = 1
	for i := range s.Players[1].Pieces {
		s.Players[1].Pieces[i].Progress = 41
	}
	if err := s.Roll(1); err != nil {
		t.Fatal(err)
	}
	if len(s.LastMove.Captured) != 2 {
		t.Fatalf("capture=%+v", s.LastMove)
	}
	for _, p := range s.Players[1].Pieces {
		if p.Progress != -1 {
			t.Fatal("captured piece not reset")
		}
	}
}

func TestSixGrantsAnotherTurnAndIllegalMoveDoesNotAdvance(t *testing.T) {
	s := New(Options{Mode: "quick", Names: []string{"甲", "乙"}})
	if err := s.Roll(6); err != nil {
		t.Fatal(err)
	}
	v := s.Version
	if err := s.Move("other-player-piece"); err == nil {
		t.Fatal("illegal move accepted")
	}
	if s.Version != v {
		t.Fatal("failed move advanced version")
	}
	if err := s.Move(s.Players[0].Pieces[0].ID); err != nil {
		t.Fatal(err)
	}
	if s.Current != 0 || s.Turn != 2 || s.Phase != "roll" {
		t.Fatal("six did not grant turn")
	}
}

func TestCompleteGamesMaintainInvariants(t *testing.T) {
	for seed := int64(0); seed < 12; seed++ {
		rng := rand.New(rand.NewSource(seed))
		s := New(Options{Mode: "classic", Names: []string{"甲", "乙", "丙", "丁"}})
		for step := 0; step < 5000 && s.Phase != "finished"; step++ {
			var err error
			if s.Phase == "roll" {
				err = s.Roll(rng.Intn(6) + 1)
			} else {
				err = s.Move(s.BestMove().PieceID)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, player := range s.Players {
				for _, p := range player.Pieces {
					if p.Progress < -1 || p.Progress > Finish {
						t.Fatalf("invalid piece=%+v", p)
					}
				}
			}
			if s.Current < 0 || s.Current >= len(s.Players) {
				t.Fatal("invalid current player")
			}
		}
		if s.Phase != "finished" || s.Winner == "" {
			t.Fatalf("seed %d failed to finish", seed)
		}
	}
}
