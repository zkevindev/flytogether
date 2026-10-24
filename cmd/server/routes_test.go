package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/room"
)

// clearConfigEnv removes every variable loadConfig reads so each test observes
// exactly the configuration it sets.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"LISTEN_ADDR", "PUBLIC_URL", "SAVE_PATH", "WEB_DIR",
		"LINGXI_REALTIME_URL", "LINGXI_REALTIME_API_KEY", "LINGXI_REALTIME_MODEL",
		"SAVE_RETENTION_HOURS", "DIAGNOSTICS_RETENTION_HOURS", "DIAGNOSTICS_TRANSCRIPTS",
		"ROOM_HOST_RECLAIM_SECONDS", "ROOM_DELEGATION_SECONDS", "ADMIN_ACCESS_KEY",
		"LINGXI_MAX_CONNECTIONS", "LINGXI_SESSION_SECONDS", "LINGXI_REALTIME_DAILY_LIMIT",
		"LINGXI_TTS_DAILY_LIMIT", "SPECTATOR_CAPACITY", "SPECTATOR_JOINS_PER_MINUTE",
		"SPECTATOR_MAX_CONNECTIONS", "SPECTATOR_SESSION_SECONDS", "SPECTATOR_REALTIME_DAILY_LIMIT",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	clearConfigEnv(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" || cfg.SavePath != "data/games.db" || cfg.WebDir != "web/dist" {
		t.Fatalf("default paths: %+v", cfg)
	}
	if cfg.Retention != 168 || cfg.DiagnosticHours != 168 || cfg.HostDelay != 60 || cfg.DelegationDelay != 60 || !cfg.Transcripts {
		t.Fatalf("default limits: %+v", cfg)
	}
	if cfg.PublicHost != "" || cfg.SecureCookie {
		t.Fatalf("default public host: %+v", cfg)
	}
	want := map[string]int{
		"LINGXI_MAX_CONNECTIONS": 4, "LINGXI_SESSION_SECONDS": 900,
		"LINGXI_REALTIME_DAILY_LIMIT": 2000, "LINGXI_TTS_DAILY_LIMIT": 1000,
		"SPECTATOR_CAPACITY": 8, "SPECTATOR_JOINS_PER_MINUTE": 20,
		"SPECTATOR_MAX_CONNECTIONS": 4, "SPECTATOR_SESSION_SECONDS": 900, "SPECTATOR_REALTIME_DAILY_LIMIT": 500,
	}
	for key, value := range want {
		if cfg.Limits[key] != value {
			t.Fatalf("%s = %d, want %d", key, cfg.Limits[key], value)
		}
	}
}

func TestLoadConfigOverridesAndHTTPSPublicURL(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LISTEN_ADDR", "0.0.0.0:9000")
	t.Setenv("PUBLIC_URL", "https://game.example.test/fly")
	t.Setenv("SAVE_PATH", "/tmp/custom.db")
	t.Setenv("WEB_DIR", "/srv/web")
	t.Setenv("SAVE_RETENTION_HOURS", "24")
	t.Setenv("DIAGNOSTICS_RETENTION_HOURS", "12")
	t.Setenv("DIAGNOSTICS_TRANSCRIPTS", "false")
	t.Setenv("ROOM_HOST_RECLAIM_SECONDS", "30")
	t.Setenv("ROOM_DELEGATION_SECONDS", "45")
	t.Setenv("LINGXI_REALTIME_URL", "wss://provider.example.test/v1/realtime")
	t.Setenv("LINGXI_REALTIME_API_KEY", "test-only-key")
	t.Setenv("LINGXI_REALTIME_MODEL", "test-model")
	t.Setenv("SPECTATOR_CAPACITY", "16")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicHost != "game.example.test" || !cfg.SecureCookie {
		t.Fatalf("https public url: %+v", cfg)
	}
	if cfg.Retention != 24 || cfg.DiagnosticHours != 12 || cfg.Transcripts || cfg.HostDelay != 30 || cfg.DelegationDelay != 45 {
		t.Fatalf("overrides: %+v", cfg)
	}
	if cfg.Limits["SPECTATOR_CAPACITY"] != 16 {
		t.Fatalf("spectator capacity: %+v", cfg.Limits)
	}
	// Plain HTTP public URLs must not mark cookies Secure.
	t.Setenv("PUBLIC_URL", "http://game.example.test")
	if cfg, err = loadConfig(); err != nil || cfg.SecureCookie {
		t.Fatalf("http public url: %+v %v", cfg, err)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	for name, set := range map[string]func(){
		"retention seconds":  func() { t.Setenv("SAVE_RETENTION_HOURS", "0") },
		"retention text":     func() { t.Setenv("SAVE_RETENTION_HOURS", "week") },
		"lingxi connections": func() { t.Setenv("LINGXI_MAX_CONNECTIONS", "64") },
		"session seconds":    func() { t.Setenv("LINGXI_SESSION_SECONDS", "10") },
		"daily realtime":     func() { t.Setenv("LINGXI_REALTIME_DAILY_LIMIT", "0") },
		"daily tts":          func() { t.Setenv("LINGXI_TTS_DAILY_LIMIT", "100001") },
		"spectator capacity": func() { t.Setenv("SPECTATOR_CAPACITY", "33") },
		"spectator joins":    func() { t.Setenv("SPECTATOR_JOINS_PER_MINUTE", "0") },
		"spectator conns":    func() { t.Setenv("SPECTATOR_MAX_CONNECTIONS", "-1") },
		"spectator session":  func() { t.Setenv("SPECTATOR_SESSION_SECONDS", "10") },
		"spectator realtime": func() { t.Setenv("SPECTATOR_REALTIME_DAILY_LIMIT", "nope") },
		"diagnostics hours":  func() { t.Setenv("DIAGNOSTICS_RETENTION_HOURS", "5000") },
		"diagnostics bool":   func() { t.Setenv("DIAGNOSTICS_TRANSCRIPTS", "yes-please") },
		"host reclaim":       func() { t.Setenv("ROOM_HOST_RECLAIM_SECONDS", "1") },
		"delegation seconds": func() { t.Setenv("ROOM_DELEGATION_SECONDS", "601") },
	} {
		t.Run(name, func(t *testing.T) {
			clearConfigEnv(t)
			set()
			if _, err := loadConfig(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestNewServerWiresLimitsDiagnosticsAndVoice(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ADMIN_ACCESS_KEY", strings.Repeat("k", 40))
	store, err := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Limits["LINGXI_MAX_CONNECTIONS"] = 6
	cfg.Limits["SPECTATOR_MAX_CONNECTIONS"] = 2
	cfg.Limits["LINGXI_SESSION_SECONDS"] = 120
	cfg.Limits["SPECTATOR_SESSION_SECONDS"] = 45
	cfg.Limits["LINGXI_REALTIME_DAILY_LIMIT"] = 1
	cfg.Limits["LINGXI_TTS_DAILY_LIMIT"] = 1
	cfg.Limits["SPECTATOR_REALTIME_DAILY_LIMIT"] = 1
	cfg.Limits["SPECTATOR_CAPACITY"] = 5
	cfg.LinxURL, cfg.LinxAPIKey, cfg.LinxModel = "wss://provider.example.test/v1/realtime", "key", "model"
	s, err := newServer(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.admin == nil {
		t.Fatal("administrator key was not enabled")
	}
	if s.bridge.MaxConnections != 6 || s.bridge.SpectatorMax != 2 || s.bridge.SessionTimeout != 120*time.Second || s.bridge.SpectatorTimeout != 45*time.Second {
		t.Fatalf("bridge limits: %+v", s.bridge)
	}
	if !s.bridge.Config.Enabled() || s.speaker == nil {
		t.Fatal("voice wiring missing")
	}
	ctx := context.Background()
	if err = s.bridge.Reserve(ctx); err != nil {
		t.Fatalf("realtime reservation: %v", err)
	}
	if err = s.bridge.Reserve(ctx); err == nil {
		t.Fatal("daily realtime budget not enforced")
	}
	if err = s.bridge.ReserveSpectator(ctx); err != nil {
		t.Fatalf("spectator reservation: %v", err)
	}
	// Verify the speaker quota hook directly: unit tests must not depend on DNS
	// or open a connection to a real provider.
	if err = s.speaker.Reserve(ctx); err != nil {
		t.Fatalf("tts reservation: %v", err)
	}
	if err = s.speaker.Reserve(ctx); err == nil {
		t.Fatal("daily tts budget not enforced")
	}
	if status := store.DiagnosticStatus(); status["enabled"] != true {
		t.Fatalf("diagnostics not enabled with an administrator: %+v", status)
	}

	// A malformed administrator key must fail startup instead of disabling admin.
	t.Setenv("ADMIN_ACCESS_KEY", "short")
	if _, err = newServer(store, cfg); err == nil {
		t.Fatal("short administrator key accepted")
	}
}

func routeTestServer(t *testing.T) (*server, http.Handler, runtimeConfig, string) {
	t.Helper()
	store, err := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	webDir := t.TempDir()
	if err = os.MkdirAll(filepath.Join(webDir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(webDir, "index.html"), "<html>family board</html>")
	writeFile(t, filepath.Join(webDir, "assets", "app.js"), "console.log('board')")
	cfg := runtimeConfig{
		Limits:    map[string]int{"LINGXI_SESSION_SECONDS": 900, "SPECTATOR_CAPACITY": 8},
		Retention: 168, DiagnosticHours: 168, HostDelay: 60, DelegationDelay: 60,
		WebDir: webDir, SavePath: "ignored", Transcripts: true,
	}
	s, err := newServer(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux, cfg)
	return s, s.securityHandler(mux), cfg, webDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRoutesServesHealthConfigAndStaticApp(t *testing.T) {
	_, handler, _, _ := routeTestServer(t)
	for _, tc := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/healthz", 200, `"status":"ok"`},
		{"GET", "/readyz", 200, `"storage":"sqlite"`},
		{"GET", "/api/config", 200, `"ruleset":"family-v1"`},
		{"GET", "/", 200, "family board"},
		{"HEAD", "/", 200, ""},
		{"GET", "/play", 200, "family board"},
		{"GET", "/admin", 200, "family board"},
		{"GET", "/assets/app.js", 200, "console.log"},
		{"GET", "/missing", 404, ""},
		{"POST", "/", 404, ""},
		{"GET", "/api/unknown", 404, ""},
		{"GET", "/ws/unknown", 404, ""},
	} {
		r := httptest.NewRequest(tc.method, "https://example.test"+tc.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if tc.contains != "" && !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatalf("%s %s body: %s", tc.method, tc.path, w.Body)
		}
	}
}

func TestConfigPayloadExposesOnlyPublicLimits(t *testing.T) {
	_, handler, _, _ := routeTestServer(t)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "https://example.test/api/config", nil))
	var cfg struct {
		VoiceConfigured     bool             `json:"voice_configured"`
		Ruleset             string           `json:"ruleset"`
		PersistenceEnabled  bool             `json:"persistence_enabled"`
		VoiceSessionSeconds int              `json:"voice_session_seconds"`
		SpectatorCapacity   int              `json:"spectator_capacity"`
		SaveRetentionHours  int              `json:"save_retention_hours"`
		HostReclaimSeconds  int              `json:"host_reclaim_seconds"`
		DelegationSeconds   int              `json:"delegation_seconds"`
		Boards              []map[string]any `json:"boards"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("config payload: %v %s", err, w.Body)
	}
	if cfg.VoiceConfigured || !cfg.PersistenceEnabled || cfg.VoiceSessionSeconds != 900 || cfg.SpectatorCapacity != 8 || cfg.SaveRetentionHours != 168 || cfg.HostReclaimSeconds != 60 || cfg.DelegationSeconds != 60 || len(cfg.Boards) != 3 {
		t.Fatalf("unexpected public config: %+v", cfg)
	}
	for _, leaked := range []string{"api_key", "ADMIN_ACCESS_KEY", "speaker", "provider"} {
		if strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(leaked)) {
			t.Fatalf("config leaked %q", leaked)
		}
	}
}

func TestSecurityHandlerHeadersOriginAndCaching(t *testing.T) {
	s := &server{publicHost: "public.test"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	handler := s.securityHandler(mux)

	check := func(path, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://example.test"+path, nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := check("/api/session", "")
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
		"X-Frame-Options":        "DENY",
		"Permissions-Policy":     "microphone=(self), camera=()",
		"Cache-Control":          "no-store",
	} {
		if got := w.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if w = check("/admin", ""); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admin caching: %q", w.Header().Get("Cache-Control"))
	}
	if w = check("/app.js", ""); w.Header().Get("Cache-Control") == "no-store" {
		t.Fatal("static asset must stay cacheable")
	}
	if w = check("/api/session", "https://evil.test"); w.Code != 403 || !strings.Contains(w.Body.String(), "ORIGIN_DENIED") {
		t.Fatalf("foreign origin: %d %s", w.Code, w.Body)
	}
	if w = check("/api/session", "https://public.test"); w.Code != 200 {
		t.Fatalf("public origin: %d %s", w.Code, w.Body)
	}
	if w = check("/api/session", "https://example.test"); w.Code != 200 {
		t.Fatalf("same origin: %d %s", w.Code, w.Body)
	}
}
