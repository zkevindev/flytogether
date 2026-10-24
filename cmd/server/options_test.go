package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworkOptionsAllowBotAndStillValidateSeats(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"bot", `{"names":["房主","朋友","灵矽机器人"],"network":true,"ai":true,"waiting":false,"difficulty":"hard"}`, true},
		{"one human and bot", `{"names":["房主","机器人"],"network":true,"ai":true}`, true},
		{"too many", `{"names":["一","二","三","四","五"],"network":true,"ai":true}`, false},
		{"too few", `{"names":["机器人"],"network":true,"ai":true}`, false},
		{"duplicate", `{"names":["机器人","机器人"],"network":true,"ai":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			o, e := options(httptest.NewRecorder(), req)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%t error=%v", tc.valid, e)
			}
			if tc.valid && (!o.Waiting || !o.AI || !o.Network) {
				t.Fatal("network must wait and preserve bot")
			}
		})
	}
}
