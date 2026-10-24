package linx

import (
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestActionNarrationStatesActualCardRuleAndNames(t *testing.T) {
	r := room.New(game.Options{Names: []string{"孩子", "家长"}, Props: true})
	state, _, _ := roomGame(r)
	a, ok := resolveVoiceAction(state, "现在轮到谁")
	if !ok || !strings.Contains(a.Notice, "现在轮到孩子") {
		t.Fatal("query bypassed authoritative nickname")
	}
	before := lastEventID(state)
	_ = r.BeginSpeech()
	b := Bridge{}
	output := b.executeTool(r, functionCall{Name: "use_prop", Arguments: `{"prop_id":"launch","piece_id":"p0-0"}`, Grant: r.CurrentActionToken()})
	text := actionNarration(r, voiceAction{Name: "use_prop", PropID: "launch"}, before, output)
	if !strings.Contains(text, "从机库直接起飞到起飞位") || strings.Contains(text, "六点") {
		t.Fatal(text)
	}
	after, _, _ := roomGame(r)
	if after.Players[0].Pieces[0].Progress != 0 || after.ActiveProp != "launch_used" || after.Players[0].Stats.Takeoffs != 1 {
		t.Fatal("test did not actually move the selected plane to the launch pad")
	}
	text = actionNarration(r, voiceAction{Name: "move_piece"}, before, `{"error":"没有这个走法"}`)
	if !strings.Contains(text, "没有执行") || !strings.Contains(text, "没有这个走法") {
		t.Fatal(text)
	}
}

func TestConciseReplyOmitsTacticalMenuButKeepsFacts(t *testing.T) {
	r := room.New(game.Options{Names: []string{"孩子", "家长"}, Props: true})
	state, _, _ := roomGame(r)
	a, _ := resolveVoiceAction(state, "现在轮到谁")
	reply := actionNarration(r, a, "", actionOutput(r, a.Notice), CompanionPreferences{Style: "concise"})
	if reply != "现在轮到孩子。" {
		t.Fatal(reply)
	}
	p := CompanionPreferences{Style: "concise"}
	if p.Cue(state) != "轮到孩子掷骰子。" {
		t.Fatal("concise cue retained optional card menu")
	}
}
