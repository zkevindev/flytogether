package main

import (
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
	"net/http"
)

func (s *server) family(w http.ResponseWriter, r *http.Request) {
	key := cookie(r)
	if key == "" {
		key = game.ID()
	}
	result, err := s.store.Family(key)
	if err != nil {
		failure(w, err)
		return
	}
	s.setSession(w, key)
	writeJSON(w, 200, result)
}
func (s *server) saveProfile(w http.ResponseWriter, r *http.Request) {
	key := cookie(r)
	if key == "" {
		key = game.ID()
	}
	var p room.Profile
	if err := decode(w, r, &p); err != nil {
		failure(w, err)
		return
	}
	result, err := s.store.SaveProfile(key, p)
	if err != nil {
		failure(w, err)
		return
	}
	s.setSession(w, key)
	writeJSON(w, 200, result)
}
func (s *server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	if err := s.store.DeleteProfile(cookie(r), r.PathValue("id")); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *server) clearHistory(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	if err := s.store.ClearHistory(cookie(r)); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
