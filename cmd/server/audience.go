package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/linx"
	"github.com/zkevindev/flytogether/internal/room"
)

func (s *server) configureAudience(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled     *bool `json:"enabled"`
		Rotate      bool  `json:"rotate"`
		ChatEnabled *bool `json:"chat_enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	view, err := s.store.ConfigureAudience(cookie(r), r.PathValue("id"), in.Enabled, in.Rotate, in.ChatEnabled)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *server) watchSnapshot(w http.ResponseWriter, r *http.Request) {
	key := cookie(r)
	v := s.store.AudienceRoom(key, r.PathValue("id"))
	if v == nil {
		writeJSON(w, 401, map[string]string{"code": "NOT_WATCHING", "message": "请先填写昵称加入观赛"})
		return
	}
	writeJSON(w, 200, v.SnapshotFor(key))
}

func (s *server) watchJoin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	key := cookie(r)
	if key == "" {
		key = game.ID()
	}
	v, err := s.store.Watch(key, r.PathValue("id"), in.Token, in.Name)
	if err != nil {
		failure(w, err)
		return
	}
	s.setSession(w, key)
	writeJSON(w, 200, v.SnapshotFor(key))
}

func (s *server) watchLeave(w http.ResponseWriter, r *http.Request) {
	if err := s.store.LeaveAudience(cookie(r), r.PathValue("id")); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) postChat(w http.ResponseWriter, r *http.Request) {
	var in room.ChatInput
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	view, err := s.store.PostChat(cookie(r), r.PathValue("id"), in)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *server) blockChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FromID  string `json:"from_id"`
		Blocked bool   `json:"blocked"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	key := cookie(r)
	v := s.store.Get(key, r.PathValue("id"))
	if v == nil {
		v = s.store.AudienceRoom(key, r.PathValue("id"))
	}
	if v == nil {
		writeJSON(w, 401, map[string]string{"code": "NOT_AUTHORIZED", "message": "请先入座或加入观赛"})
		return
	}
	view, err := v.BlockChat(key, in.FromID, in.Blocked)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *server) audienceVoiceSocket(w http.ResponseWriter, r *http.Request) {
	key := cookie(r)
	v := s.store.AudienceRoom(key, r.URL.Query().Get("room_id"))
	if v == nil {
		writeJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED", "message": "请先加入观赛，再询问自己的灵矽"})
		return
	}
	if !s.bridge.Config.Enabled() {
		writeJSON(w, 503, map[string]string{"message": "灵矽尚未配置，仍可安静观看棋局"})
		return
	}
	preferences, err := (linx.CompanionPreferences{Style: r.URL.Query().Get("style"), Frequency: "quiet"}).Normalized()
	if err != nil {
		failure(w, err)
		return
	}
	sessionRoom, err := v.VoiceForSpectator(key)
	if err != nil {
		failure(w, err)
		return
	}
	defer sessionRoom.Close()
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer c.CloseNow()
	if e = s.bridge.Serve(r.Context(), c, sessionRoom, preferences); e != nil {
		b, _ := json.Marshal(map[string]string{"type": "error", "message": e.Error()})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = c.Write(ctx, websocket.MessageText, b)
	}
}
