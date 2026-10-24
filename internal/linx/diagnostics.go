package linx

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/room"
	"strings"
)

// Extract final assistant content only. Audio frames, tools and protocol details are excluded.
func responseDiagnostic(event realtimeEvent) room.DiagnosticEvent {
	var body struct {
		Response struct {
			Status string `json:"status"`
			Output []struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Transcript string `json:"transcript"`
					Text       string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		} `json:"response"`
	}
	_ = json.Unmarshal(event.Raw, &body)
	text := []string{}
	for _, item := range body.Response.Output {
		if item.Type != "message" || item.Role != "assistant" {
			continue
		}
		for _, c := range item.Content {
			if c.Transcript != "" {
				text = append(text, c.Transcript)
			} else if c.Text != "" {
				text = append(text, c.Text)
			}
		}
	}
	return room.DiagnosticEvent{Kind: "assistant_text", Ref: event.ResponseID, Text: strings.Join(text, "\n"), Code: body.Response.Status}
}
