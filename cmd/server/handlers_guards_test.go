package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/linx"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestOptionsRejectControlCharactersInRoomName(t *testing.T) {
	_, mux := handlerTestServer(t)
	if w := send(mux, "POST", "/api/rooms", `{"names":["一","二"],"room_name":"周末\u0007局"}`, nil); w.Code != 400 {
		t.Fatalf("control character room name: %d %s", w.Code, w.Body)
	}
}

func TestRestartRejectsUnresolvableProfilesAndNetworkRooms(t *testing.T) {
	_, mux := handlerTestServer(t)
	cookie, view := createRoom(t, mux, `{"names":["甲","乙"]}`)
	base := "/api/rooms/" + view.ID + "/matches"
	if w := send(mux, "POST", base, `{"names":["甲","乙"],"profile_ids":["missing","missing"]}`, cookie); w.Code != 400 {
		t.Fatalf("unknown profile restart: %d %s", w.Code, w.Body)
	}
	netCookie, netView := networkRoom(t, mux)
	if w := send(mux, "POST", "/api/rooms/"+netView.ID+"/matches", `{"names":["甲","乙"]}`, netCookie); w.Code != 400 {
		t.Fatalf("network restart: %d %s", w.Code, w.Body)
	}
}

func TestDiscardAndVoiceSettingsRejectInvalidRequests(t *testing.T) {
	_, mux := handlerTestServer(t)
	cookie, view := createRoom(t, mux, `{"names":["甲","乙"]}`)
	if w := send(mux, "POST", "/api/rooms/"+view.ID+"/discard", `{}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 403 {
		t.Fatalf("stranger discard: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", "/api/rooms/"+view.ID+"/voice-settings", `not-json`, cookie); w.Code != 400 {
		t.Fatalf("invalid voice settings: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "GET", "/ws/game?room_id="+view.ID, "", cookie); w.Code != 426 {
		t.Fatalf("non-upgrade game socket: %d %s", w.Code, w.Body)
	}
}

func TestFamilyHandlersReportStorageFailure(t *testing.T) {
	store, err := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/family", s.family)
	mux.HandleFunc("POST /api/family/profiles", s.saveProfile)
	mux.HandleFunc("POST /api/family/profiles/{id}/delete", s.deleteProfile)
	mux.HandleFunc("POST /api/family/history/clear", s.clearHistory)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "flytogether", Value: "host"}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/family", ""},
		{"POST", "/api/family/profiles", `{"name":"小明","avatar":"bear"}`},
		{"POST", "/api/family/profiles/id/delete", `{}`},
		{"POST", "/api/family/history/clear", `{}`},
	} {
		if w := send(mux, tc.method, tc.path, tc.body, cookie); w.Code != 503 {
			t.Fatalf("%s %s with broken storage: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}

func TestAudienceHandlersRejectMalformedBodies(t *testing.T) {
	_, mux := handlerTestServer(t)
	_, view := networkRoom(t, mux)
	base := "/api/rooms/" + view.ID
	for _, path := range []string{"/watch", "/chat", "/chat/block"} {
		if w := send(mux, "POST", base+path, `{"bad":`, nil); w.Code != 400 {
			t.Fatalf("%s malformed body: %d %s", path, w.Code, w.Body)
		}
	}
	if w := send(mux, "POST", base+"/watch/leave", `{}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 403 {
		t.Fatalf("stranger watch leave: %d %s", w.Code, w.Body)
	}
}

func TestNetworkHandlersRejectMalformedBodies(t *testing.T) {
	_, mux := handlerTestServer(t)
	host, view := networkRoom(t, mux)
	base := "/api/rooms/" + view.ID
	if w := send(mux, "POST", base+"/join", `{"bad":`, nil); w.Code != 400 {
		t.Fatalf("join malformed body: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/ready", `{"bad":`, host); w.Code != 400 {
		t.Fatalf("ready malformed body: %d %s", w.Code, w.Body)
	}
}

func bridgeTestServer(t *testing.T, providerURL string) (*server, *http.ServeMux) {
	t.Helper()
	s, mux := handlerTestServer(t)
	if providerURL != "" {
		s.bridge.Config = linx.Config{URL: providerURL, APIKey: "test-only", Model: "test"}
	}
	return s, mux
}

func dialWithCookie(t *testing.T, url, cookieValue string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"flytogether=" + cookieValue}}})
}

func TestVoiceSocketBridgesAudioWhenConfigured(t *testing.T) {
	// The provider keeps the first response open so the intercom lease stays held.
	hold := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(hold) }) })
	provider, _ := startSpeechProvider(t, speechScript{onRequest: func() { <-hold }})
	_, mux := bridgeTestServer(t, provider)
	host, view := createRoom(t, mux, `{"names":["妈妈","爸爸"]}`)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Invalid companion preferences fail before any upstream session is opened.
	if _, resp, err := dialWithCookie(t, base+"/ws/voice?room_id="+view.ID+"&style=shouty", host.Value); err == nil || resp == nil || resp.StatusCode != 400 {
		t.Fatalf("invalid style: %v %v", err, resp)
	}
	first, _, err := dialWithCookie(t, base+"/ws/voice?room_id="+view.ID+"&style=warm", host.Value)
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	readSocketUntil(t, first, "hello")
	// The intercom lease is single-owner while the first socket is open.
	if _, resp, err := dialWithCookie(t, base+"/ws/voice?room_id="+view.ID, host.Value); err == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatalf("second voice socket: %v %v", err, resp)
	}
	// Closing the first socket releases the lease for the same player.
	first.CloseNow()
	var reopened bool
	for i := 0; i < 100; i++ {
		c, resp, err := dialWithCookie(t, base+"/ws/voice?room_id="+view.ID, host.Value)
		if err == nil {
			reopened = true
			c.CloseNow()
			break
		}
		if resp == nil || resp.StatusCode != 409 {
			t.Fatalf("reopen voice socket: %v %v", err, resp)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !reopened {
		t.Fatal("voice lease was never released")
	}
}

func TestAudienceVoiceSocketBridgesReadOnlySession(t *testing.T) {
	provider, _ := startSpeechProvider(t, speechScript{})
	_, mux := bridgeTestServer(t, provider)
	host, view := networkRoom(t, mux)
	hostView := currentView(t, mux, host)
	if hostView.Audience == nil || hostView.Audience.Token == "" {
		t.Fatal("no watch token")
	}
	joined := send(mux, "POST", "/api/rooms/"+view.ID+"/watch", `{"token":"`+hostView.Audience.Token+`","name":"粉丝"}`, nil)
	if joined.Code != 200 {
		t.Fatalf("watch join: %d %s", joined.Code, joined.Body)
	}
	fan := sessionCookie(t, joined)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, _, err := dialWithCookie(t, base+"/ws/audience-voice?room_id="+view.ID, fan.Value)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	readSocketUntil(t, c, "hello")
	// A second assistant socket for the same spectator is refused.
	if _, resp, err := dialWithCookie(t, base+"/ws/audience-voice?room_id="+view.ID, fan.Value); err == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatalf("second audience voice socket: %v %v", err, resp)
	}
}

func readSocketUntil(t *testing.T, c *websocket.Conn, kind string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		var event map[string]any
		if json.Unmarshal(data, &event) == nil && event["type"] == kind {
			return event
		}
	}
}
