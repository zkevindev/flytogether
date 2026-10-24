package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

const adminCookie = "flytogether_admin"

type adminSession struct{ expires time.Time }
type loginAttempts struct {
	count int
	until time.Time
}
type adminAuth struct {
	hash     [32]byte
	mu       sync.Mutex
	sessions map[[32]byte]adminSession
	failures map[string]loginAttempts
}

func newAdminAuth(secret string) (*adminAuth, error) {
	if secret == "" {
		return nil, nil
	}
	if len(secret) < 32 || len(secret) > 256 || strings.TrimSpace(secret) != secret {
		return nil, errors.New("ADMIN_ACCESS_KEY must be 32 to 256 characters without outer whitespace")
	}
	return &adminAuth{hash: sha256.Sum256([]byte(secret)), sessions: map[[32]byte]adminSession{}, failures: map[string]loginAttempts{}}, nil
}
func (s *server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", s.adminLogin)
	mux.HandleFunc("GET /api/admin/session", s.adminSession)
	mux.HandleFunc("POST /api/admin/logout", s.adminLogout)
	mux.HandleFunc("GET /api/admin/matches", s.adminMatches)
	mux.HandleFunc("GET /api/admin/matches/{id}", s.adminMatch)
	mux.HandleFunc("GET /api/admin/matches/{id}/events", s.adminEvents)
}
func adminHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}
func (s *server) adminEnabled(w http.ResponseWriter) bool {
	adminHeaders(w)
	if s.admin == nil {
		writeJSON(w, 503, map[string]string{"code": "ADMIN_DISABLED", "message": "管理后台尚未启用，请在服务器配置管理员密钥"})
		return false
	}
	return true
}
func (a *adminAuth) cleanup(now time.Time) {
	for k, v := range a.sessions {
		if !v.expires.After(now) {
			delete(a.sessions, k)
		}
	}
	for k, v := range a.failures {
		if !v.until.After(now) {
			delete(a.failures, k)
		}
	}
}
func (s *server) adminAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminEnabled(w) {
		return false
	}
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, 403, map[string]string{"message": "请从同源管理页面访问"})
		return false
	}
	c, err := r.Cookie(adminCookie)
	if err == nil {
		s.admin.mu.Lock()
		s.admin.cleanup(time.Now())
		session, ok := s.admin.sessions[sha256.Sum256([]byte(c.Value))]
		s.admin.mu.Unlock()
		if ok && session.expires.After(time.Now()) {
			return true
		}
	}
	writeJSON(w, 401, map[string]string{"code": "ADMIN_REQUIRED", "message": "请先登录管理后台，会话有效期8小时"})
	return false
}
func (s *server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.adminEnabled(w) {
		return
	}
	if !s.originOK(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, 403, map[string]string{"message": "请从同源管理页面登录"})
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := decode(w, r, &in); err != nil {
		failure(w, err)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a := s.admin
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	a.cleanup(now)
	// Do not trust client-supplied forwarding headers; proxy deployments share the peer budget.
	attempt := a.failures[ip]
	if attempt.count >= 8 || len(a.failures) >= 1024 {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]string{"message": "登录尝试过多，请一分钟后重试"})
		return
	}
	actual := sha256.Sum256([]byte(in.Key))
	if subtle.ConstantTimeCompare(actual[:], a.hash[:]) != 1 {
		if attempt.until.IsZero() {
			attempt.until = now.Add(time.Minute)
		}
		attempt.count++
		a.failures[ip] = attempt
		writeJSON(w, 401, map[string]string{"message": "管理员密钥不正确"})
		return
	}
	if len(a.sessions) >= 64 {
		writeJSON(w, 429, map[string]string{"message": "管理会话已满，请退出其他会话或稍后再试"})
		return
	}
	delete(a.failures, ip)
	token := game.ID() + game.ID()
	expires := now.Add(8 * time.Hour)
	a.sessions[sha256.Sum256([]byte(token))] = adminSession{expires: expires}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/api/admin", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600, Expires: expires})
	writeJSON(w, 200, map[string]any{"authenticated": true, "expires_at": expires})
}
func (s *server) adminSession(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "diagnostics": s.store.DiagnosticStatus()})
}
func (s *server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	c, _ := r.Cookie(adminCookie)
	s.admin.mu.Lock()
	delete(s.admin.sessions, sha256.Sum256([]byte(c.Value)))
	s.admin.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/api/admin", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func queryNumber(r *http.Request, key string, fallback, min, max int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback, nil
	}
	i, e := strconv.Atoi(v)
	if e != nil || i < min || i > max {
		return 0, errors.New("分页参数无效")
	}
	return i, nil
}
func adminQuery(w http.ResponseWriter, r *http.Request) (room.DiagnosticQuery, bool) {
	q := room.DiagnosticQuery{Search: strings.TrimSpace(r.URL.Query().Get("q")), Status: r.URL.Query().Get("status"), Kind: r.URL.Query().Get("kind")}
	var err error
	q.Limit, err = queryNumber(r, "limit", 25, 1, 100)
	if err == nil {
		q.Offset, err = queryNumber(r, "offset", 0, 0, 100000)
	}
	if err == nil {
		v := r.URL.Query().Get("before")
		if v != "" {
			q.Before, err = strconv.ParseInt(v, 10, 64)
			if q.Before < 0 {
				err = errors.New("分页参数无效")
			}
		}
	}
	if len(q.Search) > 160 {
		err = errors.New("搜索内容过长")
	}
	switch q.Status {
	case "", "waiting", "playing", "paused", "ended", "finished":
	default:
		err = errors.New("状态筛选无效")
	}
	switch q.Kind {
	case "", "game", "command", "voice", "user_text", "assistant_text", "error", "connection", "tts", "tool", "first_audio":
	default:
		err = errors.New("事件筛选无效")
	}
	if err != nil {
		failure(w, err)
		return q, false
	}
	return q, true
}
func adminReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"message": "对局记录不存在或已超过保留期"})
	} else {
		writeJSON(w, 503, map[string]string{"message": "排障记录暂时不可读，请稍后重试"})
	}
}
func (s *server) adminMatches(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	q, ok := adminQuery(w, r)
	if !ok {
		return
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	out, err := s.store.DiagnosticMatches(r.Context(), q)
	if err != nil {
		adminReadError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *server) adminMatch(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	out, err := s.store.DiagnosticMatch(r.Context(), r.PathValue("id"))
	if err != nil {
		adminReadError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *server) adminEvents(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	q, ok := adminQuery(w, r)
	if !ok {
		return
	}
	out, err := s.store.DiagnosticTimeline(r.Context(), r.PathValue("id"), q)
	if err != nil {
		adminReadError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
