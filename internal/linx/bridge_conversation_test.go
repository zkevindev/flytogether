package linx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func protocolState(t *testing.T, r *room.Room) game.State {
	t.Helper()
	var s game.State
	if err := json.Unmarshal(r.Snapshot().State, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBridgeProtocolHostCancellationAndClickNarration(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	h := newProtocolHarness(t, r, false, "normal")
	readUntil(t, h.ctx, h.browser, "host_cue")
	spectatorRequest(t, h.ctx, h.requests, "response.create")
	h.control(t, map[string]any{"type": "companion_preferences", "frequency": "quiet", "style": "coach"})
	readUntil(t, h.ctx, h.browser, "host_cancel")
	readUntil(t, h.ctx, h.browser, "companion_updated")
	spectatorRequest(t, h.ctx, h.requests, "response.cancel")
	h.control(t, map[string]any{"type": "companion_preferences", "frequency": "normal", "style": "warm"})
	readUntil(t, h.ctx, h.browser, "companion_updated")
	before := protocolState(t, r)
	if _, err := r.Do(room.Command{ID: "click-roll", GameID: before.ID, Version: before.Version, Action: "roll"}, false); err != nil {
		t.Fatal(err)
	}
	cue := readUntil(t, h.ctx, h.browser, "host_cue")
	if text, ok := cue["text"].(string); !ok || text == "" {
		t.Fatalf("missing click narration: %v", cue)
	}
	spectatorRequest(t, h.ctx, h.requests, "response.create")
	h.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
	readUntil(t, h.ctx, h.browser, "speech_started")
	spectatorRequest(t, h.ctx, h.requests, "response.cancel")
	h.control(t, map[string]any{"type": "abort"})
	readUntil(t, h.ctx, h.browser, "wake_state")
	if r.CurrentActionToken() != "" {
		t.Fatal("abort retained grant")
	}
}

func TestBridgeProtocolMissingSessionAcknowledgementTimesOut(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}})
	h := newProtocolHarness(t, r, false, "quiet")
	// Drain the initial configuration, then withhold the next acknowledgement.
	spectatorRequest(t, h.ctx, h.requests, "session.update")
	h.holdAck.Store(true)
	h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "timeout-input", "transcript": "讲一个故事"})
	readUntil(t, h.ctx, h.browser, "stt")
	spectatorRequest(t, h.ctx, h.requests, "session.update")
	e := readUntil(t, h.ctx, h.browser, "error")
	if !strings.Contains(e["message"].(string), "配置暂未确认") {
		t.Fatal(e)
	}
	if r.CurrentActionToken() != "" {
		t.Fatal("ack timeout retained grant")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	found := false
	for _, event := range h.traces {
		found = found || event.Code == "session_ack_timeout"
	}
	if !found {
		t.Fatal("ack timeout diagnostic absent")
	}
}

func TestBridgeProtocolClarifiesLaunchThenCommitsOnlyChosenPlane(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}, Props: true})
	h := newProtocolHarness(t, r, false, "quiet")
	input := func(id, text string) map[string]any {
		h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id, "transcript": text})
		readUntil(t, h.ctx, h.browser, "stt")
		receipt := readUntil(t, h.ctx, h.browser, "game_action")
		spectatorRequest(t, h.ctx, h.requests, "response.create")
		h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
		readUntil(t, h.ctx, h.browser, "response.created")
		h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		readUntil(t, h.ctx, h.browser, "response.done")
		return receipt
	}
	before := protocolState(t, r)
	clarification := input("clarify", "用起飞卡")
	if !strings.Contains(clarification["text"].(string), "名字") {
		t.Fatalf("no target clarification: %v", clarification)
	}
	if got := protocolState(t, r); got.Version != before.Version || got.Players[0].Props["launch"] != 1 {
		t.Fatal("ambiguous launch mutated state")
	}
	receipt := input("choose", "小熊号")
	if receipt["action"] != "use_prop" {
		t.Fatal(receipt)
	}
	after := protocolState(t, r)
	if after.Players[0].Pieces[0].Progress != 0 || after.Players[0].Pieces[1].Progress != -1 || after.Players[0].Props["launch"] != 0 || after.Players[0].Stats.Rolls != 0 {
		t.Fatalf("incorrect launch: %+v", after)
	}
}
