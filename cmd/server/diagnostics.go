package main

import (
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
	"net/http"
)

func (s *server) clientDiagnostic(w http.ResponseWriter, req *http.Request) {
	r := s.owned(w, req, req.PathValue("id"))
	if r == nil {
		return
	}
	var in struct {
		GameID string `json:"game_id"`
		Code   string `json:"code"`
	}
	if err := decode(w, req, &in); err != nil {
		failure(w, err)
		return
	}
	messages := map[string]string{"microphone_denied": "浏览器未获得麦克风权限", "audio_unlock": "浏览器声音未解锁或不可用", "playback_overflow": "浏览器停止了过长的回复", "audio_decode": "浏览器音频解码失败", "voice_disconnected": "浏览器语音连接中断", "voice_failed": "浏览器语音连接失败"}
	text, ok := messages[in.Code]
	if !ok {
		writeJSON(w, 400, map[string]string{"message": "未知诊断类别"})
		return
	}
	snap := r.Snapshot()
	var state game.State
	_ = json.Unmarshal(snap.State, &state)
	if state.ID != in.GameID {
		writeJSON(w, 409, map[string]string{"message": "诊断所属棋局已变化"})
		return
	}
	s.store.Diagnostic(room.DiagnosticEvent{RoomID: r.ID, GameID: state.ID, Version: state.Version, Source: "browser", Kind: "error", Code: in.Code, Text: text})
	w.WriteHeader(http.StatusNoContent)
}
