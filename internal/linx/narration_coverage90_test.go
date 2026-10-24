package linx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestNarrationCoverage90SpectatorLifecycleAndProgress(t *testing.T) {
	state := *game.New(game.Options{Names: []string{"甲", "乙"}})
	encode := func(status string, recent []string) string {
		b, err := json.Marshal(map[string]any{"room": map[string]any{"status": status, "state": state}, "recent_events": recent})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range []struct{ status, want string }{{"waiting", "等待开局"}, {"paused", "比赛暂停"}, {"ended", "房主已经结束"}, {"playing", "甲掷骰子"}} {
		if text, ok := spectatorNarration("轮到谁", encode(tc.status, nil)); !ok || !strings.Contains(text, tc.want) {
			t.Fatalf("%s: %q %t", tc.status, text, ok)
		}
	}
	state.Phase = "move"
	state.Dice = 4
	if text, ok := spectatorNarration("谁的回合", encode("playing", nil)); !ok || !strings.Contains(text, "4点") {
		t.Fatalf("move: %q %t", text, ok)
	}
	state.Winner = state.Players[1].ID
	if text, ok := spectatorNarration("轮到谁", encode("playing", nil)); !ok || !strings.Contains(text, "乙已经获胜") {
		t.Fatalf("winner: %q %t", text, ok)
	}
	state.Winner = "unknown"
	state.Current = -1
	if _, ok := spectatorNarration("轮到谁", encode("playing", nil)); ok {
		t.Fatal("invalid current player produced a turn answer")
	}
	state.Current = 0
	state.Players[0].Pieces[0].Progress = state.Board.Finish
	if text, ok := spectatorNarration("谁领先", encode("playing", nil)); !ok || !strings.Contains(text, "甲有1架抵达终点") || !strings.Contains(text, "乙的飞机还都在机库") {
		t.Fatalf("standings: %q %t", text, ok)
	}
	for _, tc := range []struct {
		recent []string
		want   string
	}{{nil, "不能猜测"}, {[]string{"历史一步", "最新一步"}, "最新一步"}} {
		if text, ok := spectatorNarration("最后一步", encode("playing", tc.recent)); !ok || !strings.Contains(text, tc.want) {
			t.Fatalf("recent: %q %t", text, ok)
		}
	}
	for _, request := range []string{"帮我使用炸弹", "帮我移动飞机", "暂停游戏", "结束游戏"} {
		if text, ok := spectatorNarration(request, encode("playing", nil)); !ok || !strings.Contains(text, "不能替选手") {
			t.Fatalf("write request: %q %t", text, ok)
		}
	}
	for _, raw := range []string{"not-json", `{"room":{"state":{}}}`} {
		if _, ok := spectatorNarration("轮到谁", raw); ok {
			t.Fatal("invalid snapshot answered")
		}
	}
	if _, ok := spectatorNarration("讲个故事", encode("playing", nil)); ok {
		t.Fatal("non-board question was replaced with board facts")
	}
}

func TestNarrationCoverage90MoveClarificationAndTactics(t *testing.T) {
	state := *game.New(game.Options{Names: []string{"甲", "乙"}, Props: true})
	if action, ok := resolveCandidates(state, nil, ""); !ok || !strings.Contains(action.Notice, "还不能走棋") {
		t.Fatal(action)
	}
	state.Phase = "move"
	options := []game.Move{{PieceID: "p0-0", Label: "小熊号"}, {PieceID: "p0-1", Label: "兔子号"}}
	if action, ok := resolveCandidates(state, options, ""); !ok || action.PieceID != "" || !strings.Contains(action.Notice, "小熊号、兔子号") {
		t.Fatal(action)
	}
	if action, ok := resolveCandidates(state, nil, "不能撞到"); !ok || action.Notice != "不能撞到" {
		t.Fatal(action)
	}
	if len(leading(nil, true)) != 0 {
		t.Fatal("empty candidates became a move")
	}
	if lastEventID(game.State{}) != "" {
		t.Fatal("empty history has an event ID")
	}
	state.Legal = []game.Move{{Blocked: []string{"missing"}, Captured: []string{"missing"}}}
	if blockedNotice(state) != "" || capturedNames(state, state.Legal[0]) != "对方" {
		t.Fatal("unknown plane ID fabricated a name")
	}
	state.Legal = []game.Move{{PieceID: "p0-0", Label: "小熊号", Finish: true}, {PieceID: "p0-1", Label: "兔子号"}}
	if text := moveCue(state, "甲"); !strings.Contains(text, "可以到家") {
		t.Fatal(text)
	}
	state.Phase = "roll"
	state.Players[1].Pieces[0].Progress = 8
	state.Players[0].Props = map[string]int{"bomb": 1}
	if text := tacticalAdvice(state); !strings.Contains(text, "建议用炸弹攻击乙") {
		t.Fatal(text)
	}
	state.Players[0].Props = map[string]int{"shield": 1}
	state.Players[0].Pieces[0].Progress = 30
	if text := tacticalAdvice(state); !strings.Contains(text, "建议给甲") {
		t.Fatal(text)
	}
	if text := rollCue(state, "甲"); !strings.Contains(text, "加护盾") {
		t.Fatal(text)
	}
	for _, tc := range []struct {
		prop, want string
		progress   int
	}{{"launch", "起飞卡", -1}, {"boost", "加速卡", 3}, {"luck", "幸运骰", -1}} {
		state.Players[0].Props = map[string]int{tc.prop: 1}
		state.Players[0].Pieces[0].Progress = tc.progress
		if text := tacticalAdvice(state); !strings.Contains(text, tc.want) {
			t.Fatalf("%s: %s", tc.prop, text)
		}
	}
}
