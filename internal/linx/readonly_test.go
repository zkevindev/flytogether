package linx

import (
	"strings"
	"testing"
)

func TestReadOnlySessionOnlyOffersGameQueries(t *testing.T) {
	update := realtimeReadOnlySessionUpdate("test-model")
	session := update["session"].(map[string]any)
	tools := session["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "get_game_state" {
		t.Fatalf("read-only realtime session exposed write tools: %#v", tools)
	}
	if !strings.Contains(session["instructions"].(string), "只问不操作") {
		t.Fatal("read-only instructions are missing")
	}
}
