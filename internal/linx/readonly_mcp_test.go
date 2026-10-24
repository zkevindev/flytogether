package linx

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copy)
}

func TestReadonlyMCPIsSessionBoundAndOnlyProvidesReadTools(t *testing.T) {
	gateway := NewReadonlyMCP("https://flytogether.example")
	r := room.New(game.Options{Mode: "coop", Names: []string{"孩子", "家长"}, MissionID: "launch-formation"})
	session, err := r.VoiceFor("owner")
	if err != nil {
		t.Fatal(err)
	}
	token, revoke := gateway.Register(session, time.Minute)
	defer revoke()
	server := httptest.NewServer(gateway)
	defer server.Close()
	unauthorized, err := http.Post(server.URL+"/api/mcp/game", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized request status = %d", unauthorized.StatusCode)
	}

	proxied := httptest.NewRequest(http.MethodPost, "https://flytogether.example/api/mcp/game", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"proxy-test","version":"1"}}}`))
	proxied.Header.Set("Content-Type", "application/json")
	proxied.Header.Set("Authorization", "Bearer "+token)
	proxied = proxied.WithContext(context.WithValue(proxied.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}))
	proxiedResponse := httptest.NewRecorder()
	gateway.ServeHTTP(proxiedResponse, proxied)
	if proxiedResponse.Code == http.StatusForbidden {
		t.Fatalf("trusted reverse-proxy Host was rejected: %s", proxiedResponse.Body.String())
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: server.URL + "/api/mcp/game", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{token, http.DefaultTransport}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	var names []string
	for tool, err := range connected.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "get_game_rules,get_game_summary,get_legal_actions,get_mission_progress" {
		t.Fatalf("unexpected read-only tools: %v", names)
	}
	result, err := connected.CallTool(ctx, &mcp.CallToolParams{Name: "get_mission_progress", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var mission map[string]any
	if err = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &mission); err != nil {
		t.Fatal(err)
	}
	if mission["status"] != "active" || mission["progress"] != float64(0) || mission["target"] != float64(2) {
		t.Fatalf("unexpected mission result: %#v", mission)
	}
	if after := r.Snapshot().State; len(after) == 0 {
		t.Fatal("read-only call removed game state")
	}
	revoke()
	response, err := http.Post(server.URL+"/api/mcp/game", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked bearer status = %d", response.StatusCode)
	}
}

func TestRealtimeMCPConfigIsScopedAndReadOnly(t *testing.T) {
	mcpTool := NewReadonlyMCP("https://flytogether.example").ToolConfig("short-lived-token")
	if mcpTool["server_url"] != "https://flytogether.example/api/mcp/game" || mcpTool["authorization"] != "short-lived-token" || mcpTool["require_approval"] != "never" {
		t.Fatalf("MCP config: %#v", mcpTool)
	}
	allowed := mcpTool["allowed_tools"].([]string)
	if len(allowed) != 4 {
		t.Fatalf("allowed MCP surface = %v", allowed)
	}
	if NewReadonlyMCP("http://localhost:8080").Enabled() {
		t.Fatal("non-TLS MCP endpoint enabled")
	}
}

func TestMCPAuthorizationTokenAcceptsProviderBearerNormalization(t *testing.T) {
	for _, header := range []string{"short-lived-token", "Bearer short-lived-token", "Bearer Bearer short-lived-token", "bearer BEARER short-lived-token"} {
		if token := mcpAuthorizationToken(header); token != "short-lived-token" {
			t.Errorf("mcpAuthorizationToken(%q) = %q", header, token)
		}
	}
}
