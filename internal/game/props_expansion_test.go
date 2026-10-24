package game

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExpandedPropsBoundaries(t *testing.T) {
	for _, board := range Boards() {
		t.Run(board.ID, func(t *testing.T) {
			s := New(Options{Names: []string{"甲", "乙"}, Board: board.ID, Props: true})
			enemy := &s.Players[1].Pieces[0]
			enemy.Progress = 2
			if err := s.UsePropOn("boomerang", enemy.ID); err != nil {
				t.Fatal(err)
			}
			if enemy.Progress != 1 || s.Players[0].Stats.Captures != 0 || s.Dice != 0 || s.Phase != "roll" {
				t.Fatal("retreat must clamp to main route and never roll/capture")
			}
			e := s.History[len(s.History)-1]
			if e.PropID != "boomerang" || len(e.Effects) != 1 || e.Effects[0].From != 2 || e.Effects[0].To != 1 {
				t.Fatal("missing committed effect")
			}
			before, _ := json.Marshal(s)
			if err := s.UseProp("escort"); err == nil {
				t.Fatal("stacked props")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("failed command mutated game")
			}
			s = New(Options{Names: []string{"甲", "乙"}, Board: board.ID, Props: true})
			enemy = &s.Players[1].Pieces[0]
			enemy.Progress = 12
			enemy.Shield = true
			if err := s.UsePropOn("boomerang", enemy.ID); err != nil {
				t.Fatal(err)
			}
			if enemy.Progress != 12 || enemy.Shield || s.History[len(s.History)-1].Effects[0].Kind != "blocked" {
				t.Fatal("shield absorption")
			}
			s = New(Options{Names: []string{"甲", "乙"}, Board: board.ID, Props: true})
			own := &s.Players[0].Pieces[0]
			own.Progress = board.RingLength
			own.Shield = true
			if err := s.UsePropOn("rescue", own.ID); err != nil {
				t.Fatal(err)
			}
			if own.Progress != 0 || own.Shield || s.Players[0].Stats.Takeoffs != 0 || s.Players[0].Stats.Captures != 0 || s.Players[0].Stats.PropsUsed != 1 {
				t.Fatal("rescue must not invent flight or capture")
			}
		})
	}
}
func TestEscortTargetsAndOldInventory(t *testing.T) {
	s := New(Options{Names: []string{"甲", "乙"}, Mode: "classic", Props: true})
	s.Players[0].Pieces[0].Progress = 1
	s.Players[0].Pieces[1].Progress = 6
	s.Players[0].Pieces[2].Progress = s.Board.RingLength + 1
	s.Players[1].Pieces[0].Progress = 2
	if err := s.UseProp("escort"); err != nil {
		t.Fatal(err)
	}
	if !s.Players[0].Pieces[0].Shield || !s.Players[0].Pieces[1].Shield || s.Players[0].Pieces[2].Shield || s.Players[1].Pieces[0].Shield {
		t.Fatal("area protection leaked")
	}
	if len(s.History[len(s.History)-1].Effects) != 2 || s.Players[0].Props["escort"] != 0 {
		t.Fatal("consumption or metadata")
	}
	for _, id := range []string{"boomerang", "rescue", "escort"} {
		for _, progress := range []int{-1, 0, s.Board.RingLength + 1, s.Board.Finish} {
			s = propGame()
			s.Players[0].Pieces[0].Progress = progress
			s.Players[1].Pieces[0].Progress = progress
			before, _ := json.Marshal(s)
			target := "p0-0"
			if id == "boomerang" {
				target = "p1-0"
			}
			if id == "escort" {
				target = ""
			}
			if s.UsePropOn(id, target) == nil {
				t.Fatalf("%s accepted protected target %d", id, progress)
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("invalid target consumed inventory")
			}
		}
	}
	s = propGame()
	s.Players[0].Props = map[string]int{"launch": 1, "bomb": 1, "shield": 1}
	s.Players[1].Pieces[0].Progress = 5
	before := s.Players[0].Props
	copyProps := map[string]int{}
	for k, v := range before {
		copyProps[k] = v
	}
	if s.UsePropOn("boomerang", "p1-0") == nil || !reflect.DeepEqual(before, copyProps) {
		t.Fatal("old save must not gain new inventory")
	}
	gentle := New(Options{Names: []string{"甲", "乙"}, Preset: "gentle"})
	if gentle.Players[0].Props["rescue"] != 1 || gentle.Players[0].Props["escort"] != 1 || gentle.Players[0].Props["boomerang"] != 0 {
		t.Fatal("gentle supplies")
	}
}
