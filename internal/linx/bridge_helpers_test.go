package linx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestRealtimeURLValidatesProviderSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		raw, model string
		want       string
		wantErr    bool
	}{
		"plain":            {"wss://provider.test/v1/realtime", "model-a", "wss://provider.test/v1/realtime?model=model-a", false},
		"trailing slash":   {"wss://provider.test/v1/realtime/", "model-a", "wss://provider.test/v1/realtime/?model=model-a", false},
		"same model query": {"wss://provider.test/v1/realtime?model=model-a", "model-a", "wss://provider.test/v1/realtime?model=model-a", false},
		"model mismatch":   {"wss://provider.test/v1/realtime?model=old", "model-a", "", true},
		"wrong scheme":     {"https://provider.test/v1/realtime", "model-a", "", true},
		"wrong path":       {"wss://provider.test/v1/realtime/other", "model-a", "", true},
		"credentials":      {"wss://user:pass@provider.test/v1/realtime", "model-a", "", true},
		"fragment":         {"wss://provider.test/v1/realtime#x", "model-a", "", true},
		"missing host":     {"wss:///v1/realtime", "model-a", "", true},
		"garbage":          {"://broken", "model-a", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := realtimeURL(tc.raw, tc.model)
			if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
				t.Fatalf("realtimeURL(%q) = %q, %v", tc.raw, got, err)
			}
		})
	}
}

func TestRealtimeEventUnmarshalAndForwarding(t *testing.T) {
	var event realtimeEvent
	if err := json.Unmarshal([]byte(`{"type":"response.created","response":{"id":"resp-1"}}`), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "response.created" || event.ResponseID != "resp-1" || string(event.Raw) == "" {
		t.Fatalf("response id fallback: %+v", event)
	}
	if err := json.Unmarshal([]byte(`{"type":"x","response_id":"explicit"}`), &event); err != nil || event.ResponseID != "explicit" {
		t.Fatalf("explicit response id: %+v %v", event, err)
	}
	if err := json.Unmarshal([]byte(`{`), &event); err == nil {
		t.Fatal("invalid event JSON accepted")
	}
	for _, kind := range []string{
		"response.audio.delta", "response.audio.done", "response.audio_transcript.delta", "response.audio_transcript.done",
		"response.output_audio.delta", "response.output_audio.done", "response.output_audio_transcript.delta",
		"response.output_audio_transcript.done", "response.output_text.delta", "response.output_text.done",
		"conversation.item.input_audio_transcription.completed", "input_audio_buffer.speech_started",
		"input_audio_buffer.speech_stopped", "response.created", "response.done",
	} {
		if !shouldForward(kind) {
			t.Fatalf("%s should be forwarded", kind)
		}
	}
	for _, kind := range []string{"session.created", "session.updated", "response.output_item.done", "error", ""} {
		if shouldForward(kind) {
			t.Fatalf("%s should stay internal", kind)
		}
	}
}

func TestSessionUpdatePayloadsMatchTheirAudience(t *testing.T) {
	player := realtimeSessionUpdate("model-a")
	session := player["session"].(map[string]any)
	if session["model"] != "model-a" || len(session["tools"].([]map[string]any)) != 6 || session["tool_choice"] != "auto" {
		t.Fatalf("player session: %+v", session)
	}
	spectator := realtimeSpectatorSessionUpdate("model-a")
	spectatorSession := spectator["session"].(map[string]any)
	tools := spectatorSession["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "get_game_state" {
		t.Fatalf("spectator tools: %+v", tools)
	}
	if !strings.Contains(spectatorSession["instructions"].(string), "观众") {
		t.Fatal("spectator instructions lost their role")
	}
	if len(realtimeTools()) != 6 {
		t.Fatalf("tool catalog: %d", len(realtimeTools()))
	}
}

func TestRoomGameRejectsUndecodableState(t *testing.T) {
	good := room.New(game.Options{Names: []string{"甲", "乙"}})
	if _, status, ok := roomGame(good); !ok || status != "playing" {
		t.Fatalf("valid room: %s %t", status, ok)
	}
	broken := &actionSession{view: room.View{Status: "paused", State: json.RawMessage("{broken")}}
	if _, status, ok := roomGame(broken); ok || status != "paused" {
		t.Fatalf("broken room: %s %t", status, ok)
	}
}

func TestExecuteToolReportsInvalidArgumentsAndErrors(t *testing.T) {
	session := &actionSession{state: *game.New(game.Options{Names: []string{"甲", "乙"}})}
	if output := (&Bridge{}).executeTool(session, functionCall{Name: "roll_dice", Arguments: "{broken"}); !strings.Contains(output, "工具参数无效") {
		t.Fatalf("invalid arguments: %s", output)
	}
	failing := &actionSession{err: &room.Error{Code: "STALE_ACTION", Message: "expired"}, tools: map[string]any{}}
	if output := (&Bridge{}).executeTool(failing, functionCall{Name: "roll_dice", Arguments: "{}", Grant: "grant-1"}); !strings.Contains(output, "expired") || !strings.Contains(output, "操作未执行") {
		t.Fatalf("tool error: %s", output)
	}
	// A non-map tool result is encoded verbatim.
	plain := &actionSession{tools: map[string]any{"get_game_state": []string{"a"}}}
	if output := (&Bridge{}).executeTool(plain, functionCall{Name: "get_game_state"}); output != `["a"]` {
		t.Fatalf("plain result: %s", output)
	}
}
