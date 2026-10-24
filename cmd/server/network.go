package main

import (
	"github.com/zkevindev/flytogether/internal/game"
	"net/http"
)

func (s *server) setSession(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{Name: "flytogether", Value: key, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
}
func (s *server) join(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Invite string `json:"invite"`
		Name   string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	key := cookie(r)
	if key == "" {
		key = game.ID()
	}
	v, err := s.store.Join(key, r.PathValue("id"), in.Invite, in.Name)
	if err != nil {
		failure(w, err)
		return
	}
	s.setSession(w, key)
	writeJSON(w, 200, v.SnapshotFor(key))
}
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.PathValue("id"))
	if v == nil {
		return
	}
	var in struct {
		Ready bool `json:"ready"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	out, err := v.SetReady(cookie(r), in.Ready)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *server) leave(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.PathValue("id"))
	if v == nil {
		return
	}
	if err := s.store.Leave(cookie(r), v.ID); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
