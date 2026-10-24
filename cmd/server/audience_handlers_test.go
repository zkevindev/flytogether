package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/room"
)

func networkRoom(t *testing.T, mux *http.ServeMux) (*http.Cookie, room.View) {
	t.Helper()
	return createRoom(t, mux, `{"names":["房主","朋友","灵矽机器人"],"network":true,"ai":true}`)
}

func TestAudienceConfigureWatchChatAndBlock(t *testing.T) {
	s, mux := handlerTestServer(t)
	host, view := networkRoom(t, mux)
	base := "/api/rooms/" + view.ID
	if view.Audience == nil || !view.Audience.Enabled || view.Audience.Token == "" || !view.Audience.ChatEnabled {
		t.Fatalf("network room should open the audience channel: %+v", view.Audience)
	}
	token := view.Audience.Token

	// Same-screen rooms never expose a watch link.
	sameCookie, sameView := createRoom(t, mux, `{"names":["甲","乙"]}`)
	if w := send(mux, "POST", "/api/rooms/"+sameView.ID+"/audience", `{"enabled":true}`, sameCookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "NETWORK_SETTINGS" {
		t.Fatalf("same-screen audience: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/audience", `{"enabled":true}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 403 {
		t.Fatalf("stranger audience config: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/audience", `not-json`, host); w.Code != 400 {
		t.Fatalf("invalid audience body: %d %s", w.Code, w.Body)
	}

	if w := send(mux, "POST", base+"/watch", `{"token":"forged","name":"观众甲"}`, nil); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_WATCH" {
		t.Fatalf("forged watch token: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"token":"` + token + `","name":""}`, `{"token":"` + token + `","name":"房主"}`} {
		if w := send(mux, "POST", base+"/watch", body, nil); w.Code != 400 {
			t.Fatalf("invalid watch identity accepted: %s %d %s", body, w.Code, w.Body)
		}
	}
	// Settle the join allowance before the rate-limit assertion below.
	s.store.SetAudienceLimits(8, 20)

	w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众甲"}`, nil)
	if w.Code != 200 {
		t.Fatalf("watch join: %d %s", w.Code, w.Body)
	}
	spectator := sessionCookie(t, w)
	joined := decodeView(t, w.Body.Bytes())
	if joined.Audience == nil || joined.Audience.Me == nil || joined.Audience.Me.Role != "spectator" || joined.Audience.Me.Name != "观众甲" {
		t.Fatalf("spectator view: %+v", joined.Audience)
	}
	if joined.Network != nil && joined.Network.Invite != "" {
		t.Fatal("spectator view leaked the invite token")
	}
	if len(joined.ProfileIDs) != 0 || joined.Me != nil {
		t.Fatalf("spectator must not receive seats or profile ids: %+v", joined)
	}
	// Spectators cannot drive the room or read it as a member.
	if w := send(mux, "GET", base+"/watch", "", spectator); w.Code != 200 {
		t.Fatalf("watch snapshot: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "GET", base+"/watch", "", nil); w.Code != 401 {
		t.Fatalf("anonymous watch snapshot: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/commands", `{"command_id":"c1","game_id":"x","action":"roll"}`, spectator); w.Code != 403 {
		t.Fatalf("spectator command: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/ready", `{"ready":true}`, spectator); w.Code != 403 {
		t.Fatalf("spectator ready: %d %s", w.Code, w.Body)
	}

	// Duplicate display names are rejected for other watchers.
	if w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众甲"}`, nil); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "NAME_TAKEN" {
		t.Fatalf("duplicate spectator name: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众乙"}`, nil); w.Code != 200 {
		t.Fatalf("second spectator: %d %s", w.Code, w.Body)
	}
	// Capacity is enforced from the configured limit.
	s.store.SetAudienceLimits(2, 20)
	if w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众丙"}`, nil); w.Code != 403 || errorCode(t, w.Body.Bytes()) != "AUDIENCE_FULL" {
		t.Fatalf("audience capacity: %d %s", w.Code, w.Body)
	}
	// Joins are also rate limited inside a minute.
	s.store.SetAudienceLimits(8, 2)
	if w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众丁"}`, nil); w.Code != 429 || errorCode(t, w.Body.Bytes()) != "AUDIENCE_RATE" {
		t.Fatalf("audience join rate: %d %s", w.Code, w.Body)
	}
	s.store.SetAudienceLimits(8, 20)

	// The host can speak in the same channel and is visible to spectators.
	if w := send(mux, "POST", base+"/chat", `{"id":"host-1","kind":"text","text":"大家好"}`, host); w.Code != 200 {
		t.Fatalf("host chat: %d %s", w.Code, w.Body)
	}

	chat := func(body string) *httptest.ResponseRecorder { return send(mux, "POST", base+"/chat", body, spectator) }
	for name, body := range map[string]string{
		"unknown emoji":     `{"kind":"emoji","emoji":"🍕"}`,
		"markup text":       `{"kind":"text","text":"<b>hi</b>"}`,
		"blank text":        `{"kind":"text","text":"   "}`,
		"long text":         `{"kind":"text","text":"` + strings.Repeat("长", 41) + `"}`,
		"unknown kind":      `{"kind":"voice","text":"hi"}`,
		"unknown player":    `{"kind":"text","text":"加油","target_player_id":"player-9"}`,
		"bot player target": `{"kind":"text","text":"加油","target_player_id":"player-2"}`,
		"long message id":   `{"id":"` + strings.Repeat("x", 81) + `","kind":"text","text":"加油"}`,
	} {
		if w := chat(body); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_CHAT" {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body)
		}
	}
	if w := chat(`{"id":"sp-1","kind":"emoji","emoji":"👏","target_player_id":"player-0"}`); w.Code != 200 {
		t.Fatalf("spectator emoji: %d %s", w.Code, w.Body)
	}
	if w := chat(`{"id":"sp-1","kind":"emoji","emoji":"👏","target_player_id":"player-0"}`); w.Code != 200 {
		t.Fatalf("idempotent chat: %d %s", w.Code, w.Body)
	}
	if w := chat(`{"id":"sp-2","kind":"text","text":"加油"}`); w.Code != 200 {
		t.Fatalf("spectator text: %d %s", w.Code, w.Body)
	}

	// Messages arrive in the personalised view.
	got := decodeView(t, send(mux, "GET", base+"/watch", "", spectator).Body.Bytes())
	if got.Audience == nil || len(got.Audience.Messages) < 3 {
		t.Fatalf("chat not visible to spectator: %+v", got.Audience)
	}

	// Blocking hides a speaker from this spectator only.
	if w := send(mux, "POST", base+"/chat/block", `{"from_id":"nope","blocked":true}`, spectator); w.Code != 400 {
		t.Fatalf("invalid block id: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/chat/block", `{"from_id":"player-0","blocked":true}`, spectator); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body)
	}
	blocked := decodeView(t, w.Body.Bytes())
	for _, m := range blocked.Audience.Messages {
		if m.FromID == "player-0" {
			t.Fatalf("blocked speaker still visible: %+v", m)
		}
	}
	if w := send(mux, "POST", base+"/chat/block", `{"from_id":"player-0","blocked":false}`, spectator); w.Code != 200 {
		t.Fatalf("unblock: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/chat/block", `{"from_id":"player-0","blocked":true}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 401 {
		t.Fatalf("stranger block: %d %s", w.Code, w.Body)
	}

	// Chat spam is throttled.
	for i := 0; i < 3; i++ {
		if w := chat(`{"kind":"emoji","emoji":"💪"}`); w.Code != 200 {
			t.Fatalf("chat %d: %d %s", i, w.Code, w.Body)
		}
	}
	if w := chat(`{"kind":"emoji","emoji":"💪"}`); w.Code != 429 || errorCode(t, w.Body.Bytes()) != "CHAT_RATE" {
		t.Fatalf("chat rate limit: %d %s", w.Code, w.Body)
	}

	// Disabling chat keeps spectators read-only but still watching.
	if w := send(mux, "POST", base+"/audience", `{"chat_enabled":false}`, host); w.Code != 200 {
		t.Fatalf("disable chat: %d %s", w.Code, w.Body)
	}
	if w := chat(`{"kind":"emoji","emoji":"💪"}`); w.Code != 403 || errorCode(t, w.Body.Bytes()) != "CHAT_DISABLED" {
		t.Fatalf("disabled chat: %d %s", w.Code, w.Body)
	}

	// Turning the channel off revokes every spectator immediately.
	if w := send(mux, "POST", base+"/audience", `{"enabled":false}`, host); w.Code != 200 {
		t.Fatalf("disable audience: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "GET", base+"/watch", "", spectator); w.Code != 401 {
		t.Fatalf("revoked spectator snapshot: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/watch", `{"token":"`+token+`","name":"观众甲"}`, nil); w.Code != 403 || errorCode(t, w.Body.Bytes()) != "SPECTATE_CLOSED" {
		t.Fatalf("watch closed channel: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/watch/leave", `{}`, spectator); w.Code != 403 {
		t.Fatalf("leave revoked spectator: %d %s", w.Code, w.Body)
	}

	// Reopen and leave cleanly.
	if w := send(mux, "POST", base+"/audience", `{"enabled":true,"chat_enabled":true}`, host); w.Code != 200 {
		t.Fatalf("reopen audience: %d %s", w.Code, w.Body)
	}
	// The host reads the rotated token through their own session view, not the
	// spectator endpoint.
	if w := send(mux, "GET", base+"/watch", "", host); w.Code != 401 {
		t.Fatalf("host watch snapshot should be spectator-only: %d %s", w.Code, w.Body)
	}
	reopened := currentView(t, mux, host)
	if reopened.Audience == nil || reopened.Audience.Token == "" {
		t.Fatalf("host should see the new watch token: %+v", reopened.Audience)
	}
	rejoin := send(mux, "POST", base+"/watch", `{"token":"`+reopened.Audience.Token+`","name":"观众戊"}`, nil)
	if rejoin.Code != 200 {
		t.Fatalf("rejoin audience: %d %s", rejoin.Code, rejoin.Body)
	}
	rejoinCookie := sessionCookie(t, rejoin)
	// The host is a seated player, so the watch link must not steal the seat.
	if w := send(mux, "POST", base+"/watch", `{"token":"`+reopened.Audience.Token+`","name":"观众己"}`, host); w.Code != 409 || errorCode(t, w.Body.Bytes()) != "SEAT_KEPT" {
		t.Fatalf("host watch: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/watch/leave", `{}`, rejoinCookie); w.Code != 200 {
		t.Fatalf("leave audience: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "GET", base+"/watch", "", rejoinCookie); w.Code != 401 {
		t.Fatalf("watch after leave: %d %s", w.Code, w.Body)
	}
}

func TestGameSocketRequiresMembershipAndStreamsState(t *testing.T) {
	s, mux := handlerTestServer(t)
	host, view := createRoom(t, mux, `{"names":["妈妈","爸爸"]}`)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/game?room_id=" + view.ID

	if _, resp, err := websocket.Dial(t.Context(), wsURL, nil); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous game socket: %v %v", err, resp)
	}
	if _, resp, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"flytogether=stranger"}}}); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("stranger game socket: %v %v", err, resp)
	}
	c, _, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"flytogether=" + host.Value}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	_, data, err := c.Read(t.Context())
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var msg struct {
		Type string    `json:"type"`
		Room room.View `json:"room"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" || msg.Room.ID != view.ID {
		t.Fatalf("unexpected socket message: %v %s", err, data)
	}
	_ = s
}

func TestVoiceSocketsRejectWithoutConfiguration(t *testing.T) {
	_, mux := handlerTestServer(t)
	host, view := createRoom(t, mux, `{"names":["妈妈","爸爸"]}`)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")

	if _, resp, err := websocket.Dial(t.Context(), base+"/ws/voice?room_id="+view.ID, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"flytogether=" + host.Value}}}); err == nil || resp == nil || resp.StatusCode != 503 {
		t.Fatalf("unconfigured voice socket: %v %v", err, resp)
	}
	if _, resp, err := websocket.Dial(t.Context(), base+"/ws/voice?room_id="+view.ID, nil); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous voice socket: %v %v", err, resp)
	}
	if _, resp, err := websocket.Dial(t.Context(), base+"/ws/audience-voice?room_id="+view.ID, nil); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("anonymous audience voice socket: %v %v", err, resp)
	}
	// Voice needs an active match, not a lobby.
	lobbyHost, lobby := networkRoom(t, mux)
	if _, resp, err := websocket.Dial(t.Context(), base+"/ws/voice?room_id="+lobby.ID, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"flytogether=" + lobbyHost.Value}}}); err == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatalf("lobby voice socket: %v %v", err, resp)
	}
}
