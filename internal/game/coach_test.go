package game

import (
	"encoding/json"
	"testing"
)

func TestCoachComparesRealCaptureRepliesWithoutChangingState(t *testing.T) {
	for _, board := range Boards() {
		s := New(Options{Names: []string{"孩子", "爸爸"}, Mode: "quick", Board: board.ID})
		s.Players[0].Pieces[0].Progress = 30
		s.Players[0].Pieces[1].Progress = 4
		// Place an opponent three normal steps behind the first destination.
		s.Players[1].Pieces[0].Progress = (32 - 3 - board.RingLength/4 + board.RingLength) % board.RingLength
		if err := s.Roll(2); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(s)
		best, safe := s.CoachOptions(false), s.CoachOptions(true)
		after, _ := json.Marshal(s)
		if string(before) != string(after) {
			t.Fatal("coach changed authoritative game")
		}
		if len(best) != 2 || best[0].Move.PieceID != s.BestMove().PieceID {
			t.Fatal("coach did not use legal best move")
		}
		if safe[0].Threats > best[0].Threats {
			t.Fatal("safe option increased visible threat")
		}
		if board.RingLength == 52 && (best[0].Threats == 0 || safe[0].Threats != 0 || best[0].Move.PieceID == safe[0].Move.PieceID) {
			t.Fatalf("failed to distinguish exposed plane: %+v / %+v", best, safe)
		}
	}
}
