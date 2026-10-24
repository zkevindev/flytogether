package linx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

type actionSession struct {
	state game.State
	view  room.View
	tools map[string]any
	err   error
}

func (s *actionSession) Snapshot() room.View {
	if s.view.State == nil {
		s.view = room.View{Status: "playing", State: json.RawMessage(mustJSON(s.state))}
	}
	return s.view
}
func (s *actionSession) VoiceSettings() room.VoiceSettings { return room.DefaultVoiceSettings() }
func (s *actionSession) BeginSpeech() error                { return nil }
func (s *actionSession) CurrentActionToken() string        { return "" }
func (s *actionSession) VoiceState(string)                 {}
func (s *actionSession) Tool(name string, args map[string]string) (any, error) {
	if s.err != nil {
		return nil, s.err
	}
	if value, ok := s.tools[name]; ok {
		return value, nil
	}
	return map[string]any{"state": "ok"}, nil
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func rollPhaseState() game.State {
	return *game.New(game.Options{Names: []string{"甲", "乙"}, Mode: "quick"})
}

func movePhaseState(t *testing.T) game.State {
	t.Helper()
	state := rollPhaseState()
	if err := state.Roll(6); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestResolveVoiceActionAnswersQuestionsWithoutActing(t *testing.T) {
	state := rollPhaseState()
	for text, want := range map[string]string{
		"轮到谁了":   "现在轮到甲",
		"规则是什么":  "6点起飞",
		"你好":     "请掷骰子",
		"帮我推荐一下": "还没有执行操作",
		"我掷过几点":  "没有执行操作",
		"不要用道具":  "先不这么做",
		"为什么":    "请掷骰子",
		"可以走吗":   "请掷骰子",
		"现在怎么样":  "请掷骰子",
	} {
		action, handled := resolveVoiceAction(state, text)
		if !handled || action.Name != "get_game_state" || !strings.Contains(action.Notice, want) {
			t.Fatalf("%q -> %+v handled=%t", text, action, handled)
		}
	}
	if _, handled := resolveVoiceAction(state, "随便聊聊"); handled {
		t.Fatal("plain conversation was treated as a game action")
	}
	requested, handled := resolveVoiceAction(state, "帮我掷骰子")
	if !handled || requested.Name != "roll_dice" {
		t.Fatalf("test phrase did not resolve to a roll: %+v", requested)
	}
	blocked := readonlyAction(state, requested)
	if blocked.Name != "get_game_state" || !strings.Contains(blocked.Notice, "这次没有执行棋局操作") {
		t.Fatalf("read-only voice did not clearly refuse the action: %+v", blocked)
	}
	// A finished match answers turn questions differently.
	finished := state
	finished.Phase = "finished"
	finished.Winner = finished.Players[1].ID
	if action, handled := resolveVoiceAction(finished, "轮到谁"); !handled || !strings.Contains(action.Notice, "赢") {
		t.Fatalf("finished turn question: %+v", action)
	}
	// A called die outside the roll phase is spoken as a cheer, never rolled.
	move := movePhaseState(t)
	move.Dice = 4
	if action, handled := resolveVoiceAction(move, "我要六点"); !handled || action.Cheer != 6 || !strings.Contains(action.Notice, "已经掷过了") {
		t.Fatalf("cheer after roll: %+v", action)
	}
	if action, handled := resolveVoiceAction(move, "赌三"); !handled || action.Cheer != 3 {
		t.Fatalf("cheer wording: %+v", action)
	}
}

func TestResolveVoiceActionPropsNeedExplicitTargets(t *testing.T) {
	base := rollPhaseState()
	base.PropsEnabled = true
	base.Players[0].Props = map[string]int{"launch": 1, "bomb": 1, "shield": 1, "boost": 1, "luck": 1}
	base.Players[1].Pieces[0].Progress = 6

	if action, _ := resolveVoiceAction(base, "提一下炸弹"); !strings.Contains(action.Notice, "只是提及") {
		t.Fatalf("mention only: %+v", action)
	}
	if action, _ := resolveVoiceAction(base, "用护盾保护对方"); !strings.Contains(action.Notice, "目标所属玩家不合法") {
		t.Fatalf("shield on opponent: %+v", action)
	}
	if action, _ := resolveVoiceAction(base, "用炸弹炸我的"); !strings.Contains(action.Notice, "目标所属玩家不合法") {
		t.Fatalf("bomb on self: %+v", action)
	}
	if action, _ := resolveVoiceAction(base, "用炸弹和护盾"); !strings.Contains(action.Notice, "一次只能用一张") {
		t.Fatalf("two props: %+v", action)
	}
	// A complete bomb instruction with one unambiguous target executes.
	if action, handled := resolveVoiceAction(base, "用炸弹炸乙的小熊号"); !handled || action.Name != "use_prop" || action.PropID != "bomb" || action.PieceID != "p1-0" {
		t.Fatalf("bomb target: %+v", action)
	}
	// Without a named target the notice lists the legal choices.
	if action, _ := resolveVoiceAction(base, "用炸弹"); action.Name != "get_game_state" || !strings.Contains(action.Notice, "乙的小熊号") {
		t.Fatalf("bomb without target: %+v", action)
	}
	// Two named labels are ambiguous.
	base.Players[1].Pieces[1].Progress = 6
	if action, _ := resolveVoiceAction(base, "用炸弹炸小熊号和兔子号"); !strings.Contains(action.Notice, "一次只能选择一架目标飞机") {
		t.Fatalf("two labels: %+v", action)
	}
	// Non-targeted props apply directly.
	for text, prop := range map[string]string{"用加速卡": "boost", "用幸运骰": "luck"} {
		if action, handled := resolveVoiceAction(base, text); !handled || action.Name != "use_prop" || action.PropID != prop {
			t.Fatalf("%q -> %+v", text, action)
		}
	}
	// Launch card explains the hangar choices instead of guessing.
	if action, _ := resolveVoiceAction(base, "用起飞卡"); action.Name != "get_game_state" || !action.Pending || !strings.Contains(action.Notice, "请说出自己一架机库飞机的名字") {
		t.Fatalf("launch without piece: %+v", action)
	}
	if action, handled := resolveVoiceAction(base, "用起飞卡让小熊号起飞"); !handled || action.Name != "use_prop" || action.PieceID != "p0-0" {
		t.Fatalf("launch target: %+v", action)
	}
}

func TestPendingLaunchChoiceScopesBarePlaneNames(t *testing.T) {
	state := rollPhaseState()
	state.PropsEnabled = true
	state.Players[0].Props = map[string]int{"launch": 1}
	if action, handled := pendingLaunchChoice(&actionSession{state: state}, "小熊号"); !handled || action.Name != "use_prop" || action.PieceID != "p0-0" {
		t.Fatalf("bare launch choice: %+v", action)
	}
	if _, handled := pendingLaunchChoice(&actionSession{state: state}, "不要小熊号"); handled {
		t.Fatal("cancelled launch choice was executed")
	}
	if action, handled := pendingLaunchChoice(&actionSession{state: state}, "小熊号和兔子号"); !handled || !action.Pending || action.Name != "get_game_state" {
		t.Fatalf("two-plane launch choice: %+v", action)
	}
	// An unknown plane name is not a launch choice.
	if _, handled := pendingLaunchChoice(&actionSession{state: state}, "大飞机"); handled {
		t.Fatal("unknown plane name consumed the launch card")
	}
	// Outside the launch clarification the bare name is not a prop command.
	for name, mutate := range map[string]func(*game.State){
		"no props":       func(s *game.State) { s.PropsEnabled = false },
		"move phase":     func(s *game.State) { s.Phase = "move" },
		"no launch card": func(s *game.State) { s.Players[0].Props = map[string]int{} },
		"active prop":    func(s *game.State) { s.ActiveProp = "launch_used" },
	} {
		other := state
		mutate(&other)
		if _, handled := pendingLaunchChoice(&actionSession{state: other}, "小熊号"); handled {
			t.Fatalf("%s: bare name consumed the launch card", name)
		}
	}
}

func TestPlanVoiceActionFallsBackToClarification(t *testing.T) {
	roll := rollPhaseState()
	if action, handled := planVoiceAction(&actionSession{state: roll}, "色子"); !handled || !strings.Contains(action.Notice, "明确说") {
		t.Fatalf("roll phase clarification: %+v", action)
	}
	move := movePhaseState(t)
	if action, handled := planVoiceAction(&actionSession{state: move}, "操作"); !handled || !strings.Contains(action.Notice, "请先选择一架高亮飞机") {
		t.Fatalf("move phase clarification: %+v", action)
	}
	finished := roll
	finished.Phase = "finished"
	if action, handled := planVoiceAction(&actionSession{state: finished}, "继续"); !handled || !strings.Contains(action.Notice, "本局已经结束") {
		t.Fatalf("finished clarification: %+v", action)
	}
	if _, handled := planVoiceAction(&actionSession{state: roll}, "今天天气不错"); handled {
		t.Fatal("small talk forced a game clarification")
	}
	// A state that cannot be decoded is never acted on.
	broken := &actionSession{view: room.View{State: json.RawMessage("{broken")}}
	if _, handled := planVoiceAction(broken, "掷骰子"); handled {
		t.Fatal("undecodable state produced an action")
	}
}

func TestActionOutputRemovesActionTokens(t *testing.T) {
	session := &actionSession{tools: map[string]any{"get_game_state": map[string]any{"action_token": "secret", "state": "ok"}}}
	output := actionOutput(session, "本轮没有执行操作")
	if strings.Contains(output, "secret") || !strings.Contains(output, "本轮没有执行操作") {
		t.Fatalf("action output leaked authority: %s", output)
	}
}

func TestIntentMovesAndMentions(t *testing.T) {
	state := movePhaseState(t)
	state.Current = 0
	state.Dice = 3
	state.Players[1].Pieces[0].Progress = 5
	state.Players[1].Pieces[1].Progress = 9
	state.Legal = []game.Move{
		{PieceID: "p0-1", Label: "兔子号", From: 3, To: 6, Summary: "前进"},
		{PieceID: "p0-0", Label: "小熊号", From: 9, To: 12, Summary: "前进"},
	}
	// Leading/finish intents resolve to the single best matching piece.
	state.Legal[0].From, state.Legal[1].From = 9, 3
	if candidates, ok := intentMoves(state, "走最前面的"); !ok || len(candidates) != 1 || candidates[0].PieceID != "p0-1" {
		t.Fatalf("leading: %+v", candidates)
	}
	if candidates, ok := intentMoves(state, "走最后"); !ok || candidates[0].PieceID != "p0-0" {
		t.Fatalf("trailing: %+v", candidates)
	}
	state.Legal[0].Finish = true
	if candidates, _ := intentMoves(state, "回家"); len(candidates) != 1 || candidates[0].PieceID != "p0-1" {
		t.Fatalf("finish intent: %+v", candidates)
	}
	// Capture intent without any capture is an explanation, not a move.
	if candidates, ok := intentMoves(state, "撞乙"); !ok || len(candidates) != 0 {
		t.Fatalf("capture without target: %+v", candidates)
	}
	state.Legal[0].Captured = []string{"p1-0"}
	state.Legal[1].Captured = []string{"p1-1"}
	if candidates, _ := intentMoves(state, "撞乙"); len(candidates) != 2 {
		t.Fatalf("unfiltered captures: %+v", candidates)
	}
	if candidates, _ := intentMoves(state, "撞小熊号"); len(candidates) != 1 || candidates[0].PieceID != "p0-1" {
		t.Fatalf("label-filtered captures: %+v", candidates)
	}
	if candidates, _ := intentMoves(state, "撞丙"); len(candidates) != 2 {
		t.Fatalf("unknown opponent should not filter: %+v", candidates)
	}
	// A shield turns the capture into an explanation.
	state.Legal = []game.Move{{PieceID: "p0-0", Label: "小熊号", Blocked: []string{"p1-0"}}}
	if notice := intentMiss(state, "撞乙"); !strings.Contains(notice, "护盾") {
		t.Fatalf("blocked notice: %q", notice)
	}
	state.Legal = nil
	if notice := intentMiss(state, "撞乙"); !strings.Contains(notice, "撞不到乙") {
		t.Fatalf("miss notice: %q", notice)
	}
	if notice := intentMiss(state, "回家"); !strings.Contains(notice, "没有飞机能正好到家") {
		t.Fatalf("finish miss notice: %q", notice)
	}
	if notice := intentMiss(state, "随便"); !strings.Contains(notice, "甲") {
		t.Fatalf("default miss notice: %q", notice)
	}
}

func TestMoveCueRollCueAndHostCue(t *testing.T) {
	state := movePhaseState(t)
	state.Dice = 6
	// Multiple legal moves with one capture.
	state.Legal = []game.Move{
		{PieceID: "p0-0", Label: "小熊号", To: 6, Summary: "前进"},
		{PieceID: "p0-1", Label: "兔子号", To: 9, Summary: "前进", Captured: []string{"p1-0"}},
	}
	state.Players[1].Pieces[0].Progress = 5
	if cue := hostCue(state); !strings.Contains(cue, "撞回去") {
		t.Fatalf("capture cue: %q", cue)
	}
	state.Legal = []game.Move{{PieceID: "p0-0", Label: "小熊号", To: 3, Summary: "前进"}}
	if cue := moveCue(state, "甲"); !strings.Contains(cue, "回「好」就飞") {
		t.Fatalf("single move cue: %q", cue)
	}
	state.Legal = nil
	if cue := moveCue(state, "甲"); !strings.Contains(cue, "没有飞机能走") {
		t.Fatalf("empty move cue: %q", cue)
	}
	state.Legal = []game.Move{{PieceID: "p0-0", Label: "小熊号", To: 3}, {PieceID: "p0-1", Label: "兔子号", To: 4}}
	if cue := moveCue(state, "甲"); !strings.Contains(cue, "请说飞机名字") {
		t.Fatalf("choice cue: %q", cue)
	}
	// A host player lost from the state still yields a safe answer.
	broken := state
	broken.Current = 5
	if cue := hostCue(broken); !strings.Contains(cue, "准备") {
		t.Fatalf("invalid current cue: %q", cue)
	}
	broken.Current = 0
	broken.Phase = "finished"
	broken.Winner = ""
	if cue := hostCue(broken); !strings.Contains(cue, "有人赢了") {
		t.Fatalf("unknown winner cue: %q", cue)
	}
	broken.Winner = "team"
	if cue := hostCue(broken); !strings.Contains(cue, "星河救援成功") {
		t.Fatalf("cooperative winner cue: %q", cue)
	}
	broken.Phase = "roll"
	broken.Players[0].AI = true
	if cue := hostCue(broken); !strings.Contains(cue, "等它飞完") {
		t.Fatalf("AI cue: %q", cue)
	}
	broken.Players[0].AI = false
	broken.PropsEnabled = true
	broken.Players[0].Props = map[string]int{"launch": 1, "boost": 1, "luck": 1}
	for _, tc := range []struct {
		want, prop string
		progress   int
	}{
		{"起飞卡", "launch", -1},
		{"加速卡", "boost", 5},
		{"幸运骰", "luck", -1},
	} {
		broken.Players[0].Props = map[string]int{tc.prop: 1}
		broken.Players[0].Pieces[0].Progress = tc.progress
		if cue := rollCue(broken, "甲"); !strings.Contains(cue, tc.want) {
			t.Fatalf("%s cue: %q", tc.prop, cue)
		}
	}
	empty := game.State{}
	if cue := hostCue(empty); cue != "棋局还在准备。" {
		t.Fatalf("empty host cue: %q", cue)
	}
}

func TestNextHostLineDramasAndMarkers(t *testing.T) {
	state := rollPhaseState()
	if line, mark := nextHostLine(state, ""); line != "" || mark != lastEventID(state) {
		t.Fatalf("first line marker: %q %q", line, mark)
	}
	state.History = append(state.History,
		game.Event{ID: "e1", Kind: "rolled", Text: "甲掷出6点"},
		game.Event{ID: "e2", Kind: "moved", Text: "小熊号飞到第6格"},
		game.Event{ID: "e3", Kind: "moved", Text: "兔子号撞回机场"},
		game.Event{ID: "e4", Kind: "won", Text: "甲赢了"},
		game.Event{ID: "e5", Kind: "prop", Text: "使用护盾"},
	)
	line, mark := nextHostLine(state, "e1")
	if line == "" || mark != "e5" || strings.Count(line, "。") < 2 {
		t.Fatalf("drama line: %q %q", line, mark)
	}
	if line, mark = nextHostLine(state, "e5"); line != "" || mark != "e5" {
		t.Fatalf("no new events: %q %q", line, mark)
	}
	// Only the tail of the feed is used when the marker is unknown.
	if events := eventsAfter(state.History, "missing"); len(events) != 1 {
		t.Fatalf("unknown marker events: %+v", events)
	}
	if last := lastEventID(state); last != "e5" {
		t.Fatalf("last event: %q", last)
	}
	if eventsAfter(nil, "e1") != nil || eventsAfter(state.History, "") != nil {
		t.Fatal("empty history or marker should produce no events")
	}
	// A rolled event alone still produces the host cue when it is a human turn.
	rolledOnly := rollPhaseState()
	rolledOnly.History = []game.Event{{ID: "r1", Kind: "rolled", Text: "甲掷出3点"}}
	if line, _ := nextHostLine(rolledOnly, "r1"); line != "" {
		t.Fatalf("seen marker repeated a line: %q", line)
	}
	rolledOnly.History = append(rolledOnly.History, game.Event{ID: "r2", Kind: "rolled", Text: "甲掷出5点"})
	if line, _ := nextHostLine(rolledOnly, "r1"); !strings.Contains(line, "请掷骰子") && !strings.Contains(line, "甲") {
		t.Fatalf("rolled cue: %q", line)
	}
	// AI turns stay quiet unless a drama happened.
	ai := rollPhaseState()
	ai.History = []game.Event{{ID: "a1", Kind: "rolled", Text: "灵矽掷出2点"}}
	ai.Current = 1
	ai.Players[1].AI = true
	if line, _ := nextHostLine(ai, "a0"); line != "" {
		t.Fatalf("AI turn narration: %q", line)
	}
	if !dramatic("兔子号撞回机场") || dramatic("普通前进") {
		t.Fatal("dramatic detection")
	}
}

func TestCheerTakesItsRolledEventsOnly(t *testing.T) {
	state := movePhaseState(t)
	before := state.History[0].ID
	if note := (*Cheer)(nil).take(state); note != "" {
		t.Fatal("nil cheer spoke")
	}
	cheer := &Cheer{Die: 6, After: lastEventID(state)}
	if note := cheer.take(state); note != "" {
		t.Fatalf("cheer without a new roll: %q", note)
	}
	state.History = append(state.History, game.Event{ID: "roll-1", Kind: "rolled", Text: "掷出6点"})
	state.Dice = 6
	cheer = &Cheer{Die: 6, After: before}
	if note := cheer.take(state); !strings.Contains(note, "喊中了") || cheer.Die != 0 {
		t.Fatalf("matching cheer: %q die=%d", note, cheer.Die)
	}
	miss := &Cheer{Die: 2, After: before}
	state.Dice = 5
	if note := miss.take(state); !strings.Contains(note, "实际是5点") {
		t.Fatalf("mismatched cheer: %q", note)
	}
}

func TestTacticalAdviceExplainsWithoutActing(t *testing.T) {
	finished := rollPhaseState()
	finished.Phase = "finished"
	if advice := tacticalAdvice(finished); !strings.Contains(advice, "没有执行操作") {
		t.Fatalf("finished advice: %q", advice)
	}
	move := movePhaseState(t)
	if advice := tacticalAdvice(move); !strings.Contains(advice, "建议走") || !strings.Contains(advice, "还没有") && !strings.Contains(advice, "只是建议") {
		t.Fatalf("move advice: %q", advice)
	}
	// A move phase without any legal move falls back to the prop advice path.
	noMoves := move
	noMoves.Legal = nil
	if advice := tacticalAdvice(noMoves); advice == "" || !strings.Contains(advice, "还没有执行操作") {
		t.Fatalf("prop advice: %q", advice)
	}
}

func TestNarrationFactsPreferFreshEvents(t *testing.T) {
	state := movePhaseState(t)
	if facts := narrationFacts(state, lastEventID(state)); !strings.Contains(facts, "没有发生新的掷骰") {
		t.Fatalf("no fresh roll facts: %q", facts)
	}
	started := state.History[0].ID
	state.History = append(state.History,
		game.Event{ID: "r1", Kind: "rolled", Text: "甲掷出6点", DiceRolls: []int{6}},
		game.Event{ID: "m1", Kind: "moved", Text: "小熊号前进6格"},
	)
	if facts := narrationFacts(state, started); !strings.Contains(facts, "甲掷出6点") || !strings.Contains(facts, "小熊号前进6格") {
		t.Fatalf("fresh roll facts: %q", facts)
	}
}
