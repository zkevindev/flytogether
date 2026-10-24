package linx

import (
	"encoding/json"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestCoachFollowupsUseCurrentProposalAndKeepQuestionsReadOnly(t *testing.T) {
	r := room.New(game.Options{Names: []string{"孩子", "爸爸"}, Mode: "quick", Props: true, Demo: true})
	for _, phrase := range []string{"就它", "确认飞行", "就这个"} {
		a, ok := planVoiceAction(r, phrase)
		if !ok || a.Name != "get_game_state" || a.Notice == "" {
			t.Fatalf("dangling confirmation became action: %s %+v", phrase, a)
		}
	}
	for phrase, choice := range map[string]string{"帮我选": "best", "推荐走法": "best", "另一架呢": "next", "换一个": "next", "安全一点": "safe", "刚才那个": "previous"} {
		a, ok := planVoiceAction(r, phrase)
		if !ok || a.Name != "coach_move" || a.Choice != choice {
			t.Fatalf("missed coach intent: %s %+v", phrase, a)
		}
	}
	// Demo supplies two airborne legal choices; a lucky roll is unnecessary.
	var state game.State
	if err := json.Unmarshal(r.Snapshot().State, &state); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Do(room.Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "roll"}, false); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(r.Snapshot().State, &state); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Do(room.Command{ID: game.ID(), GameID: state.ID, Version: state.Version, Action: "coach", Choice: "best"}, false); err != nil {
		t.Fatal(err)
	}
	proposal := r.Snapshot().Coach
	if a, ok := planVoiceAction(r, "晾衣架呢"); !ok || a.Name != "coach_move" || a.Choice != "next" {
		t.Fatal("context-bound ASR alternative missed")
	}
	for _, phrase := range []string{"就它", "就这架", "好的", "那就" + proposal.Move.Label} {
		a, ok := planVoiceAction(r, phrase)
		if !ok || a.Name != "confirm_coach" || a.CoachID != proposal.ID {
			t.Fatalf("confirmation missed: %s %+v", phrase, a)
		}
	}
	for _, phrase := range []string{"就它吗？", "就它？", "能不能就它", "不要走这个", "不走了", "不确认", "那就兔子号呢"} {
		a, ok := planVoiceAction(r, phrase)
		if !ok || a.Name == "confirm_coach" || a.Name == "move_piece" || a.Name == "roll_dice" {
			t.Fatalf("question or refusal changed game: %s %+v", phrase, a)
		}
	}
	r.VoiceState("disconnected")
	a, ok := planVoiceAction(r, "好的")
	if !ok || a.Name != "get_game_state" {
		t.Fatal("expired proposal short reply became a roll")
	}
}
