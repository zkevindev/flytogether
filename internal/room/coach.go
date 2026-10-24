package room

import "github.com/zkevindev/flytogether/internal/game"

type Coach struct {
	ID       string `json:"id"`
	GameID   string `json:"game_id"`
	Version  int    `json:"version"`
	PlayerID string `json:"player_id"`
	game.CoachOption
}

func (r *Room) invalidateCoach() {
	if r.coach != nil {
		r.coachExpired = true
	}
	r.coach, r.coachPrevious = nil, ""
}

func (r *Room) propose(choice string) error {
	if choice == "cancel" {
		r.invalidateCoach()
		return nil
	}
	options := r.state.CoachOptions(choice == "safe")
	if len(options) == 0 {
		return fail("NO_COACH_MOVE", "请先掷骰子，出现可选飞机后再让我帮你选")
	}
	index := 0
	switch choice {
	case "best", "safe":
	case "next", "previous":
		if r.coach == nil {
			return fail("NO_COACH_MOVE", "还没有建议，请先点或说“帮我选”")
		}
		for i, option := range options {
			if (choice == "previous" && option.Move.PieceID == r.coachPrevious) || (choice == "next" && option.Move.PieceID == r.coach.Move.PieceID) {
				index = i
				if choice == "next" {
					index = (i + 1) % len(options)
				}
				break
			}
		}
		if choice == "previous" && r.coachPrevious == "" {
			return fail("NO_PREVIOUS_MOVE", "还没有另一条建议，可以说“另一架呢”比较")
		}
	default:
		return fail("INVALID_COACH_CHOICE", "请选择推荐、另一架、安全一点或刚才那个")
	}
	if r.coach != nil && r.coach.Move.PieceID != options[index].Move.PieceID {
		r.coachPrevious = r.coach.Move.PieceID
	}
	r.coach = &Coach{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, PlayerID: r.state.Players[r.state.Current].ID, CoachOption: options[index]}
	r.coachExpired = false
	return nil
}
