package game

import (
	"encoding/json"
	"testing"
)

func TestGentleFiveLaunchesWithoutBonusAndKeepsUniformDiceContract(t *testing.T) {
	s := New(Options{Names: []string{"孩子", "家长"}, Preset: "gentle", Mode: "classic"})
	if s.Mode != "quick" || !s.PropsEnabled || len(s.Players[0].Pieces) != 2 {
		t.Fatal("gentle options not canonical")
	}
	if s.Players[0].Props["launch"] != 2 || s.Players[0].Props["shield"] != 2 || s.Players[0].Props["bomb"] != 0 {
		t.Fatal("wrong supply")
	}
	if err := s.UseProp("bomb"); err == nil {
		t.Fatal("gentle bomb allowed")
	}
	if err := s.Roll(5); err != nil {
		t.Fatal(err)
	}
	if len(s.Legal) != 2 {
		t.Fatal("five did not offer both hangar planes")
	}
	if err := s.Move(s.Legal[0].PieceID); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Pieces[0].Progress != 0 || s.Current != 1 {
		t.Fatal("five did not launch or wrongly granted another roll")
	}
	c := New(Options{Names: []string{"甲", "乙"}, Preset: "classic", Props: true, Mode: "quick"})
	if c.Mode != "classic" || c.PropsEnabled || len(c.Players[0].Pieces) != 4 {
		t.Fatal("classic options not canonical")
	}
	_ = c.Roll(5)
	if c.Current != 1 || c.Players[0].Pieces[0].Progress != -1 {
		t.Fatal("classic five launched")
	}
	if err := c.UseProp("launch"); err == nil {
		t.Fatal("classic props allowed")
	}
}
func TestPresetGoalsAndLegacySaveValidation(t *testing.T) {
	for _, preset := range []string{"gentle", "classic"} {
		s := New(Options{Names: []string{"甲", "乙"}, Preset: preset})
		s.Players[0].Pieces[0].Progress = s.Board.Finish - 1
		if err := s.Roll(1); err != nil {
			t.Fatal(err)
		}
		if preset == "gentle" && s.Phase != "finished" {
			t.Fatal("gentle did not finish at one")
		}
		if preset == "classic" && s.Phase == "finished" {
			t.Fatal("classic finished before all four")
		}
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	s := New(Options{Names: []string{"甲", "乙"}, Mode: "quick", Props: true})
	s.Preset = ""
	s.RuleInfo = RuleSpec{}
	data, _ := json.Marshal(s)
	var old State
	_ = json.Unmarshal(data, &old)
	if err := old.Validate(); err != nil || old.Preset != "custom" {
		t.Fatal("legacy game was not preserved")
	}
	old.Preset = "future"
	if old.Validate() == nil {
		t.Fatal("unknown preset accepted")
	}
}
