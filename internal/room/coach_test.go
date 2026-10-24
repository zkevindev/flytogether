package room

import (
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func coachRoom(t *testing.T) *Room {
	t.Helper()
	r := New(game.Options{Names: []string{"孩子", "爸爸"}, Mode: "quick", Props: true, Demo: true})
	if err := r.state.Roll(2); err != nil {
		t.Fatal(err)
	}
	if len(r.state.Legal) < 2 {
		t.Fatal("need multiple real legal choices")
	}
	return r
}

func coachCommand(r *Room, choice string) (View, error) {
	return r.Do(Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: "coach", Choice: choice}, false)
}

func TestCoachProposalChangesOnlyPreviewAndConfirmsOnce(t *testing.T) {
	r := coachRoom(t)
	before := string(r.Snapshot().State)
	first, err := coachCommand(r, "best")
	if err != nil {
		t.Fatal(err)
	}
	next, err := coachCommand(r, "next")
	if err != nil {
		t.Fatal(err)
	}
	if first.Coach.Move.PieceID == next.Coach.Move.PieceID {
		t.Fatal("next did not switch")
	}
	previous, err := coachCommand(r, "previous")
	if err != nil {
		t.Fatal(err)
	}
	if first.Coach.Move.PieceID != previous.Coach.Move.PieceID {
		t.Fatal("previous lost selection")
	}
	if string(r.Snapshot().State) != before {
		t.Fatal("advice changed dice, pieces, inventory, history or version")
	}
	stale := Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: "confirm_coach", CoachID: first.Coach.ID}
	if _, err := r.Do(stale, false); err == nil {
		t.Fatal("confirmed an older proposal with same game version")
	}
	confirm := stale
	confirm.ID, confirm.CoachID = game.ID(), previous.Coach.ID
	after, err := r.Do(confirm, false)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := r.Do(confirm, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.State) != string(retry.State) || r.state.Version != confirm.Version+1 || r.coach != nil {
		t.Fatal("confirmation did not commit once and clear proposal")
	}
}

func TestCoachInvalidatesOnCancelPauseRestartOtherMoveAndDisconnect(t *testing.T) {
	for _, action := range []string{"cancel", "pause", "end", "restart", "move", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			r := coachRoom(t)
			first, err := coachCommand(r, "best")
			if err != nil {
				t.Fatal(err)
			}
			old := first.Coach
			switch action {
			case "cancel":
				_, err = coachCommand(r, "cancel")
			case "pause", "end":
				_, err = r.Do(Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: action}, false)
			case "restart":
				_, err = r.Restart(game.Options{Names: []string{"孩子", "爸爸"}})
			case "move":
				_, err = r.Do(Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: "move", PieceID: old.Move.PieceID}, false)
			case "disconnect":
				r.VoiceState("disconnected")
			}
			if err != nil || r.Snapshot().Coach != nil {
				t.Fatalf("invalidation failed: %v", err)
			}
			before := string(r.Snapshot().State)
			_, err = r.Do(Command{ID: game.ID(), GameID: r.state.ID, Version: r.state.Version, Action: "confirm_coach", CoachID: old.ID}, false)
			if err == nil || string(r.Snapshot().State) != before {
				t.Fatal("old suggestion still executable")
			}
		})
	}
}

func TestCoachVoiceProposalConsumesOnlyItsOwnGrant(t *testing.T) {
	r := coachRoom(t)
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	token := r.CurrentActionToken()
	before := string(r.Snapshot().State)
	if _, err := r.Tool("coach_move", map[string]string{"action_token": token, "choice": "best"}); err != nil {
		t.Fatal(err)
	}
	if string(r.Snapshot().State) != before {
		t.Fatal("spoken advice moved a plane")
	}
	if _, err := r.Tool("confirm_coach", map[string]string{"action_token": token, "coach_id": r.coach.ID}); err == nil {
		t.Fatal("proposal reused grant for a game move")
	}
	r.VoiceState("ready")
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Tool("confirm_coach", map[string]string{"action_token": r.CurrentActionToken(), "coach_id": r.coach.ID}); err != nil {
		t.Fatal(err)
	}
}
