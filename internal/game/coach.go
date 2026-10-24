package game

import "sort"

// CoachOption only evaluates already legal moves. Threats counts opponent/die
// combinations that could capture the moved plane on the current board, without
// props or intervening moves. It is a comparison, not a winning probability.
type CoachOption struct {
	Move    Move   `json:"move"`
	Reason  string `json:"reason"`
	Risk    string `json:"risk"`
	Threats int    `json:"threats"`
}

func (s *State) CoachOptions(safe bool) []CoachOption {
	options := []CoachOption{}
	if s.Phase != "move" || s.Players[s.Current].AI {
		return options
	}
	for _, move := range s.Legal {
		option := CoachOption{Move: move, Reason: move.Summary}
		// Move a deep copy so captured planes and shields affect the estimate.
		after := *s
		after.Players = append([]Player(nil), s.Players...)
		for i := range after.Players {
			after.Players[i].Pieces = append([]Piece(nil), s.Players[i].Pieces...)
		}
		after.History = append([]Event(nil), s.History...)
		_ = after.Move(move.PieceID)
		for opponent := range after.Players {
			if opponent == s.Current || after.Phase == "finished" {
				continue
			}
			after.Current, after.ActiveProp = opponent, ""
			for die := 1; die <= 6; die++ {
				after.Dice = die
				canHit := false
				for _, reply := range after.legalMoves() {
					for _, captured := range reply.Captured {
						canHit = canHit || captured == move.PieceID
					}
				}
				if canHit {
					option.Threats++
				}
			}
		}
		switch {
		case move.Finish:
			option.Risk = "这架可以抵达终点。"
		case move.To == 0 || move.To > s.Board.RingLength:
			option.Risk = "落点不参与公共航线撞机。"
		case option.Threats == 0:
			option.Risk = "按当前棋局比较，这步相对稳妥，但道具和后续走棋仍会改变风险。"
		default:
			option.Risk = "这个落点有被对手普通一步撞回机场的可能。"
		}
		options = append(options, option)
	}
	best := s.BestMove().PieceID
	sort.SliceStable(options, func(i, j int) bool {
		if safe && options[i].Threats != options[j].Threats {
			return options[i].Threats < options[j].Threats
		}
		return options[i].Move.PieceID == best && options[j].Move.PieceID != best
	})
	return options
}
