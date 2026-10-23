package linx

import (
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestExplicitVoiceActionsUseLegalSelection(t *testing.T) {
	s := game.New(game.Options{Names: []string{"甲", "乙"}})
	if err := s.Roll(6); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"让小熊号起飞", "小熊号"} {
		a, ok := resolveVoiceAction(*s, text)
		if !ok || a.Name != "move_piece" || a.PieceID != s.Players[0].Pieces[0].ID || a.Notice != "" {
			t.Fatalf("explicit selection not resolved: %+v", a)
		}
	}
	for _, text := range []string{"飞机起飞", "让小熊号和兔子号起飞", "不要让小熊号起飞", "为什么小熊号不能起飞", "小熊号可以起飞吗"} {
		a, ok := resolveVoiceAction(*s, text)
		if !ok || a.Notice == "" || a.PieceID != "" {
			t.Fatalf("ambiguous or non-command speech selected a piece: %q %+v", text, a)
		}
	}
	if a, ok := resolveVoiceAction(*s, "我喜欢小熊号"); ok {
		t.Fatalf("casual speech was handled as an action: %+v", a)
	}
	if err := s.Move(s.Players[0].Pieces[0].ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := resolveVoiceAction(*s, "小熊号起飞"); a.Notice == "" || a.PieceID != "" {
		t.Fatal("launch of an airborne piece became a move")
	}
}

func TestVoiceCommandCannotBypassGameRules(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	if err := r.BeginSpeech(); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	a, ok := planVoiceAction(r, "让小熊号起飞")
	if !ok || a.PieceID == "" {
		t.Fatal("named command was not resolved")
	}
	_, err := r.Tool(a.Name, map[string]string{"piece_id": a.PieceID, "action_token": r.CurrentActionToken()})
	if err == nil || string(before.State) != string(r.Snapshot().State) {
		t.Fatal("launch without a rolled six changed the game")
	}
	g := wakeGate{settings: room.VoiceSettings{WakeEnabled: true, WakeWord: "起飞助手"}}
	if text := g.commandText("起飞助手，掷骰子"); text != "掷骰子" {
		t.Fatalf("wake word became part of command: %s", text)
	}
	if a, ok := planVoiceAction(r, "请帮我掷骰子"); !ok || a.Name != "roll_dice" {
		t.Fatal("dice instruction was not resolved")
	}
}
