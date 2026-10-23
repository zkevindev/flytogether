package game

import (
	"encoding/json"
	"testing"
)

func propGame() *State { return New(Options{Names: []string{"甲", "乙"}, Props: true}) }

func TestLaunchCardDoesNotCreateSixOrBonus(t *testing.T) {
	s := propGame()
	if err := s.UseProp("launch"); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Props["launch"] != 0 || s.ActiveProp != "launch" || s.Version != 2 {
		t.Fatal("card was not consumed once")
	}
	if err := s.Roll(2); err != nil {
		t.Fatal(err)
	}
	if s.Dice != 2 || len(s.Legal) != 2 || !s.Legal[0].Launch {
		t.Fatal("card did not enable a real two-point launch")
	}
	if err := s.Move(s.Players[0].Pieces[0].ID); err != nil {
		t.Fatal(err)
	}
	if s.Current != 1 || s.ActiveProp != "" || s.Players[0].Pieces[0].Progress != 0 {
		t.Fatal("launch card incorrectly moved extra steps or granted a bonus")
	}
}

func TestBoostAndLuckyDice(t *testing.T) {
	s := propGame()
	s.Players[0].Pieces[0].Progress = 1
	if err := s.UseProp("boost"); err != nil {
		t.Fatal(err)
	}
	if err := s.Roll(1); err != nil {
		t.Fatal(err)
	}
	if s.LastMove == nil || s.LastMove.To != 4 || s.Dice != 1 || s.ActiveProp != "" {
		t.Fatal("boost must add two steps to movement, not to dice")
	}
	s = propGame()
	if err := s.UseProp("luck"); err != nil {
		t.Fatal(err)
	}
	if err := s.RollValues(2, 6); err != nil {
		t.Fatal(err)
	}
	if s.Dice != 6 || len(s.DiceRolls) != 2 || s.DiceRolls[0] != 2 || s.Phase != "move" {
		t.Fatal("lucky dice did not retain both real rolls and select their maximum")
	}
	if err := s.Move(s.Players[0].Pieces[0].ID); err != nil {
		t.Fatal(err)
	}
	if s.Current != 0 || s.ActiveProp != "" {
		t.Fatal("a real six should preserve its bonus and expire the card")
	}
}

func TestIllegalPropUseIsAtomic(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*State)
		prop  string
	}{
		{"classic", func(s *State) { s.PropsEnabled = false }, "launch"},
		{"unknown", func(*State) {}, "fake"},
		{"no airborne plane", func(*State) {}, "boost"},
		{"no hangar", func(s *State) {
			for i := range s.Players[0].Pieces {
				s.Players[0].Pieces[i].Progress = 1
			}
		}, "launch"},
		{"exhausted", func(s *State) { s.Players[0].Props["launch"] = 0 }, "launch"},
		{"already selected", func(s *State) { _ = s.UseProp("luck") }, "launch"},
		{"after roll", func(s *State) { _ = s.Roll(6) }, "launch"},
		{"finished", func(s *State) { s.Phase = "finished" }, "luck"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := propGame()
			tc.setup(s)
			before, _ := json.Marshal(s)
			if err := s.UseProp(tc.prop); err == nil {
				t.Fatal("illegal card accepted")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("rejection changed game state")
			}
		})
	}
}

func TestLuckyDiceRequiresTwoValidValues(t *testing.T) {
	s := propGame()
	_ = s.UseProp("luck")
	before, _ := json.Marshal(s)
	for _, values := range [][]int{{6}, {2, 7}, {0, 4}, {1, 2, 3}} {
		if err := s.RollValues(values...); err == nil {
			t.Fatal("invalid dice accepted")
		}
		after, _ := json.Marshal(s)
		if string(before) != string(after) {
			t.Fatal("invalid roll changed game")
		}
	}
}
