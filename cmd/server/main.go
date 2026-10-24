package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/linx"
	"github.com/zkevindev/flytogether/internal/room"
)

type server struct {
	admin        *adminAuth
	store        *room.Store
	bridge       *linx.Bridge
	speaker      *linx.Speaker
	publicHost   string
	secureCookie bool
}

// runtimeConfig is the validated, typed view of the environment for one process.
type runtimeConfig struct {
	Limits          map[string]int
	Addr            string
	PublicHost      string
	SavePath        string
	WebDir          string
	LinxURL         string
	LinxAPIKey      string
	LinxModel       string
	MCPPublicURL    string
	Retention       int
	DiagnosticHours int
	HostDelay       int
	DelegationDelay int
	SecureCookie    bool
	Transcripts     bool
}

func main() {
	loadEnv(".env")
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	savedStore, err := room.OpenStore(cfg.SavePath, time.Duration(cfg.Retention)*time.Hour)
	if err != nil {
		slog.Error("cannot open game saves", "error", err)
		os.Exit(1)
	}
	defer savedStore.Close()
	s, err := newServer(savedStore, cfg)
	if err != nil {
		slog.Error("invalid server configuration", "error", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux, cfg)
	handler := s.securityHandler(mux)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workersDone := make(chan struct{})
	go func() { defer close(workersDone); s.store.Run(ctx.Done()) }()
	shutdownDone := make(chan struct{})
	httpServer := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(c)
	}()
	slog.Info("FlyTogether ready", "listen", cfg.Addr, "voice_configured", s.bridge.Config.Enabled())
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
	<-shutdownDone
	<-workersDone
}

// newServer wires storage, limits, diagnostics and the Linx bridge from a
// validated configuration. It never reaches the network by itself.
func newServer(store *room.Store, cfg runtimeConfig) (*server, error) {
	s := &server{store: store, bridge: &linx.Bridge{Config: linx.Config{URL: cfg.LinxURL, APIKey: cfg.LinxAPIKey, Model: cfg.LinxModel}, MCP: linx.NewReadonlyMCP(cfg.MCPPublicURL)}, publicHost: cfg.PublicHost, secureCookie: cfg.SecureCookie}
	admin, err := newAdminAuth(os.Getenv("ADMIN_ACCESS_KEY"))
	if err != nil {
		return nil, err
	}
	s.admin = admin
	if err = store.ConfigureDiagnostics(room.DiagnosticConfig{Enabled: admin != nil, Transcripts: cfg.Transcripts, Retention: time.Duration(cfg.DiagnosticHours) * time.Hour}); err != nil {
		return nil, err
	}
	store.SetHostReclaimDelay(time.Duration(cfg.HostDelay) * time.Second)
	store.SetDelegationDelay(time.Duration(cfg.DelegationDelay) * time.Second)
	store.SetAudienceLimits(cfg.Limits["SPECTATOR_CAPACITY"], cfg.Limits["SPECTATOR_JOINS_PER_MINUTE"])
	s.bridge.Observe = store.Diagnostic
	s.bridge.MaxConnections = cfg.Limits["LINGXI_MAX_CONNECTIONS"]
	s.bridge.SpectatorMax = cfg.Limits["SPECTATOR_MAX_CONNECTIONS"]
	s.bridge.SessionTimeout = time.Duration(cfg.Limits["LINGXI_SESSION_SECONDS"]) * time.Second
	s.bridge.SpectatorTimeout = time.Duration(cfg.Limits["SPECTATOR_SESSION_SECONDS"]) * time.Second
	s.bridge.ReserveSpectator = func(ctx context.Context) error {
		return store.ReserveVoice(ctx, "spectator", cfg.Limits["SPECTATOR_REALTIME_DAILY_LIMIT"])
	}
	s.bridge.Reserve = func(ctx context.Context) error {
		return store.ReserveVoice(ctx, "realtime", cfg.Limits["LINGXI_REALTIME_DAILY_LIMIT"])
	}
	s.speaker = &linx.Speaker{Config: s.bridge.Config, Reserve: func(ctx context.Context) error {
		return store.ReserveVoice(ctx, "tts", cfg.Limits["LINGXI_TTS_DAILY_LIMIT"])
	}}
	return s, nil
}

// loadConfig validates every environment value that affects behaviour. A bad
// value fails startup instead of silently falling back to a different limit.
func loadConfig() (runtimeConfig, error) {
	cfg := runtimeConfig{
		Addr:       env("LISTEN_ADDR", "127.0.0.1:8080"),
		SavePath:   env("SAVE_PATH", "data/games.db"),
		WebDir:     env("WEB_DIR", "web/dist"),
		LinxURL:    env("LINGXI_REALTIME_URL", "wss://xrobo-io.qiniuapi.com/v1/realtime"),
		LinxAPIKey: os.Getenv("LINGXI_REALTIME_API_KEY"),
		LinxModel:  os.Getenv("LINGXI_REALTIME_MODEL"),
	}
	public, _ := url.Parse(os.Getenv("PUBLIC_URL"))
	cfg.PublicHost, cfg.SecureCookie = public.Host, public.Scheme == "https"
	cfg.MCPPublicURL = strings.TrimSpace(os.Getenv("MCP_PUBLIC_URL"))
	if cfg.MCPPublicURL == "" && cfg.SecureCookie {
		cfg.MCPPublicURL = "https://" + public.Host
	}
	if cfg.MCPPublicURL != "" {
		mcpURL, parseErr := url.Parse(cfg.MCPPublicURL)
		if parseErr != nil || mcpURL.Scheme != "https" || mcpURL.Host == "" || mcpURL.User != nil || mcpURL.RawQuery != "" || mcpURL.Fragment != "" || (mcpURL.Path != "" && mcpURL.Path != "/") {
			return cfg, errors.New("MCP_PUBLIC_URL must be an HTTPS site origin without a path")
		}
		cfg.MCPPublicURL = "https://" + mcpURL.Host
	}
	var err error
	if cfg.Retention, err = positiveEnv("SAVE_RETENTION_HOURS", 168, 1, 8760); err != nil {
		return cfg, err
	}
	cfg.Limits = map[string]int{}
	for key, spec := range map[string][3]int{
		"LINGXI_MAX_CONNECTIONS": {4, 1, 32}, "LINGXI_SESSION_SECONDS": {900, 30, 3600},
		"LINGXI_REALTIME_DAILY_LIMIT": {2000, 1, 100000}, "LINGXI_TTS_DAILY_LIMIT": {1000, 1, 100000},
		"SPECTATOR_CAPACITY": {8, 1, 32}, "SPECTATOR_JOINS_PER_MINUTE": {20, 1, 120},
		"SPECTATOR_MAX_CONNECTIONS": {4, 1, 32}, "SPECTATOR_SESSION_SECONDS": {900, 30, 3600},
		"SPECTATOR_REALTIME_DAILY_LIMIT": {500, 1, 100000},
	} {
		value, e := positiveEnv(key, spec[0], spec[1], spec[2])
		if e != nil {
			return cfg, e
		}
		cfg.Limits[key] = value
	}
	if cfg.DiagnosticHours, err = positiveEnv("DIAGNOSTICS_RETENTION_HOURS", 168, 1, 2160); err != nil {
		return cfg, err
	}
	if cfg.Transcripts, err = strconv.ParseBool(env("DIAGNOSTICS_TRANSCRIPTS", "true")); err != nil {
		return cfg, err
	}
	if cfg.HostDelay, err = positiveEnv("ROOM_HOST_RECLAIM_SECONDS", 60, 5, 600); err != nil {
		return cfg, err
	}
	if cfg.DelegationDelay, err = positiveEnv("ROOM_DELEGATION_SECONDS", 60, 5, 600); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// registerRoutes maps the public HTTP surface. Health, configuration and static
// assets stay reachable without credentials; every game action is session-bound.
func (s *server) registerRoutes(mux *http.ServeMux, cfg runtimeConfig) {
	s.adminRoutes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "voice_configured": s.bridge.Config.Enabled(), "storage": "sqlite"})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"voice_configured": s.bridge.Config.Enabled(), "version": "0.1.0", "ruleset": "family-v1", "save_retention_hours": cfg.Retention, "host_reclaim_seconds": cfg.HostDelay, "delegation_seconds": cfg.DelegationDelay, "persistence_enabled": true, "voice_session_seconds": cfg.Limits["LINGXI_SESSION_SECONDS"], "spectator_session_seconds": cfg.Limits["SPECTATOR_SESSION_SECONDS"], "spectator_capacity": cfg.Limits["SPECTATOR_CAPACITY"], "boards": game.Boards(), "missions": game.MissionChoices(true), "diagnostics": s.store.DiagnosticStatus()})
	})
	mux.Handle("/api/mcp/game", s.bridge.MCP)
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("GET /api/family", s.family)
	mux.HandleFunc("POST /api/family/profiles", s.saveProfile)
	mux.HandleFunc("POST /api/family/profiles/{id}/delete", s.deleteProfile)
	mux.HandleFunc("POST /api/family/history/clear", s.clearHistory)
	mux.HandleFunc("POST /api/rooms", s.create)
	mux.HandleFunc("POST /api/rooms/new", s.create)
	mux.HandleFunc("POST /api/rooms/{id}/matches", s.restart)
	mux.HandleFunc("POST /api/rooms/{id}/join", s.join)
	mux.HandleFunc("POST /api/rooms/{id}/ready", s.ready)
	mux.HandleFunc("POST /api/rooms/{id}/leave", s.leave)
	mux.HandleFunc("POST /api/rooms/{id}/discard", s.discard)
	mux.HandleFunc("POST /api/rooms/{id}/commands", s.command)
	mux.HandleFunc("POST /api/rooms/{id}/diagnostics", s.clientDiagnostic)
	mux.HandleFunc("POST /api/rooms/{id}/voice-settings", s.voiceSettings)
	mux.HandleFunc("POST /api/rooms/{id}/announcement", s.announcement)
	mux.HandleFunc("POST /api/rooms/{id}/audience", s.configureAudience)
	mux.HandleFunc("GET /api/rooms/{id}/watch", s.watchSnapshot)
	mux.HandleFunc("POST /api/rooms/{id}/watch", s.watchJoin)
	mux.HandleFunc("POST /api/rooms/{id}/watch/leave", s.watchLeave)
	mux.HandleFunc("POST /api/rooms/{id}/chat", s.postChat)
	mux.HandleFunc("POST /api/rooms/{id}/chat/block", s.blockChat)
	mux.HandleFunc("GET /ws/game", s.gameSocket)
	mux.HandleFunc("GET /ws/voice", s.voiceSocket)
	mux.HandleFunc("GET /ws/audience-voice", s.audienceVoiceSocket)
	mux.HandleFunc("/", staticHandler(cfg.WebDir))
}

// staticHandler serves the built front end and falls back to index.html for the
// single-page routes. API and socket paths are never rewritten to HTML.
func staticHandler(webDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			http.NotFound(w, r)
			return
		}
		p := filepath.Join(webDir, filepath.Clean("/"+r.URL.Path))
		if stat, e := os.Stat(p); e == nil && !stat.IsDir() {
			http.ServeFile(w, r, p)
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/play" && r.URL.Path != "/product" && r.URL.Path != "/admin" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	}
}

// securityHandler adds the browser hardening headers and the same-origin guard
// for every request, including admin pages and API calls.
func (s *server) securityHandler(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "microphone=(self), camera=()")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/admin" {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !s.originOK(r) {
			writeJSON(w, 403, map[string]string{"code": "ORIGIN_DENIED", "message": "请在游戏页面中操作"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func loadEnv(path string) {
	f, e := os.Open(path)
	if e != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) > 1 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if _, ok = os.LookupEnv(key); !ok {
			_ = os.Setenv(key, value)
		}
	}
}
func (s *server) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	if e != nil {
		return false
	}
	return u.Host == r.Host || (s.publicHost != "" && u.Host == s.publicHost)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	code, message, status := "BAD_REQUEST", "请求未能完成", 400
	var re *room.Error
	if errors.As(err, &re) {
		code, message = re.Code, re.Message
		if code == "SAVE_UNAVAILABLE" {
			status = 503
		}
		if code == "HOST_ONLY" || code == "NOT_YOUR_TURN" || code == "NOT_AUTHORIZED" || code == "SEAT_DELEGATED" || code == "SPECTATE_CLOSED" || code == "AUDIENCE_FULL" || code == "CHAT_DISABLED" {
			status = 403
		}
		if code == "STALE_STATE" || code == "VOICE_BUSY" || code == "COMMAND_CONFLICT" || code == "HOST_GRACE_PERIOD" || code == "HOST_ONLINE" || code == "SEAT_KEPT" {
			status = 409
		}
		if code == "AUDIENCE_RATE" || code == "CHAT_RATE" {
			status = 429
		}
	} else if err != nil {
		message = err.Error()
	}
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("请使用 JSON 请求")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("请求内容无效")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("请求只能包含一个对象")
	}
	return nil
}
func options(w http.ResponseWriter, r *http.Request) (game.Options, error) {
	var o game.Options
	if e := decode(w, r, &o); e != nil {
		return o, e
	}
	if o.Mode != "" && o.Mode != "quick" && o.Mode != "classic" && o.Mode != "coop" {
		return o, errors.New("游戏模式无效")
	}
	if o.ThemeID != "" && o.ThemeID != "star-rescue" {
		return o, errors.New("主题包无效")
	}
	if o.MissionID != "" {
		valid := false
		for _, mission := range game.MissionChoices(o.Props) {
			if mission.ID == o.MissionID {
				valid = true
				break
			}
		}
		if !valid || o.Mode != "coop" {
			return o, errors.New("合作任务卡无效或当前模式不支持")
		}
	}
	if _, ok := game.FindBoard(o.Board); !ok {
		return o, errors.New("棋盘路线无效")
	}
	if len(o.Names) < 2 || len(o.Names) > 4 {
		return o, errors.New("请选择 2 到 4 个席位")
	}
	if o.Network {
		o.Waiting = true
	}
	o.RoomName = strings.TrimSpace(o.RoomName)
	if utf8.RuneCountInString(o.RoomName) > 20 {
		return o, errors.New("房间名最多 20 个字")
	}
	for _, c := range o.RoomName {
		if c < 32 {
			return o, errors.New("房间名包含不支持的字符")
		}
	}
	if o.Difficulty != "" && o.Difficulty != "easy" && o.Difficulty != "normal" && o.Difficulty != "hard" {
		return o, errors.New("游戏难度无效")
	}
	seen := map[string]bool{}
	for i, name := range o.Names {
		name = strings.TrimSpace(name)
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 10 {
			return o, errors.New("昵称需要 1 到 10 个字")
		}
		if seen[name] {
			return o, errors.New("请为每位玩家使用不同昵称")
		}
		for _, c := range name {
			if c < 32 {
				return o, errors.New("昵称包含不支持的字符")
			}
		}
		o.Names[i] = name
		seen[name] = true
	}
	if !game.ValidPreset(o.Preset) {
		return o, errors.New("规则预设无效")
	}
	o = game.ResolveOptions(o)
	return o, nil
}
func cookie(r *http.Request) string {
	c, e := r.Cookie("flytogether")
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *server) owned(w http.ResponseWriter, r *http.Request, id string) *room.Room {
	key := cookie(r)
	if v := s.store.Get(key, id); v != nil {
		return v
	}
	if id != "" && s.store.AudienceRoom(key, id) != nil {
		writeJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED", "message": "观众不能操作棋局或管理房间"})
		return nil
	}
	writeJSON(w, 401, map[string]string{"code": "NOT_AUTHORIZED", "message": "房间已过期，请重新开始"})
	return nil
}
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	if key := cookie(r); key != "" {
		s.setSession(w, key)
	}
	v := s.store.Get(cookie(r), "")
	if v == nil {
		writeJSON(w, 200, map[string]any{"room": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"room": v.SnapshotFor(cookie(r))})
}
func (s *server) create(w http.ResponseWriter, r *http.Request) {
	o, e := options(w, r)
	if e != nil {
		failure(w, e)
		return
	}
	key := cookie(r)
	if key == "" {
		key = game.ID()
	}
	var v *room.Room
	if r.URL.Path == "/api/rooms/new" {
		v, e = s.store.CreateNew(key, o)
	} else {
		v, e = s.store.Create(key, o)
	}
	if e != nil {
		failure(w, e)
		return
	}
	s.setSession(w, key)
	writeJSON(w, 201, v.SnapshotFor(key))
}
func (s *server) restart(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.PathValue("id"))
	if v == nil {
		return
	}
	o, e := options(w, r)
	if e != nil {
		failure(w, e)
		return
	}
	o, e = s.store.FamilyOptions(cookie(r), o)
	if e != nil {
		failure(w, e)
		return
	}
	result, e := v.RestartAs(o, cookie(r))
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, result)
}
func (s *server) command(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.PathValue("id"))
	if v == nil {
		return
	}
	var c room.Command
	if e := decode(w, r, &c); e != nil {
		failure(w, e)
		return
	}
	result, e := v.DoAs(c, cookie(r))
	if e != nil {
		view := v.Snapshot()
		var st game.State
		_ = json.Unmarshal(view.State, &st)
		code := "INVALID_COMMAND"
		var re *room.Error
		if errors.As(e, &re) {
			code = re.Code
		}
		s.store.Diagnostic(room.DiagnosticEvent{RoomID: v.ID, GameID: st.ID, Version: st.Version, Kind: "error", Source: "click", Code: code})
		failure(w, e)
		return
	}
	writeJSON(w, 200, result)
}
func (s *server) gameSocket(w http.ResponseWriter, r *http.Request) {
	key := cookie(r)
	id := r.URL.Query().Get("room_id")
	v := s.store.Get(key, id)
	if v == nil {
		v = s.store.AudienceRoom(key, id)
	}
	if v == nil {
		writeJSON(w, 401, map[string]string{"code": "NOT_AUTHORIZED", "message": "房间已过期，请重新开始"})
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	ch, unsubscribe := v.SubscribeFor(cookie(r))
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		case b := <-ch:
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(b, &head)
			if head.Type != "audience_closed" && !v.HasAccess(cookie(r)) {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = c.Write(writeCtx, websocket.MessageText, b)
			cancel()
			if e != nil || head.Type == "audience_closed" {
				return
			}
		}
	}
}
func (s *server) voiceSettings(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.PathValue("id"))
	if v == nil {
		return
	}
	var settings room.VoiceSettings
	if e := decode(w, r, &settings); e != nil {
		failure(w, e)
		return
	}
	if v.Snapshot().Network != nil {
		writeJSON(w, 403, map[string]string{"message": "联网房间使用直接对话，无需设置唤醒词"})
		return
	}
	result, e := v.SetVoiceSettings(settings)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, result)
}
func (s *server) voiceSocket(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.URL.Query().Get("room_id"))
	if v == nil {
		return
	}
	if snapshot := v.Snapshot(); snapshot.Status != "playing" {
		writeJSON(w, 409, map[string]string{"message": "请先开始或继续游戏，再开启语音"})
		return
	}
	if !s.bridge.Config.Enabled() {
		writeJSON(w, 503, map[string]string{"message": "灵矽尚未配置，点击玩法可以继续"})
		return
	}
	preferences, err := (linx.CompanionPreferences{Style: r.URL.Query().Get("style"), Frequency: r.URL.Query().Get("frequency")}).Normalized()
	if err != nil {
		failure(w, err)
		return
	}
	sessionRoom, err := v.VoiceForMode(cookie(r), r.URL.Query().Get("mode") == "ask")
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

func positiveEnv(name string, fallback, min, max int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, errors.New(name + " is outside allowed range")
	}
	return value, nil
}
func (s *server) discard(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		failure(w, errors.New("请使用 JSON 请求"))
		return
	}
	if err := s.store.Discard(cookie(r), r.PathValue("id")); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
