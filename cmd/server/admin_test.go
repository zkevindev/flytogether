package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAdminKey = "test-admin-key-not-a-real-secret-123456789"

func adminTestServer(t *testing.T) (*server, *http.ServeMux) {
	t.Helper()
	store, e := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), 24*time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	if e = store.ConfigureDiagnostics(room.DiagnosticConfig{Enabled: true, Transcripts: true, Retention: 24 * time.Hour}); e != nil {
		t.Fatal(e)
	}
	a, e := newAdminAuth(testAdminKey)
	if e != nil {
		t.Fatal(e)
	}
	s := &server{store: store, admin: a, secureCookie: true}
	mux := http.NewServeMux()
	s.adminRoutes(mux)
	return s, mux
}
func adminRequest(mux *http.ServeMux, method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://example.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func adminLoginCookie(t *testing.T, mux *http.ServeMux) *http.Cookie {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"key": testAdminKey})
	w := adminRequest(mux, "POST", "/api/admin/login", string(b), nil, "https://example.test")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("no admin cookie")
	}
	return cookies[0]
}
func TestAdminAccessSessionIsolationAndLogout(t *testing.T) {
	s, mux := adminTestServer(t)
	for _, path := range []string{"/api/admin/session", "/api/admin/matches", "/api/admin/matches/nope", "/api/admin/matches/nope/events"} {
		w := adminRequest(mux, "GET", path, "", &http.Cookie{Name: "flytogether", Value: "player-cookie"}, "")
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public admin access", w.Code)
		}
	}
	c := adminLoginCookie(t, mux)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/admin" {
		t.Fatal("unsafe admin cookie", c)
	}
	if w := adminRequest(mux, "GET", "/api/admin/session", "", c, ""); w.Code != 200 || strings.Contains(w.Body.String(), testAdminKey) {
		t.Fatal("session unavailable or leaked secret")
	}
	if w := adminRequest(mux, "GET", "/api/admin/matches", "", c, "https://evil.test"); w.Code != 403 {
		t.Fatal("cross origin read")
	}
	if w := adminRequest(mux, "POST", "/api/admin/logout", "{}", c, "https://evil.test"); w.Code != 403 {
		t.Fatal("cross origin logout")
	}
	if w := adminRequest(mux, "POST", "/api/admin/logout", "{}", c, "https://example.test"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := adminRequest(mux, "GET", "/api/admin/session", "", c, ""); w.Code != 401 {
		t.Fatal("logout session reusable")
	}
	c = adminLoginCookie(t, mux)
	s.admin.mu.Lock()
	s.admin.sessions[sha256.Sum256([]byte(c.Value))] = adminSession{expires: time.Now().Add(-time.Second)}
	s.admin.mu.Unlock()
	if w := adminRequest(mux, "GET", "/api/admin/matches", "", c, ""); w.Code != 401 {
		t.Fatal("expired session allowed")
	}
	s.admin = nil
	if w := adminRequest(mux, "POST", "/api/admin/login", `{"key":"x"}`, nil, ""); w.Code != 503 {
		t.Fatal("disabled login")
	}
}
func TestAdminListDetailPaginationAndRedaction(t *testing.T) {
	s, mux := adminTestServer(t)
	r, e := s.store.Create("player-secret", game.Options{Names: []string{"小朋友", "家长"}, RoomName: "排障样例", Network: true, Waiting: true})
	if e != nil {
		t.Fatal(e)
	}
	v := r.Snapshot()
	var g game.State
	json.Unmarshal(v.State, &g)
	for i := 0; i < 6; i++ {
		s.store.Diagnostic(room.DiagnosticEvent{RoomID: r.ID, GameID: g.ID, Kind: "user_text", Source: "realtime", Text: "Bearer api-secret 你好"})
	}
	c := adminLoginCookie(t, mux)
	for _, path := range []string{"/api/admin/matches?limit=-1", "/api/admin/matches?offset=-4", "/api/admin/matches?status=unknown", "/api/admin/matches/" + g.ID + "/events?before=bad", "/api/admin/matches/" + g.ID + "/events?kind=bad"} {
		if w := adminRequest(mux, "GET", path, "", c, ""); w.Code != 400 {
			t.Fatal("invalid query accepted", path, w.Code)
		}
	}
	for _, path := range []string{"/api/admin/matches?q=" + r.ID, "/api/admin/matches/" + g.ID, "/api/admin/matches/" + g.ID + "/events?limit=2"} {
		w := adminRequest(mux, "GET", path, "", c, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		for _, forbidden := range []string{"player-secret", "api-secret", v.Network.Invite, "action_token", "profile_ids"} {
			if forbidden != "" && bytes.Contains(w.Body.Bytes(), []byte(forbidden)) {
				t.Fatal("credential or identity leaked", forbidden)
			}
		}
	}
	w := adminRequest(mux, "GET", "/api/admin/matches/"+g.ID+"/events?kind=user_text&limit=2", "", c, "")
	var page room.DiagnosticEvents
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Events) != 2 || page.NextBefore == 0 {
		t.Fatal("no cursor")
	}
	if w = adminRequest(mux, "GET", "/api/admin/matches/missing", "", c, ""); w.Code != 404 {
		t.Fatal("missing game status")
	}
}
func TestAdminRateLimitsAndKeyValidation(t *testing.T) {
	for _, key := range []string{"short", strings.Repeat("x", 257), " " + testAdminKey} {
		if _, e := newAdminAuth(key); e == nil {
			t.Fatal("invalid key accepted")
		}
	}
	if a, e := newAdminAuth(""); a != nil || e != nil {
		t.Fatal("empty key not disabled")
	}
	_, mux := adminTestServer(t)
	if w := adminRequest(mux, "POST", "/api/admin/login", `{"key":"bad"}`, nil, "https://evil.test"); w.Code != 403 {
		t.Fatal("cross origin login")
	}
	for i := 0; i < 8; i++ {
		if w := adminRequest(mux, "POST", "/api/admin/login", `{"key":"bad"}`, nil, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	w := adminRequest(mux, "POST", "/api/admin/login", `{"key":"bad"}`, nil, "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("no login limit")
	}
}

func TestBrowserDiagnosticIsOwnedAllowlistedAndBounded(t *testing.T) {
	s, mux := adminTestServer(t)
	mux.HandleFunc("POST /api/rooms/{id}/diagnostics", s.clientDiagnostic)
	r, _ := s.store.Create("player-cookie", game.Options{Names: []string{"一", "二"}})
	var state game.State
	json.Unmarshal(r.Snapshot().State, &state)
	path := "/api/rooms/" + r.ID + "/diagnostics"
	body := `{"game_id":"` + state.ID + `","code":"audio_unlock"}`
	if w := adminRequest(mux, "POST", path, body, nil, ""); w.Code != 401 {
		t.Fatal("public telemetry")
	}
	c := &http.Cookie{Name: "flytogether", Value: "player-cookie"}
	if w := adminRequest(mux, "POST", path, `{"game_id":"old-game","code":"audio_unlock"}`, c, ""); w.Code != 409 {
		t.Fatal("stale telemetry")
	}
	if w := adminRequest(mux, "POST", path, `{"game_id":"`+state.ID+`","code":"raw-api-key"}`, c, ""); w.Code != 400 {
		t.Fatal("arbitrary data accepted")
	}
	for i := 0; i < 2; i++ {
		if w := adminRequest(mux, "POST", path, body, c, ""); w.Code != 204 {
			t.Fatal(w.Code)
		}
	}
	timeline, e := s.store.DiagnosticTimeline(t.Context(), state.ID, room.DiagnosticQuery{Kind: "error"})
	if e != nil || len(timeline.Events) != 1 || timeline.Events[0].Text != "浏览器声音未解锁或不可用" {
		t.Fatal("telemetry not bounded", e, timeline)
	}
}
