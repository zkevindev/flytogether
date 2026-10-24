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

func TestVoiceIntentsChooseWithoutNamingCoordinates(t *testing.T) {
	frontState := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	frontState.Players[0].Pieces[0].Progress = 10
	frontState.Players[0].Pieces[1].Progress = 30
	if err := frontState.Roll(1); err != nil {
		t.Fatal(err)
	}
	front, ok := resolveVoiceAction(*frontState, "走最前面")
	if !ok || front.PieceID != "p0-1" || front.Notice != "" {
		t.Fatalf("front plane not chosen: %+v", front)
	}
	tiedState := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	tiedState.Players[0].Pieces[0].Progress = 10
	tiedState.Players[0].Pieces[1].Progress = 10
	if err := tiedState.Roll(1); err != nil {
		t.Fatal(err)
	}
	tied, ok := resolveVoiceAction(*tiedState, "走最前面")
	if !ok || tied.PieceID != "" || tied.Notice == "" {
		t.Fatalf("tied front planes were guessed: %+v", tied)
	}

	fight := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	fight.Players[0].Pieces[0].Progress = 20
	fight.Players[0].Pieces[1].Progress = 4
	fight.Players[1].Pieces[0].Progress = 10
	if err := fight.Roll(3); err != nil {
		t.Fatal(err)
	}
	bump, ok := resolveVoiceAction(*fight, "撞爸爸")
	if !ok || bump.Name != "move_piece" || bump.PieceID != "p0-0" || bump.Notice != "" {
		t.Fatalf("capture intent missed: %+v legal=%+v", bump, fight.Legal)
	}
	home := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	home.Players[0].Pieces[0].Progress = 55
	home.Players[0].Pieces[1].Progress = 10
	if err := home.Roll(3); err != nil {
		t.Fatal(err)
	}
	chosen, ok := resolveVoiceAction(*home, "你来走")
	if !ok || chosen.PieceID != "p0-0" {
		t.Fatalf("delegated move was not the best plane: %+v", chosen)
	}
}

func TestVoiceBombPhraseAndDiceCheerDoNotInventResults(t *testing.T) {
	s := game.New(game.Options{Names: []string{"小朋友", "爸爸"}, Props: true})
	s.Players[0].Pieces[0].Progress = 30
	s.Players[1].Pieces[0].Progress = 20
	bomb, ok := resolveVoiceAction(*s, "炸爸爸的小熊号")
	if !ok || bomb.Name != "use_prop" || bomb.PropID != "bomb" || bomb.PieceID != "p1-0" {
		t.Fatalf("spoken bomb missed: %+v", bomb)
	}
	cheer, ok := resolveVoiceAction(*s, "我要六点")
	if !ok || cheer.Name != "roll_dice" || cheer.Cheer != 6 || cheer.Notice != "" {
		t.Fatalf("calling a number should roll without changing the die: %+v", cheer)
	}
	both, ok := resolveVoiceAction(*s, "我要六点掷骰子")
	if !ok || both.Name != "roll_dice" || both.Cheer != 6 {
		t.Fatalf("called roll was not kept: %+v", both)
	}
	before := s.Version
	if _, ok := resolveVoiceAction(*s, "我喜欢小熊号"); ok || s.Version != before {
		t.Fatal("casual speech became a command")
	}
}

func TestShortReplyPlaysTheOfferedTurn(t *testing.T) {
	roll := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	yes, ok := resolveVoiceAction(*roll, "好")
	if !ok || yes.Name != "roll_dice" || yes.Notice != "" {
		t.Fatalf("agree did not roll: %+v", yes)
	}
	if refused, ok := resolveVoiceAction(*roll, "我不要六点"); !ok || refused.Name == "roll_dice" {
		t.Fatalf("negated cheer rolled: %+v", refused)
	}
	if asked, ok := resolveVoiceAction(*roll, "怎么掷骰子"); !ok || asked.Name == "roll_dice" {
		t.Fatalf("a how-to question rolled: %+v", asked)
	}

	fight := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	fight.Players[0].Pieces[0].Progress = 20
	fight.Players[0].Pieces[1].Progress = 4
	fight.Players[1].Pieces[0].Progress = 10
	if err := fight.Roll(3); err != nil {
		t.Fatal(err)
	}
	polite, ok := resolveVoiceAction(*fight, "撞爸爸吗")
	if !ok || polite.Name != "move_piece" || polite.PieceID != "p0-0" || polite.Notice != "" {
		t.Fatalf("polite capture missed: %+v", polite)
	}
	unsure, ok := resolveVoiceAction(*fight, "好")
	if !ok || unsure.PieceID != "" || unsure.Notice == "" {
		t.Fatalf("agree guessed among several planes: %+v", unsure)
	}

	only := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	only.Phase = "move"
	only.Dice = 2
	only.Legal = []game.Move{{PieceID: "p0-1", Label: "兔子号", Summary: "前进 2 格"}}
	goOn, ok := resolveVoiceAction(*only, "好呀")
	if !ok || goOn.Name != "move_piece" || goOn.PieceID != "p0-1" || goOn.Notice != "" {
		t.Fatalf("agree did not take the only plane: %+v", goOn)
	}
}

func TestHostLineSpeaksOnHumanTurnAndDramaticHits(t *testing.T) {
	s := game.New(game.Options{Names: []string{"小朋友", "爸爸"}})
	if script, _ := nextHostLine(*s, ""); script != "" {
		t.Fatalf("opening baseline should stay quiet: %s", script)
	}
	seen := lastEventID(*s)
	s.History = append(s.History, game.Event{ID: "hit", Kind: "moved", Text: "小朋友的小熊号：前进 3 格，送 1 架飞机回机场"})
	s.Current = 1
	script, mark := nextHostLine(*s, seen)
	if !strings.Contains(script, "回机场") || !strings.Contains(script, "爸爸") || mark != "hit" {
		t.Fatalf("dramatic handoff missing: %s", script)
	}
	cue := hostCue(*s)
	if !strings.Contains(cue, "爸爸") || !strings.Contains(cue, "掷骰子") {
		t.Fatal(cue)
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

func TestLuckyDiceVoiceAndNarrationUseOnlyNewRollEvents(t *testing.T) {
	state := game.New(game.Options{Names: []string{"孩子", "爸爸"}, Props: true})
	action, ok := resolveVoiceAction(*state, "帮我使用幸运骰")
	if !ok || action.Name != "use_prop" || action.PropID != "luck" || action.Notice != "" {
		t.Fatalf("lucky dice utterance missed: %+v", action)
	}
	for _, text := range []string{"帮我使用幸运头。", "帮我使用幸运投"} {
		a, ok := resolveVoiceAction(*state, text)
		if !ok || a.Name != "use_prop" || a.PropID != "luck" {
			t.Fatalf("ASR lucky alias missed: %s: %+v", text, a)
		}
	}
	for _, text := range []string{"不要使用幸运头", "幸运头怎么用"} {
		a, ok := resolveVoiceAction(*state, text)
		if !ok || a.Name != "get_game_state" {
			t.Fatalf("ASR alias bypassed no-write guard: %s: %+v", text, a)
		}
	}
	state.Dice = 6
	state.History = append(state.History, game.Event{ID: "past", Kind: "rolled", Text: "上次掷出了6点", DiceRolls: []int{6}})
	if text := narrationFacts(*state, "past"); !strings.Contains(text, "没有发生新的掷骰") {
		t.Fatal("historical die could be narrated as a new roll")
	}
	state.History = append(state.History, game.Event{ID: "new", Kind: "rolled", Text: "幸运骰掷出2和5，取5点", DiceRolls: []int{2, 5}})
	if text := narrationFacts(*state, "past"); strings.Contains(text, "上次掷出了6点") || !strings.Contains(text, "2和5") || !strings.Contains(text, "必须首先完整播报") {
		t.Fatal("narration mixed old and current dice")
	}
}
