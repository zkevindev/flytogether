package linx

import (
	"strings"
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

func TestVoicePropSelectionIsSingleAndExplicit(t *testing.T) {
	s := game.New(game.Options{Names: []string{"甲", "乙"}, Props: true})
	for text, id := range map[string]string{"使用起飞卡": "launch", "来一张加速卡": "boost", "用幸运骰": "luck"} {
		a, ok := resolveVoiceAction(*s, text)
		if !ok || a.Name != "use_prop" || a.PropID != id {
			t.Fatalf("prop command unresolved: %s %+v", text, a)
		}
	}
	for _, text := range []string{"不要使用起飞卡", "起飞卡怎么用", "使用起飞卡和幸运骰"} {
		a, _ := resolveVoiceAction(*s, text)
		if a.PropID != "" || a.Notice == "" {
			t.Fatalf("ambiguous or negative prop speech could write: %s", text)
		}
	}
}

func TestTargetedVoicePropsRequireUnambiguousOwnerAndPlane(t *testing.T) {
	s := game.New(game.Options{Names: []string{"小朋友", "爸爸", "妈妈"}, Props: true})
	s.Players[0].Pieces[0].Progress = 30
	s.Players[1].Pieces[0].Progress = 20
	s.Players[2].Pieces[0].Progress = 10
	for text, want := range map[string]string{"用炸弹攻击爸爸的小熊号": "p1-0", "给我的小熊号加护盾": "p0-0"} {
		a, ok := resolveVoiceAction(*s, text)
		if !ok || a.Name != "use_prop" || a.PieceID != want || a.Notice != "" {
			t.Fatalf("targeted voice not resolved %s: %+v", text, a)
		}
	}
	for _, text := range []string{"使用炸弹", "用炸弹攻击小熊号", "用炸弹攻击自己的小熊号", "给爸爸的小熊号加护盾", "使用护盾保护兔子号", "别用炸弹攻击爸爸的小熊号", "我有炸弹", "推荐炸弹还是护盾", "护盾怎么用", "不用护盾", "推荐一个道具", "用炸弹攻击爸爸的小熊号和妈妈的兔子号"} {
		a, ok := resolveVoiceAction(*s, text)
		// Without an owner, two opponents with the same label must never be guessed.
		if !ok || a.Name == "use_prop" || a.Notice == "" {
			t.Fatalf("ambiguous/readonly speech selected a target %s: %+v", text, a)
		}
	}
}

func TestAdviceNamesActualTacticalTargetWithoutWriting(t *testing.T) {
	s := game.New(game.Options{Names: []string{"小朋友", "爸爸"}, Props: true, Demo: true})
	before := s.Version
	a, ok := resolveVoiceAction(*s, "推荐一个道具")
	if !ok || a.Name != "get_game_state" || a.PropID != "" || !strings.Contains(a.Notice, "爸爸的小熊号") || s.Version != before {
		t.Fatal("advice is not based on actual target or attempted a write")
	}
}
