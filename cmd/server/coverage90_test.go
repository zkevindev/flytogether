package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/linx"
)

func coverage90Response(t *testing.T, w *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, w.Body)
	}
	if w.Code != status || body.Code != code || body.Message != message {
		t.Fatalf("got %d %+v; want %d code=%q message=%q", w.Code, body, status, code, message)
	}
}

func TestCoverage90AdminFailureResponses(t *testing.T) {
	s, mux := adminTestServer(t)
	coverage90Response(t, adminRequest(mux, "POST", "/api/admin/login", `{`, nil, ""), 400, "BAD_REQUEST", "请求内容无效")
	if len(s.admin.failures) != 0 || len(s.admin.sessions) != 0 {
		t.Fatal("malformed login mutated authentication state")
	}
	c := adminLoginCookie(t, mux)
	for _, path := range []string{"/api/admin/matches?before=-1", "/api/admin/matches/nope/events?offset=100001"} {
		coverage90Response(t, adminRequest(mux, "GET", path, "", c, ""), 400, "BAD_REQUEST", "分页参数无效")
	}
	coverage90Response(t, adminRequest(mux, "GET", "/api/admin/matches?q="+strings.Repeat("a", 161), "", c, ""), 400, "BAD_REQUEST", "搜索内容过长")
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/matches", "/api/admin/matches/nope", "/api/admin/matches/nope/events"} {
		coverage90Response(t, adminRequest(mux, "GET", path, "", c, ""), 503, "", "排障记录暂时不可读，请稍后重试")
	}
	s.admin = nil
	coverage90Response(t, adminRequest(mux, "GET", "/api/admin/session", "", c, ""), 503, "ADMIN_DISABLED", "管理后台尚未启用，请在服务器配置管理员密钥")
}

func TestCoverage90AdminCapacityAndExpiredFailures(t *testing.T) {
	s, mux := adminTestServer(t)
	for i := 0; i < 64; i++ {
		s.admin.sessions[sha256.Sum256([]byte(fmt.Sprint(i)))] = adminSession{expires: time.Now().Add(time.Hour)}
	}
	s.admin.failures["expired-peer"] = loginAttempts{count: 8, until: time.Now().Add(-time.Minute)}
	w := adminRequest(mux, "POST", "/api/admin/login", `{"key":"`+testAdminKey+`"}`, nil, "")
	coverage90Response(t, w, 429, "", "管理会话已满，请退出其他会话或稍后再试")
	if len(w.Result().Cookies()) != 0 || len(s.admin.sessions) != 64 {
		t.Fatal("full session table issued a session")
	}
	if _, ok := s.admin.failures["expired-peer"]; ok {
		t.Fatal("expired rate limit was retained")
	}
}

func TestCoverage90DiagnosticConfigurationFailure(t *testing.T) {
	clearConfigEnv(t)
	s, _ := handlerTestServer(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_ACCESS_KEY", testAdminKey)
	got, err := newServer(s.store, cfg)
	if err == nil || got != nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed diagnostics store: server=%v err=%v", got, err)
	}
}

func TestCoverage90FamilyMalformedRequests(t *testing.T) {
	_, mux := handlerTestServer(t)
	saved := send(mux, "POST", "/api/family/profiles", `{"name":"小明","avatar":"bear"}`, nil)
	if saved.Code != 200 {
		t.Fatalf("seed profile: %d %s", saved.Code, saved.Body)
	}
	c := sessionCookie(t, saved)
	p := decodeProfile(t, saved.Body.Bytes())
	before := send(mux, "GET", "/api/family", "", c)
	for _, path := range []string{"/api/family/profiles", "/api/family/profiles/" + p.ID + "/delete", "/api/family/history/clear"} {
		coverage90Response(t, send(mux, "POST", path, `{`, c), 400, "BAD_REQUEST", "请求内容无效")
	}
	after := send(mux, "GET", "/api/family", "", c)
	if before.Code != 200 || after.Code != 200 || before.Body.String() != after.Body.String() {
		t.Fatalf("malformed requests changed family: before=%s after=%s", before.Body, after.Body)
	}
}

func TestCoverage90ReadyAndVoiceOwnership(t *testing.T) {
	_, mux := handlerTestServer(t)
	c, v := createRoom(t, mux, `{"names":["甲","乙"]}`)
	base := "/api/rooms/" + v.ID
	coverage90Response(t, send(mux, "POST", base+"/ready", `{"ready":true}`, c), 403, "NOT_AUTHORIZED", "请先入座")
	before := currentView(t, mux, c)
	coverage90Response(t, send(mux, "POST", base+"/voice-settings", `{"wake_enabled":true,"wake_word":"小灵同学"}`, &http.Cookie{Name: "flytogether", Value: "stranger"}), 401, "NOT_AUTHORIZED", "房间已过期，请重新开始")
	after := currentView(t, mux, c)
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(b) != string(a) {
		t.Fatal("unauthorized settings request changed room")
	}
}

func TestCoverage90AudienceVoiceGuards(t *testing.T) {
	s, mux := handlerTestServer(t)
	host, v := networkRoom(t, mux)
	hostView := currentView(t, mux, host)
	joined := send(mux, "POST", "/api/rooms/"+v.ID+"/watch", `{"token":"`+hostView.Audience.Token+`","name":"观众"}`, nil)
	if joined.Code != 200 {
		t.Fatalf("watch join: %d %s", joined.Code, joined.Body)
	}
	fan := sessionCookie(t, joined)
	path := "/ws/audience-voice?room_id=" + v.ID
	coverage90Response(t, send(mux, "GET", path, "", fan), 503, "", "灵犀尚未配置，仍可安静观看棋局")
	// Invalid preferences are rejected before upgrading or dialing any provider.
	s.bridge.Config = linx.Config{URL: "ws://127.0.0.1:1", APIKey: "local-only", Model: "test"}
	coverage90Response(t, send(mux, "GET", path+"&style=invalid", "", fan), 400, "BAD_REQUEST", "请选择温暖、教练或简洁风格")
	coverage90Response(t, send(mux, "POST", "/api/rooms/"+v.ID+"/voice-settings", `{}`, fan), 403, "NOT_AUTHORIZED", "观众不能操作棋局或管理房间")
	r := s.store.AudienceRoom(fan.Value, v.ID)
	lease, err := r.VoiceForSpectator(fan.Value)
	if err != nil {
		t.Fatalf("rejected socket leaked spectator lease: %v", err)
	}
	lease.Close()
}
