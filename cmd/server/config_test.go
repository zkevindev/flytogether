package main

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/room"
)

func TestPositiveEnvBounds(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      int
		wantErr   bool
	}{
		{"unset", "", 60, false},
		{"valid", "120", 120, false},
		{"min", "5", 5, false},
		{"max", "600", 600, false},
		{"not a number", "sixty", 0, true},
		{"below min", "4", 0, true},
		{"above max", "601", 0, true},
		{"negative", "-30", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLY_TEST_DELAY", tc.raw)
			got, err := positiveEnv("FLY_TEST_DELAY", 60, 5, 600)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("positiveEnv(%q) = %d, %v", tc.raw, got, err)
			}
		})
	}
}

func TestEnvFallbackAndLoadEnvFile(t *testing.T) {
	t.Setenv("FLY_TEST_ENV", "from-process")
	if got := env("FLY_TEST_ENV", "fallback"); got != "from-process" {
		t.Fatalf("env() ignored the process value: %q", got)
	}
	t.Setenv("FLY_TEST_EMPTY", "")
	if got := env("FLY_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Fatalf("env() for an empty value: %q", got)
	}

	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		"# a comment",
		"",
		"FLY_LOAD_ONE=1",
		"FLY_LOAD_TWO = \"two words\"",
		"FLY_LOAD_THREE='quoted'",
		"FLY_LOAD_FOUR=from-file",
		"FLY_LOAD_BROKEN",
		"=novalue",
		"   ",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLY_LOAD_FOUR", "from-process")
	// Ensure a clean slate for the keys this file defines.
	for _, key := range []string{"FLY_LOAD_ONE", "FLY_LOAD_TWO", "FLY_LOAD_THREE"} {
		os.Unsetenv(key)
	}
	loadEnv(path)
	for key, want := range map[string]string{
		"FLY_LOAD_ONE":   "1",
		"FLY_LOAD_TWO":   "two words",
		"FLY_LOAD_THREE": "quoted",
		"FLY_LOAD_FOUR":  "from-process",
	} {
		if got := os.Getenv(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := os.LookupEnv("FLY_LOAD_BROKEN"); ok {
		t.Fatal("malformed line created a variable")
	}
	// A missing file is ignored rather than fatal, so small deployments can run
	// with pure environment variables.
	loadEnv(filepath.Join(t.TempDir(), "absent.env"))
}

func TestOriginOK(t *testing.T) {
	s := &server{publicHost: "public.test"}
	for _, tc := range []struct {
		name, origin string
		want         bool
	}{
		{"no origin", "", true},
		{"same host", "https://example.test", true},
		{"public host", "https://public.test", true},
		{"foreign host", "https://evil.test", false},
		{"malformed", "://broken", false},
		{"empty scheme host", "/relative", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://example.test/api/session", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := s.originOK(r); got != tc.want {
				t.Fatalf("originOK(%q) = %t", tc.origin, got)
			}
		})
	}
	s.publicHost = ""
	r := httptest.NewRequest("GET", "https://example.test/", nil)
	r.Header.Set("Origin", "https://example.test")
	if !s.originOK(r) {
		t.Fatal("same host must be allowed without a public host")
	}
}

func TestFailureStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"HOST_ONLY", 403},
		{"NOT_YOUR_TURN", 403},
		{"NOT_AUTHORIZED", 403},
		{"SEAT_DELEGATED", 403},
		{"SPECTATE_CLOSED", 403},
		{"AUDIENCE_FULL", 403},
		{"CHAT_DISABLED", 403},
		{"STALE_STATE", 409},
		{"VOICE_BUSY", 409},
		{"COMMAND_CONFLICT", 409},
		{"HOST_GRACE_PERIOD", 409},
		{"HOST_ONLINE", 409},
		{"SEAT_KEPT", 409},
		{"AUDIENCE_RATE", 429},
		{"CHAT_RATE", 429},
		{"SAVE_UNAVAILABLE", 503},
		{"INVALID_COMMAND", 400},
	} {
		w := httptest.NewRecorder()
		failure(w, &room.Error{Code: tc.code, Message: "message"})
		if w.Code != tc.status {
			t.Fatalf("%s -> %d, want %d", tc.code, w.Code, tc.status)
		}
		if !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("%s missing from body %s", tc.code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	failure(w, errors.New("plain error"))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "plain error") {
		t.Fatalf("plain error mapping: %d %s", w.Code, w.Body)
	}
}

func TestDecodeRejectsMalformedBodies(t *testing.T) {
	for name, tc := range map[string]struct {
		contentType, body string
	}{
		"wrong media type":   {"text/plain", `{"a":1}`},
		"empty content type": {"", `{"a":1}`},
		"unknown field":      {"application/json", `{"a":1,"extra":2}`},
		"trailing object":    {"application/json", `{"a":1}{"b":2}`},
		"invalid json":       {"application/json", `{`},
		"oversized body":     {"application/json", `{"a":"` + strings.Repeat("x", 9000) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://example.test/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			var in struct {
				A int `json:"a"`
			}
			if err := decode(httptest.NewRecorder(), r, &in); err == nil {
				t.Fatalf("accepted %s", tc.body)
			}
		})
	}
	var in struct {
		A int `json:"a"`
	}
	r := httptest.NewRequest("POST", "https://example.test/", strings.NewReader(`{"a":7}`))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	if err := decode(httptest.NewRecorder(), r, &in); err != nil || in.A != 7 {
		t.Fatalf("valid body rejected: %v %+v", err, in)
	}
	// The writer also bounds the response when the body is refused.
	big := httptest.NewRequest("POST", "https://example.test/", strings.NewReader(`{"a":"`+strings.Repeat("x", 9000)+`"}`))
	big.Header.Set("Content-Type", "application/json")
	if err := decode(httptest.NewRecorder(), big, &in); err == nil {
		t.Fatal("oversized body accepted")
	}
}
