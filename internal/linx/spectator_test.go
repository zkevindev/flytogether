package linx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
)

func TestSpectatorFactsDoNotInventMovesOrRankHiddenHistory(t *testing.T) {
	s := game.New(game.Options{Names: []string{"孩子", "家长"}, Props: true})
	s.Current = 1
	s.Players[0].Pieces[0].Progress = 12
	for _, tc := range []struct{ question, want string }{
		{"我的暗号是蓝莓，现在轮到谁", "现在轮到家长掷骰子。"},
		{"谁更接近终点", "孩子有0架抵达终点，最远飞机走到第12格；家长的飞机还都在机库"},
		{"刚才发生了什么", "目前没有记录到最近的棋步"},
		{"帮房主使用炸弹", "不能替选手"},
	} {
		fresh, _ := json.Marshal(map[string]any{"room": map[string]any{"state": s, "status": "playing"}})
		text, ok := spectatorNarration(tc.question, string(fresh))
		if !ok || !strings.Contains(text, tc.want) || strings.Contains(text, "蓝莓") {
			t.Fatalf("%s: %q", tc.question, text)
		}
	}
	if _, ok := spectatorNarration("现在轮到谁", `{"error":"连接已失效"}`); ok {
		t.Fatal("invented facts after revoked read")
	}
}
