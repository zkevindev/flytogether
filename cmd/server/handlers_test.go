package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/linx"
	"github.com/zkevindev/flytogether/internal/room"
)

// handlerTestServer wires the public HTTP routes to a real SQLite-backed store.
// It intentionally skips main() so handlers are exercised through their real
// request/response behaviour instead of mocked internals.
func handlerTestServer(t *testing.T) (*server, *http.ServeMux) {
	t.Helper()
	store, err := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.ConfigureDiagnostics(room.DiagnosticConfig{Enabled: true, Transcripts: true, Retention: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	s := &server{store: store, bridge: &linx.Bridge{}}
	mux := http.NewServeMux()
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
	return s, mux
}

func send(mux *http.ServeMux, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://example.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "flytogether" && c.Value != "" {
			return &http.Cookie{Name: c.Name, Value: c.Value}
		}
	}
	t.Fatal("expected a flytogether session cookie")
	return nil
}

func decodeView(t *testing.T, body []byte) room.View {
	t.Helper()
	var v room.View
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("invalid room view: %v (%s)", err, body)
	}
	return v
}

// currentView reads the caller's live room through the session endpoint, which
// is the only route that re-personalises a view without mutating it.
func currentView(t *testing.T, mux *http.ServeMux, cookie *http.Cookie) room.View {
	t.Helper()
	w := send(mux, "GET", "/api/session", "", cookie)
	if w.Code != 200 {
		t.Fatalf("session: %d %s", w.Code, w.Body)
	}
	var body struct {
		Room *room.View `json:"room"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Room == nil {
		t.Fatalf("invalid session body: %v %s", err, w.Body)
	}
	return *body.Room
}

func viewState(t *testing.T, v room.View) game.State {
	t.Helper()
	var st game.State
	if err := json.Unmarshal(v.State, &st); err != nil {
		t.Fatalf("invalid game state: %v", err)
	}
	return st
}

func createRoom(t *testing.T, mux *http.ServeMux, body string) (*http.Cookie, room.View) {
	t.Helper()
	w := send(mux, "POST", "/api/rooms", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create room: %d %s", w.Code, w.Body)
	}
	return sessionCookie(t, w), decodeView(t, w.Body.Bytes())
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("invalid error body: %s", body)
	}
	return e.Code
}

func TestRoomSessionLifecycleAndCommandValidation(t *testing.T) {
	_, mux := handlerTestServer(t)

	if w := send(mux, "GET", "/api/session", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"room":null`) {
		t.Fatalf("anonymous session: %d %s", w.Code, w.Body)
	}

	// Every option rejection keeps the room from being created.
	for name, body := range map[string]string{
		"not json":       `not-json`,
		"unknown field":  `{"names":["一","二"],"extra":true}`,
		"bad mode":       `{"names":["一","二"],"mode":"tournament"}`,
		"bad board":      `{"names":["一","二"],"board":"moon"}`,
		"too few seats":  `{"names":["一"]}`,
		"too many seats": `{"names":["一","二","三","四","五"]}`,
		"duplicate name": `{"names":["一","一"]}`,
		"empty name":     `{"names":["","二"]}`,
		"long name":      `{"names":["一二三四五六七八九十一","二"]}`,
		"control char":   `{"names":["一\t二","三"]}`,
		"long room name": `{"names":["一","二"],"room_name":"一二三四五六七八九十一二三四五六七八九十一"}`,
		"bad difficulty": `{"names":["一","二"],"difficulty":"insane"}`,
		"bad preset":     `{"names":["一","二"],"preset":"chaos"}`,
	} {
		w := send(mux, "POST", "/api/rooms", body, nil)
		if w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body)
		}
	}

	cookie, view := createRoom(t, mux, `{"names":[" 爸爸 ","妈妈"],"room_name":"  周末局  ","difficulty":"hard","board":"long"}`)
	if view.Status != "playing" || view.Name != "周末局" || view.Network != nil {
		t.Fatalf("unexpected created room: %+v", view)
	}
	state := viewState(t, view)
	if state.Players[0].Name != "爸爸" || state.Difficulty != "hard" || state.Board.ID != "long" {
		t.Fatalf("options not applied: %+v", state)
	}

	if w := send(mux, "GET", "/api/session", "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), view.ID) {
		t.Fatalf("session with cookie: %d %s", w.Code, w.Body)
	}

	// The same browser keeps its room instead of silently creating another one.
	if w := send(mux, "POST", "/api/rooms", `{"names":["爸爸","妈妈"]}`, cookie); w.Code != 201 {
		t.Fatalf("repeat create: %d %s", w.Code, w.Body)
	} else if again := decodeView(t, w.Body.Bytes()); again.ID != view.ID {
		t.Fatalf("repeat create replaced room: %s != %s", again.ID, view.ID)
	}

	// /api/rooms/new replaces the same-screen room explicitly.
	w := send(mux, "POST", "/api/rooms/new", `{"names":["甲","乙"]}`, cookie)
	if w.Code != 201 {
		t.Fatalf("new room: %d %s", w.Code, w.Body)
	}
	fresh := decodeView(t, w.Body.Bytes())
	if fresh.ID == view.ID {
		t.Fatal("/api/rooms/new reused the previous room")
	}

	cmdPath := "/api/rooms/" + fresh.ID + "/commands"
	// /api/rooms/new opens a lobby, so the host starts the match first.
	st := viewState(t, fresh)
	start := `{"command_id":"start-fresh","game_id":"` + st.ID + `","expected_version":` + strconv.Itoa(st.Version) + `,"action":"start"}`
	started := send(mux, "POST", cmdPath, start, cookie)
	if started.Code != 200 {
		t.Fatalf("start: %d %s", started.Code, started.Body)
	}
	st = viewState(t, decodeView(t, started.Body.Bytes()))
	roll := `{"command_id":"cmd-roll","game_id":"` + st.ID + `","expected_version":` + strconv.Itoa(st.Version) + `,"action":"roll"}`
	if w := send(mux, "POST", cmdPath, roll, cookie); w.Code != 200 {
		t.Fatalf("roll: %d %s", w.Code, w.Body)
	}
	// Retrying the same command id is idempotent even after the turn advanced.
	if w := send(mux, "POST", cmdPath, roll, cookie); w.Code != 200 {
		t.Fatalf("idempotent retry: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", cmdPath, `{"command_id":"cmd-roll","game_id":"`+st.ID+`","expected_version":1,"action":"move","piece_id":"p0-0"}`, cookie); w.Code != 409 || errorCode(t, w.Body.Bytes()) != "COMMAND_CONFLICT" {
		t.Fatalf("command conflict: %d %s", w.Code, w.Body)
	}
	live := viewState(t, currentView(t, mux, cookie))
	if w := send(mux, "POST", cmdPath, `{"command_id":"cmd-stale","game_id":"`+live.ID+`","expected_version":`+strconv.Itoa(live.Version+9)+`,"action":"roll"}`, cookie); w.Code != 409 || errorCode(t, w.Body.Bytes()) != "STALE_STATE" {
		t.Fatalf("stale command: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", cmdPath, `{"command_id":"cmd-token","game_id":"`+live.ID+`","expected_version":`+strconv.Itoa(live.Version)+`,"action":"roll","action_token":"forged"}`, cookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_COMMAND" {
		t.Fatalf("click token: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", cmdPath, `{"command_id":"cmd-bogus","game_id":"`+live.ID+`","expected_version":`+strconv.Itoa(live.Version)+`,"action":"teleport"}`, cookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_ACTION" {
		t.Fatalf("unknown action: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", cmdPath, `{"game_id":"`+live.ID+`","action":"roll"}`, cookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_COMMAND" {
		t.Fatalf("missing command id: %d %s", w.Code, w.Body)
	}
	// Unparseable payload and wrong media type are rejected before any mutation.
	if w := send(mux, "POST", cmdPath, `{"command_id":[1]}`, cookie); w.Code != 400 {
		t.Fatalf("type mismatch accepted: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "https://example.test"+cmdPath, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != 400 {
		t.Fatalf("non-JSON accepted: %d %s", rec.Code, rec.Body)
	}

	// Play one real move when the rules offer one, so roll+move are both covered.
	for i := 0; i < 24; i++ {
		current := viewState(t, currentView(t, mux, cookie))
		if current.Phase == "finished" {
			break
		}
		if len(current.Legal) > 0 {
			move := `{"command_id":"cmd-move-` + strconv.Itoa(i) + `","game_id":"` + current.ID + `","expected_version":` + strconv.Itoa(current.Version) + `,"action":"move","piece_id":"` + current.Legal[0].PieceID + `"}`
			if w := send(mux, "POST", cmdPath, move, cookie); w.Code != 200 {
				t.Fatalf("move: %d %s", w.Code, w.Body)
			}
			break
		}
		body := `{"command_id":"cmd-roll-` + strconv.Itoa(i) + `","game_id":"` + current.ID + `","expected_version":` + strconv.Itoa(current.Version) + `,"action":"roll"}`
		if w := send(mux, "POST", cmdPath, body, cookie); w.Code != 200 {
			t.Fatalf("roll loop: %d %s", w.Code, w.Body)
		}
	}

	// Restart validates its options and returns the new match.
	if w := send(mux, "POST", "/api/rooms/"+fresh.ID+"/matches", `{"names":["甲","乙"],"room_name":"下一局"}`, cookie); w.Code != 200 {
		t.Fatalf("restart: %d %s", w.Code, w.Body)
	} else if restarted := decodeView(t, w.Body.Bytes()); restarted.Name != "下一局" || restarted.Status != "playing" {
		t.Fatalf("restart view: %+v", restarted)
	}
	if w := send(mux, "POST", "/api/rooms/"+fresh.ID+"/matches", `{"names":["甲"]}`, cookie); w.Code != 400 {
		t.Fatalf("invalid restart accepted: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", "/api/rooms/missing/matches", `{"names":["甲","乙"]}`, cookie); w.Code != 401 {
		t.Fatalf("restart on missing room: %d %s", w.Code, w.Body)
	}

	// A different browser cannot drive this room.
	if w := send(mux, "POST", cmdPath, roll, &http.Cookie{Name: "flytogether", Value: "someone-else"}); w.Code != 401 {
		t.Fatalf("foreign command: %d %s", w.Code, w.Body)
	}

	// Discard removes the room and the session points at nothing again.
	bare := httptest.NewRequest("POST", "https://example.test/api/rooms/"+fresh.ID+"/discard", strings.NewReader(`{}`))
	bare.AddCookie(cookie)
	bareRec := httptest.NewRecorder()
	mux.ServeHTTP(bareRec, bare)
	if bareRec.Code != 400 {
		t.Fatalf("discard without JSON content type: %d", bareRec.Code)
	}
	if w := send(mux, "POST", "/api/rooms/"+fresh.ID+"/discard", `{}`, cookie); w.Code != 200 {
		t.Fatalf("discard: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "GET", "/api/session", "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"room":null`) {
		t.Fatalf("session after discard: %d %s", w.Code, w.Body)
	}
}

func TestVoiceSettingsAndAnnouncementGuards(t *testing.T) {
	_, mux := handlerTestServer(t)
	cookie, view := createRoom(t, mux, `{"names":["妈妈","爸爸"]}`)
	base := "/api/rooms/" + view.ID

	if w := send(mux, "POST", base+"/voice-settings", `{"wake_enabled":true,"wake_word":"小灵同学"}`, cookie); w.Code != 200 {
		t.Fatalf("voice settings: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"wake_word":"x"}`, `{"wake_word":"小灵!"}`, `{"wake_word":"  "}`} {
		if w := send(mux, "POST", base+"/voice-settings", body, cookie); w.Code != 400 {
			t.Fatalf("invalid wake word accepted: %s %d %s", body, w.Code, w.Body)
		}
	}

	netCookie, netView := createRoom(t, mux, `{"names":["房主","朋友"],"network":true}`)
	if w := send(mux, "POST", "/api/rooms/"+netView.ID+"/voice-settings", `{"wake_word":"小灵同学"}`, netCookie); w.Code != 403 {
		t.Fatalf("network voice settings: %d %s", w.Code, w.Body)
	}

	state := viewState(t, view)
	key, _ := linx.TurnAnnouncement(state)
	if w := send(mux, "POST", base+"/announcement", `{"game_id":"`+state.ID+`","key":"wrong","companion":{}}`, cookie); w.Code != 409 {
		t.Fatalf("stale announcement key: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/announcement", `{"game_id":"`+state.ID+`","key":"`+key+`","companion":{"frequency":"quiet"}}`, cookie); w.Code != 204 {
		t.Fatalf("quiet announcement: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/announcement", `{"game_id":"`+state.ID+`","key":"`+key+`","companion":{}}`, cookie); w.Code != 503 {
		t.Fatalf("unconfigured speaker: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/announcement", `{"game_id":"`+state.ID+`","key":"`+key+`","companion":{"style":"shouty"}}`, cookie); w.Code != 400 {
		t.Fatalf("invalid companion style: %d %s", w.Code, w.Body)
	}
}

func TestNetworkLobbyJoinReadyLeaveAndHostRules(t *testing.T) {
	_, mux := handlerTestServer(t)
	hostCookie, view := createRoom(t, mux, `{"names":["房主","朋友","灵矽机器人"],"network":true,"ai":true}`)
	if view.Status != "waiting" || view.Network == nil || view.Network.Invite == "" {
		t.Fatalf("network room not waiting with invite: %+v", view)
	}
	invite := view.Network.Invite
	base := "/api/rooms/" + view.ID

	if w := send(mux, "POST", base+"/join", `{"invite":"wrong","name":"朋友"}`, nil); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_INVITE" {
		t.Fatalf("bad invite: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"invite":"` + invite + `","name":""}`, `{"invite":"` + invite + `","name":"一二三四五六七八九十一"}`, `{"invite":"` + invite + `","name":"房主"}`, `{"invite":"` + invite + `","name":"a\u0001b"}`} {
		if w := send(mux, "POST", base+"/join", body, nil); w.Code != 400 {
			t.Fatalf("invalid join accepted: %s %d %s", body, w.Code, w.Body)
		}
	}
	w := send(mux, "POST", base+"/join", `{"invite":"`+invite+`","name":"朋友"}`, nil)
	if w.Code != 200 {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	friendCookie := sessionCookie(t, w)
	if joined := decodeView(t, w.Body.Bytes()); joined.Me == nil || joined.Network == nil {
		t.Fatalf("join view: %+v", joined)
	}
	// The remaining seat belongs to the bot, so one more human is refused.
	if w := send(mux, "POST", base+"/join", `{"invite":"`+invite+`","name":"路人"}`, nil); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "ROOM_FULL" {
		t.Fatalf("full room: %d %s", w.Code, w.Body)
	}
	// Re-joining with the same cookie keeps the seat.
	if w := send(mux, "POST", base+"/join", `{"invite":"`+invite+`","name":"朋友"}`, friendCookie); w.Code != 200 {
		t.Fatalf("rejoin: %d %s", w.Code, w.Body)
	}

	if w := send(mux, "POST", base+"/ready", `{"ready":true}`, friendCookie); w.Code != 200 {
		t.Fatalf("ready: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/ready", `{"ready":false}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 401 {
		t.Fatalf("stranger ready: %d %s", w.Code, w.Body)
	}
	// Humans are not connected to the socket here, so start is rejected.
	lobbyState := viewState(t, currentView(t, mux, hostCookie))
	startBody := `{"command_id":"start-1","game_id":"` + lobbyState.ID + `","expected_version":` + strconv.Itoa(lobbyState.Version) + `,"action":"start"}`
	if w := send(mux, "POST", base+"/commands", startBody, hostCookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "PLAYERS_NOT_READY" {
		t.Fatalf("start without online players: %d %s", w.Code, w.Body)
	}
	// Non-host cannot control the match.
	if w := send(mux, "POST", base+"/commands", `{"command_id":"start-2","game_id":"x","action":"start"}`, friendCookie); w.Code != 403 || errorCode(t, w.Body.Bytes()) != "HOST_ONLY" {
		t.Fatalf("guest start: %d %s", w.Code, w.Body)
	}
	// Non-host cannot hand over ownership either.
	if w := send(mux, "POST", base+"/commands", `{"command_id":"transfer-1","game_id":"x","action":"transfer_host","target_player_id":"player-1"}`, friendCookie); w.Code != 403 {
		t.Fatalf("guest transfer: %d %s", w.Code, w.Body)
	}

	if w := send(mux, "POST", base+"/leave", `{}`, hostCookie); w.Code != 403 || errorCode(t, w.Body.Bytes()) != "HOST_ONLY" {
		t.Fatalf("host leave: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/leave", `{}`, friendCookie); w.Code != 200 {
		t.Fatalf("guest leave: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/leave", `{}`, &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 401 {
		t.Fatalf("stranger leave: %d %s", w.Code, w.Body)
	}

	// Ending is host-only and closes the lobby for new joins/watchers.
	endState := viewState(t, currentView(t, mux, hostCookie))
	if w := send(mux, "POST", base+"/commands", `{"command_id":"end-1","game_id":"`+endState.ID+`","expected_version":`+strconv.Itoa(endState.Version)+`,"action":"end"}`, hostCookie); w.Code != 200 {
		t.Fatalf("end: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/join", `{"invite":"`+invite+`","name":"迟到"}`, nil); w.Code != 400 {
		t.Fatalf("join ended room: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", base+"/watch", `{"token":"`+view.Network.Invite+`","name":"观众"}`, nil); w.Code != 400 {
		t.Fatalf("watch ended room: %d %s", w.Code, w.Body)
	}
}

func TestFamilyProfilesAndRoomPrefill(t *testing.T) {
	_, mux := handlerTestServer(t)

	w := send(mux, "GET", "/api/family", "", nil)
	if w.Code != 200 {
		t.Fatalf("family: %d %s", w.Code, w.Body)
	}
	cookie := sessionCookie(t, w)
	if fam := decodeFamily(t, w.Body.Bytes()); len(fam.Profiles) != 0 || fam.Total != 0 {
		t.Fatalf("new family not empty: %+v", fam)
	}

	for _, body := range []string{`{"name":"","avatar":"bear"}`, `{"name":"一二三四五六七八九十一","avatar":"bear"}`, `{"name":"小明","avatar":""}`, `{"name":"小明","avatar":"https://example.test/a.png"}`} {
		if w := send(mux, "POST", "/api/family/profiles", body, cookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "INVALID_PROFILE" {
			t.Fatalf("invalid profile accepted: %s %d %s", body, w.Code, w.Body)
		}
	}
	w = send(mux, "POST", "/api/family/profiles", `{"name":" 小明 ","avatar":"bear"}`, cookie)
	if w.Code != 200 {
		t.Fatalf("save profile: %d %s", w.Code, w.Body)
	}
	first := decodeProfile(t, w.Body.Bytes())
	if first.ID == "" || first.Name != "小明" || first.Avatar != "bear" {
		t.Fatalf("profile not normalised: %+v", first)
	}
	w = send(mux, "POST", "/api/family/profiles", `{"name":"小红","avatar":"cat"}`, cookie)
	if w.Code != 200 {
		t.Fatalf("second profile: %d %s", w.Code, w.Body)
	}
	second := decodeProfile(t, w.Body.Bytes())

	// Profiles prefill a same-screen match.
	if w := send(mux, "POST", "/api/rooms", `{"names":["位一","位二"],"profile_ids":["`+first.ID+`","`+second.ID+`"]}`, cookie); w.Code != 201 {
		t.Fatalf("room with profiles: %d %s", w.Code, w.Body)
	} else if st := viewState(t, decodeView(t, w.Body.Bytes())); st.Players[0].Name != "小明" || st.Players[1].Name != "小红" {
		t.Fatalf("profiles not applied: %+v", st.Players)
	}
	if w := send(mux, "POST", "/api/rooms", `{"names":["位一","位二"],"profile_ids":["missing","`+second.ID+`"]}`, cookie); w.Code != 400 {
		t.Fatalf("unknown profile accepted: %d %s", w.Code, w.Body)
	}

	if w := send(mux, "POST", "/api/family/profiles/"+first.ID+"/delete", `{}`, cookie); w.Code != 200 {
		t.Fatalf("delete profile: %d %s", w.Code, w.Body)
	}
	// Deleting an unknown id is idempotent and leaves the other profiles alone.
	if w := send(mux, "POST", "/api/family/profiles/missing/delete", `{}`, cookie); w.Code != 200 {
		t.Fatalf("idempotent delete: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", "/api/family/profiles", `{"id":"missing","name":"小明","avatar":"bear"}`, cookie); w.Code != 400 || errorCode(t, w.Body.Bytes()) != "PROFILE_NOT_FOUND" {
		t.Fatalf("update missing profile: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", "/api/family/profiles", `{"id":"`+second.ID+`","name":"小红红","avatar":"deer"}`, cookie); w.Code != 200 {
		t.Fatalf("update profile: %d %s", w.Code, w.Body)
	} else if updated := decodeProfile(t, w.Body.Bytes()); updated.Name != "小红红" || updated.Avatar != "deer" {
		t.Fatalf("profile not updated: %+v", updated)
	}
	if w := send(mux, "POST", "/api/family/history/clear", `{}`, cookie); w.Code != 200 {
		t.Fatalf("clear history: %d %s", w.Code, w.Body)
	}
	// Anonymous callers still receive a family bound to a fresh cookie.
	if w := send(mux, "POST", "/api/family/history/clear", `{}`, &http.Cookie{Name: "flytogether", Value: "nobody"}); w.Code != 200 {
		t.Fatalf("anonymous clear: %d %s", w.Code, w.Body)
	}
}

func decodeFamily(t *testing.T, body []byte) room.Family {
	t.Helper()
	var f room.Family
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatalf("invalid family body: %s", body)
	}
	return f
}

func decodeProfile(t *testing.T, body []byte) room.Profile {
	t.Helper()
	var p room.Profile
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("invalid profile body: %s", body)
	}
	return p
}
