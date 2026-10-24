package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/zkevindev/flytogether/internal/room"
	"net/http"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/linx"
)

func (s *server) announcement(w http.ResponseWriter, req *http.Request) {
	r := s.owned(w, req, req.PathValue("id"))
	if r == nil {
		return
	}
	var input struct {
		GameID    string                    `json:"game_id"`
		Key       string                    `json:"key"`
		Companion linx.CompanionPreferences `json:"companion"`
	}
	if err := decode(w, req, &input); err != nil {
		failure(w, err)
		return
	}
	preferences, err := input.Companion.Normalized()
	if err != nil {
		failure(w, err)
		return
	}
	key, text := "", ""
	current := func() bool {
		view := r.Snapshot()
		if view.Status != "playing" {
			return false
		}
		var state game.State
		if json.Unmarshal(view.State, &state) != nil || state.ID != input.GameID {
			return false
		}
		key, _ = linx.TurnAnnouncement(state)
		if key == "" || key != input.Key {
			return false
		}
		_, text = preferences.Announcement(state)
		return true
	}
	if !current() {
		writeJSON(w, 409, map[string]string{"message": "播报所属回合已经变化"})
		return
	}
	started := time.Now()
	entry := room.DiagnosticEvent{RoomID: r.ID, GameID: input.GameID, Kind: "tts", Source: "tts", Code: "requested"}
	defer func() { entry.DurationMS = time.Since(started).Milliseconds(); s.store.Diagnostic(entry) }()
	if text == "" {
		entry.Code = "quiet"
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.speaker == nil || !s.speaker.Config.Enabled() {
		entry.Code = "not_configured"
		writeJSON(w, 503, map[string]string{"message": "灵矽播报尚未配置"})
		return
	}
	requestedText := text
	audio, err := s.speaker.Say(req.Context(), text)
	if err != nil {
		entry.Code = "synthesis_failed"
		status := 503
		if errors.Is(err, linx.ErrSpeechBusy) {
			status = 429
		}
		writeJSON(w, status, map[string]string{"message": err.Error()})
		return
	}
	// Discard a slow synthesis result after a roll, pause, or new match.
	if !current() || text != requestedText {
		entry.Code = "stale_result"
		writeJSON(w, 409, map[string]string{"message": "播报所属回合已经变化"})
		return
	}
	entry.Code = "synthesized"
	entry.Text = text
	var snap game.State
	_ = json.Unmarshal(r.Snapshot().State, &snap)
	entry.Version = snap.Version
	writeJSON(w, 200, map[string]any{"key": key, "text": text, "audio": base64.StdEncoding.EncodeToString(audio), "sample_rate": 24000})
}
