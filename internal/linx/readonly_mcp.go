package linx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

const gameMCPLabel = "flytogether_game"

type mcpSession struct {
	game    room.GameSession
	expires time.Time
	count   int
}

type mcpStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *mcpStatusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *mcpStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *mcpStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ReadonlyMCP exposes a small, read-only view of one live voice session. Its
// random bearer is short-lived, room-bound and deleted when Realtime disconnects.
type ReadonlyMCP struct {
	URL      string
	mu       sync.Mutex
	sessions map[[32]byte]*mcpSession
}

func NewReadonlyMCP(publicURL string) *ReadonlyMCP {
	return &ReadonlyMCP{URL: strings.TrimRight(publicURL, "/") + "/api/mcp/game", sessions: map[[32]byte]*mcpSession{}}
}

func (m *ReadonlyMCP) Enabled() bool { return m != nil && strings.HasPrefix(m.URL, "https://") }

func (m *ReadonlyMCP) Register(g room.GameSession, ttl time.Duration) (string, func()) {
	if !m.Enabled() || g == nil {
		return "", func() {}
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", func() {}
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	m.mu.Lock()
	now := time.Now()
	for key, s := range m.sessions {
		if now.After(s.expires) {
			delete(m.sessions, key)
		}
	}
	m.sessions[hash] = &mcpSession{game: g, expires: now.Add(ttl)}
	m.mu.Unlock()
	return token, func() { m.mu.Lock(); delete(m.sessions, hash); m.mu.Unlock() }
}

func (m *ReadonlyMCP) ToolConfig(token string) map[string]any {
	if !m.Enabled() || token == "" {
		return nil
	}
	return map[string]any{"type": "mcp", "server_label": gameMCPLabel, "server_url": m.URL, "authorization": token, "allowed_tools": []string{"get_game_summary", "get_mission_progress", "get_game_rules", "get_legal_actions"}, "require_approval": "never", "server_description": "Read-only facts for the current Fly Together cooperative rescue game. Never performs game actions."}
}

func mcpAuthorizationToken(header string) string {
	token := strings.TrimSpace(header)
	for len(token) >= len("Bearer ") && strings.EqualFold(token[:len("Bearer ")], "Bearer ") {
		token = strings.TrimSpace(token[len("Bearer "):])
	}
	return token
}

func (m *ReadonlyMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !m.Enabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := mcpAuthorizationToken(r.Header.Get("Authorization"))
	hash := sha256.Sum256([]byte(token))
	m.mu.Lock()
	session := m.sessions[hash]
	if session != nil && time.Now().After(session.expires) {
		delete(m.sessions, hash)
		session = nil
	}
	if session != nil {
		session.count++
		if session.count > 120 {
			delete(m.sessions, hash)
			session = nil
		}
	}
	m.mu.Unlock()
	log.Printf("game MCP request: method=%s authorized=%t", r.Method, session != nil)
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	server := newGameMCPServer(session.game)
	// The app binds to loopback behind Caddy, so the MCP SDK sees a loopback
	// local address and the original public Host forwarded by the trusted proxy.
	// This endpoint has its own short-lived, room-bound bearer authorization.
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	tracked := &mcpStatusWriter{ResponseWriter: w}
	handler.ServeHTTP(tracked, r)
	if tracked.status == 0 {
		tracked.status = http.StatusOK
	}
	log.Printf("game MCP response: method=%s status=%d content_type=%s", r.Method, tracked.status, w.Header().Get("Content-Type"))
}

type noArguments struct{}

func newGameMCPServer(g room.GameSession) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "flytogether-game", Title: "一起飞棋局助手", Version: "1"}, &mcp.ServerOptions{Instructions: "这些工具只读当前语音会话绑定的棋局。请准确简短地说明当前棋局、任务进度、规则或 Go 服务提供的合法走法摘要。不得宣称执行掷骰、移动或使用道具；所有操作必须由现有棋盘授权流程执行。"})
	add := func(name, description string, selectText func(game.State, room.View) any) {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, req *mcp.CallToolRequest, _ noArguments) (*mcp.CallToolResult, any, error) {
			view := g.Snapshot()
			var state game.State
			if err := json.Unmarshal(view.State, &state); err != nil {
				return nil, nil, errors.New("当前棋局暂不可读取")
			}
			result, err := json.Marshal(selectText(state, view))
			if err != nil {
				return nil, nil, errors.New("棋局信息暂不可用")
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(result)}}}, nil, nil
		})
	}
	add("get_game_summary", "Read the current room status, mode, board, turn, players, pieces and winner.", func(s game.State, view room.View) any {
		players := make([]map[string]any, 0, len(s.Players))
		for _, p := range s.Players {
			arrived, airborne, hangar := 0, 0, 0
			for _, piece := range p.Pieces {
				switch {
				case piece.Progress == s.Board.Finish:
					arrived++
				case piece.Progress < 0:
					hangar++
				default:
					airborne++
				}
			}
			players = append(players, map[string]any{"id": p.ID, "name": p.Name, "arrived": arrived, "airborne": airborne, "hangar": hangar, "rolls": p.Stats.Rolls, "takeoffs": p.Stats.Takeoffs})
		}
		current := "未知"
		if s.Current >= 0 && s.Current < len(s.Players) {
			current = s.Players[s.Current].Name
		}
		return map[string]any{"room_status": view.Status, "game_id": s.ID, "version": s.Version, "mode": s.Mode, "theme_id": s.ThemeID, "board": s.Board.Name, "phase": s.Phase, "turn": s.Turn, "current_player": current, "dice": s.Dice, "winner": s.Winner, "players": players}
	})
	add("get_mission_progress", "Read the selected cooperative mission and each player's committed progress. No progress is changed by reading this tool.", func(s game.State, _ room.View) any {
		if s.Mission == nil {
			return map[string]any{"selected": false, "message": "当前对局没有额外任务卡。"}
		}
		people := make([]map[string]any, 0, len(s.Players))
		for _, p := range s.Players {
			qualified := false
			switch s.Mission.ID {
			case "launch-formation":
				qualified = p.Stats.Takeoffs > 0
			case "signal-network":
				qualified = p.Stats.Rolls >= 3
			case "supply-team":
				qualified = p.Stats.PropsUsed > 0
			}
			people = append(people, map[string]any{"player": p.Name, "contributed": qualified})
		}
		return map[string]any{"id": s.Mission.ID, "title": s.Mission.Title, "description": s.Mission.Description, "progress": s.Mission.Progress, "target": s.Mission.Target, "status": s.Mission.Status, "players": people}
	})
	add("get_game_rules", "Read the rules that were fixed when this match started.", func(s game.State, _ room.View) any { return s.RuleInfo })
	add("get_legal_actions", "Read the current legal move summaries supplied by the game server. Does not submit a move.", func(s game.State, _ room.View) any {
		moves := make([]map[string]any, 0, len(s.Legal))
		for _, move := range s.Legal {
			moves = append(moves, map[string]any{"piece_id": move.PieceID, "label": move.Label, "summary": move.Summary, "from": move.From, "to": move.To})
		}
		return map[string]any{"phase": s.Phase, "current_player": s.Players[s.Current].Name, "dice": s.Dice, "legal_moves": moves}
	})
	return server
}
