package linx

import (
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestCompanionPreferencesRejectUnknownOptions(t *testing.T) {
	if _, err := (CompanionPreferences{Style: "shouty"}).Normalized(); err == nil {
		t.Fatal("accepted an unknown style")
	}
	if _, err := (CompanionPreferences{Frequency: "always"}).Normalized(); err == nil {
		t.Fatal("accepted an unknown frequency")
	}
	for _, p := range []CompanionPreferences{
		{Style: "warm", Frequency: "normal"},
		{Style: "coach", Frequency: "highlights"},
		{Style: "concise", Frequency: "quiet"},
	} {
		got, err := p.Normalized()
		if err != nil || got != p {
			t.Fatalf("valid preferences rejected: %+v %v", p, err)
		}
	}
	defaults, err := (CompanionPreferences{}).Normalized()
	if err != nil || defaults.Style != "warm" || defaults.Frequency != "normal" {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
}

func TestCompanionInstructionsCoverEveryStyle(t *testing.T) {
	seen := map[string]bool{}
	for _, style := range []string{"warm", "coach", "concise"} {
		text := CompanionPreferences{Style: style}.Instructions()
		if text == "" || seen[text] {
			t.Fatalf("style %q produced duplicate or empty instructions", style)
		}
		seen[text] = true
	}
	if (CompanionPreferences{Style: "unknown"}).Instructions() != (CompanionPreferences{Style: "warm"}).Instructions() {
		t.Fatal("unknown style should fall back to the warm voice")
	}
	if !strings.Contains(CompanionPreferences{Style: "coach"}.Instructions(), "教练") {
		t.Fatal("coach style missing its tone")
	}
}

func TestCompanionOpeningAndNextFrequencyRules(t *testing.T) {
	state := game.New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	normal := CompanionPreferences{Style: "warm", Frequency: "normal"}
	if line := normal.Opening(*state); line == "" {
		t.Fatal("normal opening was empty")
	}
	if line := (CompanionPreferences{Frequency: "quiet"}).Opening(*state); line != "" {
		t.Fatalf("quiet opening spoke: %q", line)
	}
	coach := CompanionPreferences{Style: "coach", Frequency: "normal"}
	if line := coach.Opening(*state); !strings.Contains(line, "不着急") {
		t.Fatalf("coach first-turn opening: %q", line)
	}
	// After the first turn the highlights style only speaks for significant moves.
	state.Turn = 2
	if line := (CompanionPreferences{Frequency: "highlights"}).Opening(*state); line != "" {
		t.Fatalf("highlights opening spoke on a quiet turn: %q", line)
	}
	state.Phase = "finished"
	if line := (CompanionPreferences{Frequency: "highlights"}).Opening(*state); line == "" {
		t.Fatal("highlights must announce the finish")
	}

	quietLine, quietMark := (CompanionPreferences{Frequency: "quiet"}).Next(*state, "")
	if quietLine != "" || quietMark == "" {
		// quiet stays silent but still advances the seen marker.
		t.Fatalf("quiet next: line=%q mark=%q", quietLine, quietMark)
	}
}

func TestCompanionCueAndTipsForAIAndLaunch(t *testing.T) {
	state := game.New(game.Options{Names: []string{"甲", "灵矽"}, Mode: "quick", AI: true})
	state.Current = 1
	if cue := (CompanionPreferences{Style: "concise"}).Cue(*state); !strings.Contains(cue, "自动行动") {
		t.Fatalf("AI cue: %q", cue)
	}
	state.Current = 0
	if cue := (CompanionPreferences{Style: "concise"}).Cue(*state); !strings.Contains(cue, "掷骰子") {
		t.Fatalf("human roll cue: %q", cue)
	}
	state.Phase = "move"
	state.Dice = 6
	state.Legal = []game.Move{{PieceID: "p0-0", Label: "小熊号"}}
	if cue := (CompanionPreferences{Style: "concise"}).Cue(*state); !strings.Contains(cue, "小熊号") {
		t.Fatalf("move cue: %q", cue)
	}
	if tip := companionTip(*state); !strings.Contains(tip, "亮起") {
		t.Fatalf("move tip: %q", tip)
	}
	state.Phase = "roll"
	state.Players[0].Pieces[0].Progress = -1
	if tip := companionTip(*state); !strings.Contains(tip, "起飞") {
		t.Fatalf("hangar tip: %q", tip)
	}
	state.PropsEnabled = true
	state.Players[0].Props["launch"] = 1
	if tip := companionTip(*state); !strings.Contains(tip, "起飞卡") {
		t.Fatalf("launch card tip: %q", tip)
	}
	state.Players[0].Pieces[0].Progress = 10
	if tip := companionTip(*state); !strings.Contains(tip, "终点") {
		t.Fatalf("finish tip: %q", tip)
	}
}

func TestCompanionAnnouncementVariants(t *testing.T) {
	state := game.New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
	key, line := (CompanionPreferences{Style: "concise"}).Announcement(*state)
	if key == "" || !strings.Contains(line, "掷骰子。") {
		t.Fatalf("concise announcement: %q %q", key, line)
	}
	key, line = (CompanionPreferences{Style: "coach"}).Announcement(*state)
	if key == "" || !strings.Contains(line, "起飞") {
		t.Fatalf("coach announcement: %q %q", key, line)
	}
	state.Turn = 3
	if key, line = (CompanionPreferences{Frequency: "highlights"}).Announcement(*state); key != "" || line != "" {
		t.Fatalf("highlights quiet turn: %q %q", key, line)
	}
	state.History = append(state.History, game.Event{ID: "moved-1", Kind: "moved", Text: "甲移动到第5格"})
	state.LastMove = &game.Move{PieceID: "p0-0", Finish: true}
	if key, line = (CompanionPreferences{Frequency: "highlights"}).Announcement(*state); line == "" || !strings.Contains(line, "甲移动到第5格") {
		t.Fatalf("highlights important move: %q %q", key, line)
	}
	state.Phase = "finished"
	state.Winner = state.Players[0].ID
	if key, line = (CompanionPreferences{}).Announcement(*state); !strings.Contains(line, "胜利") {
		t.Fatalf("winner announcement: %q %q", key, line)
	}
}
