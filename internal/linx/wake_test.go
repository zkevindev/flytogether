package linx

import (
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/room"
)

func TestWakeGateConversationWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	g := wakeGate{settings: room.DefaultVoiceSettings()}
	if g.accept("请帮我掷骰子", now) || g.awake(now) {
		t.Fatal("background instruction woke the assistant")
	}
	if !g.accept("小灵，同学，帮我掷骰子", now) {
		t.Fatal("wake word with speech punctuation was ignored")
	}
	if !g.accept("让小熊号起飞", now.Add(20*time.Second)) {
		t.Fatal("follow-up inside the window was ignored")
	}
	if g.accept("再掷骰子", now.Add(51*time.Second)) {
		t.Fatal("expired conversation accepted an instruction without waking")
	}
	if !g.accept("小林同学", now.Add(time.Minute)) {
		t.Fatal("common transcription of the default name was ignored")
	}
	g.sleep()
	if !g.accept("小宁同学，请查询棋局", now.Add(time.Minute)) {
		t.Fatal("observed speech transcription variant was ignored")
	}
	g.sleep()
	if g.accept("让飞机起飞", now.Add(61*time.Second)) {
		t.Fatal("manual standby retained write intent")
	}
}

func TestWakeGateCustomWordAndFreeConversation(t *testing.T) {
	now := time.Unix(1000, 0)
	g := wakeGate{settings: room.VoiceSettings{WakeEnabled: true, WakeWord: "飞行助手"}}
	if g.accept("小灵同学，掷骰子", now) {
		t.Fatal("default word woke a custom-named assistant")
	}
	if !g.accept("飞行助手，让小熊号起飞", now) {
		t.Fatal("custom wake and command in one utterance was ignored")
	}
	g.finish(now.Add(time.Minute))
	if !g.awake(now.Add(70 * time.Second)) {
		t.Fatal("reply completion did not preserve the follow-up window")
	}
	g.settings.WakeEnabled = false
	if !g.accept("请掷骰子", now.Add(time.Hour)) {
		t.Fatal("free conversation was gated")
	}
	if g.accept("……", now) {
		t.Fatal("empty transcription should not trigger a response")
	}
}
