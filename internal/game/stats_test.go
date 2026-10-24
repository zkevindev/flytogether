package game

import "testing"

func TestStatsSurviveFeedTruncationAndCoach(t *testing.T) {
	s := New(Options{Names: []string{"甲", "乙"}, Demo: true, Props: true})
	for i := 0; i < 40; i++ {
		actor := s.Current
		if err := s.Roll(1); err != nil {
			t.Fatal(err)
		}
		before := s.Players[actor].Stats
		if s.Phase == "move" {
			_ = s.CoachOptions(true)
			if s.Players[actor].Stats != before {
				t.Fatal("coach mutated actual statistics")
			}
			if err := s.Move(s.Legal[0].PieceID); err != nil {
				t.Fatal(err)
			}
		}
		if s.Phase == "finished" {
			break
		}
	}
	count := 0
	for _, p := range s.Players {
		count += p.Stats.Rolls
	}
	if count != 40 || len(s.History) != 24 {
		t.Fatalf("rolls=%d, feed=%d", count, len(s.History))
	}
}
func TestStatsCountActionsAndShieldedBomb(t *testing.T) {
	s := New(Options{Names: []string{"甲", "乙"}, Props: true})
	if err := s.UsePropOn("launch", "p0-0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Roll(2); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Pieces[0].Progress != 2 {
		t.Fatal("unique legal move after the takeoff did not resolve automatically")
	}
	if got := s.Players[0].Stats; got.Rolls != 1 || got.Takeoffs != 1 || got.PropsUsed != 1 {
		t.Fatal(got)
	}
	s.Current = 0
	s.Phase = "roll"
	s.Players[1].Pieces[0].Progress = 15
	s.Players[1].Pieces[0].Shield = true
	if err := s.UsePropOn("bomb", "p1-0"); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Stats.Captures != 0 {
		t.Fatal("shield counted as capture")
	}
	s.ActiveProp = ""
	s.Players[0].Props["bomb"] = 1
	if err := s.UsePropOn("bomb", "p1-0"); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Stats.Captures != 1 {
		t.Fatal("bomb missing capture")
	}
	s.ActiveProp = ""
	s.Players[0].Pieces[0].Progress = s.Board.Finish - 1
	if err := s.Roll(1); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Stats.Arrivals != 1 || s.Phase != "finished" {
		t.Fatal("arrival missing")
	}
	before := s.Players[0].Stats
	if err := s.Roll(0); err == nil || s.Players[0].Stats != before {
		t.Fatal("illegal roll counted")
	}
}
func TestStatsLuckyIsOneRoll(t *testing.T) {
	s := New(Options{Names: []string{"甲", "乙"}, Props: true})
	_ = s.UseProp("luck")
	if e := s.RollValues(2, 5); e != nil {
		t.Fatal(e)
	}
	if s.Players[0].Stats.Rolls != 1 || s.Players[0].Stats.PropsUsed != 1 {
		t.Fatal(s.Players[0].Stats)
	}
}
