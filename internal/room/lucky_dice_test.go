package room

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"testing"
)

func TestLuckyDiceCommitsRealRollAndInventoryOnce(t *testing.T) {
	for _, voice := range []bool{false, true} {
		t.Run(map[bool]string{false: "click", true: "voice"}[voice], func(t *testing.T) {
			r := New(game.Options{Names: []string{"孩子", "爸爸"}, Mode: "quick", Props: true, Demo: true})
			var before game.State
			_ = json.Unmarshal(r.Snapshot().State, &before)
			command := Command{ID: "lucky-test", GameID: before.ID, Version: before.Version, Action: "prop", PropID: "luck"}
			if voice {
				if err := r.BeginSpeech(); err != nil {
					t.Fatal(err)
				}
				command.Token = r.CurrentActionToken()
				command.ID = "voice:" + command.Token
			}
			first, err := r.Do(command, voice)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := r.Do(command, voice)
			if err != nil {
				t.Fatal(err)
			}
			if string(first.State) != string(replay.State) {
				t.Fatal("retry changed the roll")
			}
			var after game.State
			_ = json.Unmarshal(r.Snapshot().State, &after)
			if after.Version != before.Version+2 || after.Players[0].Props["luck"] != 0 || len(after.DiceRolls) != 2 {
				t.Fatalf("lucky dice did not atomically activate and roll: %+v", after)
			}
			best := 0
			for _, value := range after.DiceRolls {
				if value < 1 || value > 6 {
					t.Fatal("invalid random die")
				}
				if value > best {
					best = value
				}
			}
			if after.Dice != best {
				t.Fatal("selected value is not max of real dice")
			}
			count := 0
			for _, event := range after.History {
				if event.Kind == "rolled" {
					count++
					if len(event.DiceRolls) != 2 {
						t.Fatal("missing two-dice animation event")
					}
				}
			}
			if count != 1 {
				t.Fatalf("want one actual roll event, got %d", count)
			}
			if voice {
				if _, err := r.Tool("roll_dice", map[string]string{"action_token": command.Token}); err == nil {
					t.Fatal("lucky dice reused its voice grant for a second throw")
				}
			}
		})
	}
}

func TestLuckyDiceCannotConsumeInventoryAfterAnotherProp(t *testing.T) {
	r := New(game.Options{Names: []string{"孩子", "爸爸"}, Props: true})
	var state game.State
	_ = json.Unmarshal(r.Snapshot().State, &state)
	view, err := r.Do(Command{ID: "first-card", GameID: state.ID, Version: state.Version, Action: "prop", PropID: "launch", PieceID: "p0-0"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(view.State, &state)
	if _, err = r.Do(Command{ID: "illegal-luck", GameID: state.ID, Version: state.Version, Action: "prop", PropID: "luck"}, false); err == nil {
		t.Fatal("stacked lucky dice on another prop")
	}
	if string(view.State) != string(r.Snapshot().State) {
		t.Fatal("failed lucky dice changed game or inventory")
	}
}
