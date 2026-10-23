package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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
	store        *room.Store
	bridge       *linx.Bridge
	publicHost   string
	secureCookie bool
}

func main() {
	loadEnv(".env")
	addr := env("LISTEN_ADDR", "127.0.0.1:8080")
	public, _ := url.Parse(os.Getenv("PUBLIC_URL"))
	s := &server{store: room.NewStore(), bridge: &linx.Bridge{Config: linx.Config{
		URL:    env("LINGXI_REALTIME_URL", "wss://xrobo-io.qiniuapi.com/v1/realtime"),
		APIKey: os.Getenv("LINGXI_REALTIME_API_KEY"), Model: os.Getenv("LINGXI_REALTIME_MODEL")}}, publicHost: public.Host, secureCookie: public.Scheme == "https"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "voice_configured": s.bridge.Config.Enabled()})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"voice_configured": s.bridge.Config.Enabled(), "version": "0.1.0", "ruleset": "family-v1", "boards": game.Boards()})
	})
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("POST /api/rooms", s.create)
	mux.HandleFunc("POST /api/rooms/new", s.create)
	mux.HandleFunc("POST /api/rooms/{id}/matches", s.restart)
	mux.HandleFunc("POST /api/rooms/{id}/commands", s.command)
	mux.HandleFunc("POST /api/rooms/{id}/voice-settings", s.voiceSettings)
	mux.HandleFunc("GET /ws/game", s.gameSocket)
	mux.HandleFunc("GET /ws/voice", s.voiceSocket)
	webDir := env("WEB_DIR", "web/dist")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "microphone=(self), camera=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !s.originOK(r) {
			writeJSON(w, 403, map[string]string{"code": "ORIGIN_DENIED", "message": "请在游戏页面中操作"})
			return
		}
		mux.ServeHTTP(w, r)
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.store.Run(ctx.Done())
	httpServer := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(c)
	}()
	slog.Info("FlyTogether ready", "listen", addr, "voice_configured", s.bridge.Config.Enabled())
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
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
		if code == "STALE_STATE" || code == "VOICE_BUSY" || code == "COMMAND_CONFLICT" {
			status = 409
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
	if o.Mode != "" && o.Mode != "quick" && o.Mode != "classic" {
		return o, errors.New("游戏模式无效")
	}
	if _, ok := game.FindBoard(o.Board); !ok {
		return o, errors.New("棋盘路线无效")
	}
	if len(o.Names) < 2 || len(o.Names) > 4 {
		return o, errors.New("请选择 2 到 4 个席位")
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
	v := s.store.Get(cookie(r), id)
	if v == nil {
		writeJSON(w, 401, map[string]string{"code": "NOT_AUTHORIZED", "message": "房间已过期，请重新开始"})
	}
	return v
}
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	v := s.store.Get(cookie(r), "")
	if v == nil {
		writeJSON(w, 200, map[string]any{"room": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"room": v.Snapshot()})
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
	http.SetCookie(w, &http.Cookie{Name: "flytogether", Value: key, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, 201, v.Snapshot())
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
	result, e := v.Restart(o)
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
	result, e := v.Do(c, false)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, result)
}
func (s *server) gameSocket(w http.ResponseWriter, r *http.Request) {
	v := s.owned(w, r, r.URL.Query().Get("room_id"))
	if v == nil {
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	ch, unsubscribe := v.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-ch:
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = c.Write(writeCtx, websocket.MessageText, b)
			cancel()
			if e != nil {
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
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer c.CloseNow()
	if e = s.bridge.Serve(r.Context(), c, v); e != nil {
		b, _ := json.Marshal(map[string]string{"type": "error", "message": e.Error()})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = c.Write(ctx, websocket.MessageText, b)
	}
}
