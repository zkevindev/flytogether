package linx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticAssistantExtractionExcludesProtocolAndAudio(t *testing.T) {
	var event realtimeEvent
	data := `{"type":"response.done","response":{"id":"response-one","status":"completed","output":[{"type":"function_call","arguments":"secret-token"},{"type":"message","role":"assistant","content":[{"type":"audio","audio":"base64-raw-audio","transcript":"真实投出3点"},{"type":"text","text":"请选择飞机"}]},{"type":"message","role":"user","content":[{"text":"not-assistant"}]}]}}`
	if e := json.Unmarshal([]byte(data), &event); e != nil {
		t.Fatal(e)
	}
	out := responseDiagnostic(event)
	if out.Ref != "response-one" || out.Code != "completed" || out.Text != "真实投出3点\n请选择飞机" {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out)
	for _, s := range []string{"secret-token", "base64-raw-audio", "not-assistant"} {
		if strings.Contains(string(b), s) {
			t.Fatal("raw payload persisted")
		}
	}
}
