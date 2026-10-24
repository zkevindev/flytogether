package game

import "fmt"

// Validate prevents malformed or obsolete saved records from reaching rules code.
func (s *State) Validate() error {
	if s.ID == "" || len(s.Players) < 2 || len(s.Players) > 4 || s.Current < 0 || s.Current >= len(s.Players) || s.Version < 1 || s.Turn < 1 {
		return fmt.Errorf("invalid game identity")
	}
	board, ok := FindBoard(s.Board.ID)
	if !ok || board != s.Board {
		return fmt.Errorf("unsupported board")
	}
	if s.Mode != "quick" && s.Mode != "classic" {
		return fmt.Errorf("unsupported mode")
	}
	count := 2
	if s.Mode == "classic" {
		count = 4
	}
	for i, p := range s.Players {
		if p.ID != fmt.Sprintf("player-%d", i) || len(p.Pieces) != count || p.Name == "" {
			return fmt.Errorf("invalid players")
		}
		for j, piece := range p.Pieces {
			if piece.ID != fmt.Sprintf("p%d-%d", i, j) || piece.Progress < -1 || piece.Progress > s.Board.Finish {
				return fmt.Errorf("invalid piece")
			}
		}
	}
	switch s.Phase {
	case "roll":
		s.Legal = []Move{}
	case "move":
		if s.Dice < 1 || s.Dice > 6 {
			return fmt.Errorf("invalid dice")
		}
		s.Legal = s.legalMoves()
		if len(s.Legal) < 2 {
			return fmt.Errorf("invalid choice phase")
		}
	case "finished":
		if s.Winner == "" {
			return fmt.Errorf("missing winner")
		}
		found := false
		for _, p := range s.Players {
			if p.ID == s.Winner {
				n := 0
				for _, piece := range p.Pieces {
					if piece.Progress == s.Board.Finish {
						n++
					}
				}
				target := count
				if s.Mode == "quick" {
					target = 1
				}
				found = n >= target
			}
		}
		if !found {
			return fmt.Errorf("invalid winner")
		}
		s.Legal = []Move{}
	default:
		return fmt.Errorf("invalid phase")
	}
	return nil
}
