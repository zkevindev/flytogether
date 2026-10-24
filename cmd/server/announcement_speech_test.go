package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/linx"
	"github.com/zkevindev/flytogether/internal/room"
)

// speechScript controls a fake Linx realtime provider for one synthesis.
type speechScript struct {
	onRequest func() // runs after response.create is received
	fail      bool
	audio     []byte
}

// startSpeechProvider serves the minimal Realtime handshake a Speaker needs and
// returns the wss:// endpoint plus the number of upstream sessions opened.
func startSpeechProvider(t *testing.T, script speechScript) (string, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	var connections atomic.Int32
	audio := script.audio
	if audio == nil {
		audio = []byte{1, 0, 2, 0}
	}
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("provider authorization missing")
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"realtime"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		if writeSpeechEvent(ctx, c, map[string]any{"type": "session.created"}) != nil {
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			switch event["type"] {
			case "session.update":
				if writeSpeechEvent(ctx, c, map[string]any{"type": "session.updated"}) != nil {
					return
				}
			case "response.create":
				if script.onRequest != nil {
					script.onRequest()
				}
				if script.fail {
					_ = writeSpeechEvent(ctx, c, map[string]any{"type": "error", "error": map[string]any{"message": "boom"}})
					return
				}
				_ = writeSpeechEvent(ctx, c, map[string]any{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString(audio)})
				_ = writeSpeechEvent(ctx, c, map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				return
			}
		}
	}))
	t.Cleanup(provider.Close)
	return strings.Replace(provider.URL, "https://", "wss://", 1) + "/v1/realtime", &connections
}

// TestMain installs one test-only HTTP client that accepts the self-signed
// certificates of httptest TLS providers. Setting it once avoids racing the
// global client against handlers that dial while a test is being torn down.
func TestMain(m *testing.M) {
	http.DefaultClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	os.Exit(m.Run())
}

func writeSpeechEvent(ctx context.Context, c *websocket.Conn, event map[string]any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, data)
}

func announcementFixture(t *testing.T, speakerURL string, network bool) (*room.Room, *http.ServeMux, *http.Cookie) {
	t.Helper()
	store, err := room.OpenStore(filepath.Join(t.TempDir(), "games.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	s := &server{store: store}
	if speakerURL != "" {
		s.speaker = &linx.Speaker{Config: linx.Config{URL: speakerURL, APIKey: "test-only", Model: "test"}}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/rooms/{id}/announcement", s.announcement)
	opts := game.Options{Names: []string{"小朋友", "爸爸"}}
	opts.Network = network
	r, err := store.Create("host", opts)
	if err != nil {
		t.Fatal(err)
	}
	return r, mux, &http.Cookie{Name: "flytogether", Value: "host"}
}

func announcementBody(t *testing.T, r *room.Room, key, companion string) string {
	t.Helper()
	var state game.State
	if err := json.Unmarshal(r.Snapshot().State, &state); err != nil {
		t.Fatal(err)
	}
	if key == "" {
		key, _ = linx.TurnAnnouncement(state)
	}
	if companion == "" {
		companion = "{}"
	}
	return `{"game_id":"` + state.ID + `","key":"` + key + `","companion":` + companion + `}`
}

func TestAnnouncementGuardsRejectStaleOrForeignRequests(t *testing.T) {
	r, mux, host := announcementFixture(t, "", false)
	path := "/api/rooms/" + r.ID + "/announcement"

	if w := send(mux, "POST", path, announcementBody(t, r, "", ""), &http.Cookie{Name: "flytogether", Value: "stranger"}); w.Code != 401 {
		t.Fatalf("foreign caller: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", path, `not-json`, host); w.Code != 400 {
		t.Fatalf("invalid body: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", path, announcementBody(t, r, "", `{"style":"shouty"}`), host); w.Code != 400 {
		t.Fatalf("invalid companion: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", path, announcementBody(t, r, "wrong-key", ""), host); w.Code != 409 {
		t.Fatalf("wrong key: %d %s", w.Code, w.Body)
	}
	if w := send(mux, "POST", path, `{"game_id":"other-game","key":"x","companion":{}}`, host); w.Code != 409 {
		t.Fatalf("stale game id: %d %s", w.Code, w.Body)
	}
	// Quiet companions stay silent without touching the speaker.
	if w := send(mux, "POST", path, announcementBody(t, r, "", `{"frequency":"quiet"}`), host); w.Code != 204 {
		t.Fatalf("quiet announcement: %d %s", w.Code, w.Body)
	}
	// An unconfigured speaker is reported, not silently skipped.
	if w := send(mux, "POST", path, announcementBody(t, r, "", ""), host); w.Code != 503 {
		t.Fatalf("unconfigured speaker: %d %s", w.Code, w.Body)
	}
}

func TestAnnouncementReturnsSynthesisedAudioAndCachesIt(t *testing.T) {
	url, connections := startSpeechProvider(t, speechScript{})
	r, mux, host := announcementFixture(t, url, false)
	path := "/api/rooms/" + r.ID + "/announcement"
	body := announcementBody(t, r, "", "")

	w := send(mux, "POST", path, body, host)
	if w.Code != 200 {
		t.Fatalf("announcement: %d %s", w.Code, w.Body)
	}
	var result struct {
		Key        string `json:"key"`
		Text       string `json:"text"`
		Audio      string `json:"audio"`
		SampleRate int    `json:"sample_rate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("announcement payload: %v %s", err, w.Body)
	}
	audio, err := base64.StdEncoding.DecodeString(result.Audio)
	if err != nil || len(audio) != 4 || result.SampleRate != 24000 || result.Text == "" {
		t.Fatalf("unexpected audio: %+v %v", result, err)
	}
	if result.Key == "" || !strings.Contains(result.Text, "小朋友") {
		t.Fatalf("announcement text: %+v", result)
	}
	// The same turn line is served from the speaker cache, not a new session.
	if w = send(mux, "POST", path, body, host); w.Code != 200 {
		t.Fatalf("cached announcement: %d %s", w.Code, w.Body)
	}
	if connections.Load() != 1 {
		t.Fatalf("cache miss opened %d upstream sessions", connections.Load())
	}
}

func TestAnnouncementRejectsResultAfterTheTurnChanged(t *testing.T) {
	var r *room.Room
	var host *http.Cookie
	var mux *http.ServeMux
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	url, _ := startSpeechProvider(t, speechScript{onRequest: func() {
		started <- struct{}{}
		<-release
	}})
	r, mux, host = announcementFixture(t, url, false)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	var state game.State
	if err := json.Unmarshal(r.Snapshot().State, &state); err != nil {
		t.Fatal(err)
	}
	response := make(chan int, 1)
	go func() {
		req, err := http.NewRequest("POST", ts.URL+"/api/rooms/"+r.ID+"/announcement", strings.NewReader(announcementBody(t, r, "", "")))
		if err != nil {
			response <- 0
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(host)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			response <- 0
			return
		}
		defer resp.Body.Close()
		response <- resp.StatusCode
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("speaker never started synthesis")
	}
	// The turn advances while the provider is still synthesising.
	if _, err := r.DoAs(room.Command{ID: "roll-during-tts", GameID: state.ID, Version: state.Version, Action: "roll"}, "host"); err != nil {
		t.Fatalf("advance turn: %v", err)
	}
	close(release)
	select {
	case code := <-response:
		if code != 409 {
			t.Fatalf("stale synthesis returned %d, want 409", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stale announcement never returned")
	}
}

func TestAnnouncementReportsSynthesisFailureAndBusySpeaker(t *testing.T) {
	url, _ := startSpeechProvider(t, speechScript{fail: true})
	r, mux, host := announcementFixture(t, url, false)
	if w := send(mux, "POST", "/api/rooms/"+r.ID+"/announcement", announcementBody(t, r, "", ""), host); w.Code != 503 {
		t.Fatalf("failed synthesis: %d %s", w.Code, w.Body)
	}

	// A second line with different text is rejected while the first is in flight.
	hold := make(chan struct{})
	started := make(chan struct{}, 1)
	url2, _ := startSpeechProvider(t, speechScript{onRequest: func() {
		started <- struct{}{}
		<-hold
	}})
	r2, mux2, host2 := announcementFixture(t, url2, false)
	first := make(chan int, 1)
	go func() {
		w := send(mux2, "POST", "/api/rooms/"+r2.ID+"/announcement", announcementBody(t, r2, "", ""), host2)
		first <- w.Code
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("speaker never started synthesis")
	}
	// Concise style produces different text for the same turn, so this call is a
	// genuine concurrent request instead of joining the pending one.
	if w := send(mux2, "POST", "/api/rooms/"+r2.ID+"/announcement", announcementBody(t, r2, "", `{"style":"concise"}`), host2); w.Code != 429 {
		t.Fatalf("busy speaker: %d %s", w.Code, w.Body)
	}
	close(hold)
	if code := <-first; code != 200 {
		t.Fatalf("first synthesis returned %d", code)
	}
}
