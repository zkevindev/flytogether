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
	output := b.executeTool(r, functionCall{Name: "use_prop", Arguments: `{"prop_id":"launch"}`, Grant: r.CurrentActionToken()})
	text := actionNarration(r, voiceAction{Name: "use_prop", PropID: "launch"}, before, output)
	if !strings.Contains(text, "任意点数") || strings.Contains(text, "六点") {
		t.Fatal(text)
	}
	after, _, _ := roomGame(r)
	if after.ActiveProp != "launch" {
		t.Fatal("test did not actually use card")
	}
	text = actionNarration(r, voiceAction{Name: "move_piece"}, before, `{"error":"没有这个走法"}`)
	if !strings.Contains(text, "没有执行") || !strings.Contains(text, "没有这个走法") {
		t.Fatal(text)
	}
}
