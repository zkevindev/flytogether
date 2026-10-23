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

func TestBoostStillRequiresExactFinishOnEveryBoard(t *testing.T) {
	for _, board := range Boards() {
		for _, remaining := range []int{2, 3} {
			s := New(Options{Names: []string{"甲", "乙"}, Props: true, Board: board.ID})
			s.Players[0].Pieces[0].Progress = board.Finish - remaining
			if err := s.UseProp("boost"); err != nil {
				t.Fatal(err)
			}
			if err := s.Roll(1); err != nil {
				t.Fatal(err)
			}
			if remaining == 2 && s.Players[0].Pieces[0].Progress != board.Finish-2 {
				t.Fatal("boost overshot finish")
			}
			if remaining == 3 && (s.Winner != s.Players[0].ID || s.ActiveProp != "") {
				t.Fatal("exact boosted finish did not win and expire prop")
			}
		}
	}
}

func TestBombAndShieldOnEveryBoard(t *testing.T) {
	for _, board := range Boards() {
		t.Run(board.ID, func(t *testing.T) {
			s := New(Options{Names: []string{"甲", "乙"}, Board: board.ID, Props: true})
			target := &s.Players[1].Pieces[0]
			target.Progress, target.Shield = 10, true
			if err := s.UsePropOn("bomb", target.ID); err != nil {
				t.Fatal(err)
			}
			if target.Progress != 10 || target.Shield || s.Players[0].Props["bomb"] != 0 || s.ActiveProp != "bomb" || s.Dice != 0 {
				t.Fatal("bomb must consume shield, not move a protected plane or fabricate dice")
			}
			before, _ := json.Marshal(s)
			if err := s.UseProp("luck"); err == nil {
				t.Fatal("instant effects must occupy this roll's prop slot")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("rejected stacking mutated state")
			}
			if err := s.Roll(1); err != nil {
				t.Fatal(err)
			}
			if s.Current != 1 || s.ActiveProp != "" {
				t.Fatal("bomb must not grant another turn")
			}
			if err := s.UsePropOn("shield", target.ID); err != nil {
				t.Fatal(err)
			}
			if !target.Shield || s.Players[1].Props["shield"] != 0 {
				t.Fatal("shield not armed")
			}
			// A fresh unshielded opponent is returned to its hangar exactly once.
			s = New(Options{Names: []string{"甲", "乙"}, Board: board.ID, Props: true})
			s.Players[1].Pieces[0].Progress = board.RingLength
			if err := s.UsePropOn("bomb", s.Players[1].Pieces[0].ID); err != nil {
				t.Fatal(err)
			}
			if s.Players[1].Pieces[0].Progress != -1 || s.Version != 2 {
				t.Fatal("bomb did not return target to hangar")
			}
		})
	}
}

func TestTargetedPropRejectionIsAtomic(t *testing.T) {
	for _, prop := range []string{"bomb", "shield"} {
		for _, progress := range []int{-1, 0, 53, 58} {
			s := propGame()
			owner := 1
			if prop == "shield" {
				owner = 0
			}
			target := &s.Players[owner].Pieces[0]
			target.Progress = progress
			before, _ := json.Marshal(s)
			if err := s.UsePropOn(prop, target.ID); err == nil {
				t.Fatalf("%s accepted safe progress %d", prop, progress)
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("invalid target consumed inventory")
			}
		}
		for _, targetID := range []string{"", "unknown", "p0-0", "p1-0"} {
			s := propGame()
			s.Players[0].Pieces[0].Progress = 8
			s.Players[1].Pieces[0].Progress = 8
			if (prop == "shield" && targetID == "p0-0") || (prop == "bomb" && targetID == "p1-0") {
				continue
			}
			before, _ := json.Marshal(s)
			if err := s.UsePropOn(prop, targetID); err == nil {
				t.Fatal("accepted missing or wrong owner's target")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("rejection mutated state")
			}
		}
	}
}

func TestShieldAbsorbsOneCollisionOnly(t *testing.T) {
	s := propGame()
	s.Players[0].Pieces[0].Progress = 8
	// Player 0 landing at progress 10 shares cell with player 1 progress 49.
	s.Players[1].Pieces[0].Progress, s.Players[1].Pieces[0].Shield = 49, true
	if err := s.Roll(2); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Pieces[0].Progress != 10 || s.Players[1].Pieces[0].Progress != 49 || s.Players[1].Pieces[0].Shield || len(s.LastMove.Captured) != 0 || len(s.LastMove.Blocked) != 1 {
		t.Fatal("shield must break while both planes remain on cell")
	}
	s.Current, s.Phase, s.ActiveProp = 0, "roll", ""
	s.Players[0].Pieces[0].Progress = 8
	if err := s.Roll(2); err != nil {
		t.Fatal(err)
	}
	if s.Players[1].Pieces[0].Progress != -1 || len(s.LastMove.Captured) != 1 {
		t.Fatal("used shield incorrectly blocked second collision")
	}
}

func TestTacticalSuggestionUsesValidTarget(t *testing.T) {
	s := propGame()
	s.Players[1].Pieces[0].Progress = 31
	id, target := s.SuggestedPropTarget()
	if id != "bomb" || target != "p1-0" {
		t.Fatal("AI missed valid attack")
	}
	if err := s.UsePropOn(id, target); err != nil {
		t.Fatal(err)
	}
	s = propGame()
	s.Players[0].Props["bomb"] = 0
	s.Players[0].Pieces[0].Progress = 30
	s.Players[1].Pieces[0].Progress = 10
	id, target = s.SuggestedPropTarget()
	if id != "shield" || target != "p0-0" {
		t.Fatal("AI missed valid protection")
	}
	if err := s.UsePropOn(id, target); err != nil {
		t.Fatal(err)
	}
}
