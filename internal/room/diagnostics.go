package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

// Diagnostics contains only explicit allowlisted fields. Never pass raw HTTP or upstream payloads.
type DiagnosticConfig struct {
	Enabled     bool
	Transcripts bool
	Retention   time.Duration
}
type DiagnosticEvent struct {
	ID         int64  `json:"id"`
	At         string `json:"at"`
	RoomID     string `json:"room_id"`
	GameID     string `json:"game_id"`
	Version    int    `json:"version"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	SessionID  string `json:"session_id,omitempty"`
	InputID    string `json:"input_id,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Text       string `json:"text,omitempty"`
	Action     string `json:"action,omitempty"`
	Code       string `json:"code,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}
type DiagnosticMatch struct {
	GameID       string      `json:"game_id"`
	RoomID       string      `json:"room_id"`
	Name         string      `json:"name"`
	Status       string      `json:"status"`
	Version      int         `json:"version"`
	Seq          int         `json:"seq"`
	Network      bool        `json:"network"`
	RecordedFrom string      `json:"recorded_from"`
	UpdatedAt    string      `json:"updated_at"`
	FirstVersion int         `json:"first_version"`
	State        *game.State `json:"state,omitempty"`
}
type DiagnosticQuery struct {
	Search, Status, Kind string
	Before               int64
	Limit, Offset        int
}
type DiagnosticPage struct {
	Matches []DiagnosticMatch `json:"matches"`
	Total   int               `json:"total"`
}
type DiagnosticEvents struct {
	Events     []DiagnosticEvent `json:"events"`
	NextBefore int64             `json:"next_before"`
}

var credentialText = regexp.MustCompile(`(?i)(bearer\s+[^\s，,。;；]+|sk-[a-z0-9_-]{8,}|(?:api[_ -]?key|password|authorization|cookie|token|密码|密钥)\s*[=:：]\s*[^\s，,。;；]+)`)

var diagnosticIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,160}$`)

func diagnosticRef(value string) string {
	if value == "" || diagnosticIdentifier.MatchString(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "ref-" + hex.EncodeToString(sum[:8])
}
func DiagnosticText(text string) string {
	text = credentialText.ReplaceAllString(text, "[已隐藏凭据]")
	r := []rune(strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text))
	if len(r) > 2000 {
		r = append(r[:2000], []rune("…[已截断]")...)
	}
	return string(r)
}
func (d *database) diagnosticConfig() DiagnosticConfig {
	if p := d.diagnostics.Load(); p != nil {
		return *p
	}
	return DiagnosticConfig{}
}
func (s *Store) ConfigureDiagnostics(c DiagnosticConfig) error {
	if s.storage == nil {
		return errors.New("diagnostics requires persistent storage")
	}
	if c.Retention < time.Hour {
		c.Retention = 7 * 24 * time.Hour
	}
	s.storage.diagnostics.Store(&c)
	if err := s.PruneDiagnostics(context.Background()); err != nil {
		return err
	}
	if !c.Enabled {
		return nil
	}
	// Backfill only a clearly marked starting snapshot, not invented historical events.
	s.mu.Lock()
	all := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		all = append(all, r)
	}
	s.mu.Unlock()
	for _, r := range all {
		r.mu.Lock()
		p := r.record()
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		tx, e := s.storage.db.BeginTx(ctx, nil)
		if e == nil {
			e = s.storage.saveDiagnostics(ctx, tx, p, nil, true)
			if e == nil {
				e = tx.Commit()
			} else {
				tx.Rollback()
			}
		}
		cancel()
		if e != nil {
			return e
		}
	}
	return nil
}
func (d *database) saveDiagnostics(ctx context.Context, tx *sql.Tx, p roomRecord, c *Command, baseline bool) error {
	if !d.diagnosticConfig().Enabled || p.State == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stateData, _ := json.Marshal(p.State)
	var state game.State
	_ = json.Unmarshal(stateData, &state)
	for i := range state.Players {
		state.Players[i].Name = DiagnosticText(state.Players[i].Name)
	}
	for i := range state.History {
		state.History[i].Text = DiagnosticText(state.History[i].Text)
	}
	snapshot, _ := json.Marshal(state)
	status := p.Status
	if p.State.Phase == "finished" {
		status = "finished"
	}
	// Compare with the last captured snapshot: an old save's bounded feed is not a new event.
	previous := map[string]bool{}
	var old []byte
	readErr := tx.QueryRowContext(ctx, "SELECT snapshot FROM diagnostic_matches WHERE game_id=?", p.State.ID).Scan(&old)
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return readErr
	}
	if len(old) > 0 {
		var last game.State
		if err := json.Unmarshal(old, &last); err != nil {
			return err
		}
		for _, e := range last.History {
			previous[e.ID] = true
		}
	}
	// INSERT preserves recorded_from/first_version, including across replay and restart.
	_, err := tx.ExecContext(ctx, `INSERT INTO diagnostic_matches(game_id,room_id,name,status,version,seq,network,recorded_from,updated_at,first_version,snapshot) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(game_id) DO UPDATE SET name=excluded.name,status=excluded.status,version=excluded.version,seq=excluded.seq,updated_at=excluded.updated_at,snapshot=excluded.snapshot`, p.State.ID, p.ID, DiagnosticText(p.Name), status, p.State.Version, p.Seq, p.Invite != "", now, now, p.State.Version, snapshot)
	if err != nil {
		return err
	}
	source, action := "system", "room_update"
	if c != nil {
		source = "click"
		if c.Token != "" {
			source = "voice"
		}
		action = c.Action
	}
	if baseline {
		action = "recording_started"
	}
	e := DiagnosticEvent{At: now, RoomID: p.ID, GameID: p.State.ID, Version: p.State.Version, Kind: "command", Source: source, Action: action}
	if (action == "transfer_host" || action == "claim_host") && p.HostChange != nil {
		e.Text = DiagnosticText(p.HostChange.From + "已将房主管理权交给" + p.HostChange.To)
	}
	if err = insertDiagnostic(ctx, tx, e, "state:"+p.State.ID+":"+strings.TrimSpace(timeKey(p.Seq))+":"+action); err != nil {
		return err
	}
	if baseline {
		return nil
	}
	for _, event := range p.State.History {
		if previous[event.ID] {
			continue
		}
		e.Kind, e.Text, e.Ref = "game", DiagnosticText(event.Text), event.ID
		e.Action = event.Kind
		if err = insertDiagnostic(ctx, tx, e, "event:"+p.State.ID+":"+event.ID); err != nil {
			return err
		}
	}
	return nil
}
func timeKey(i int) string { b, _ := json.Marshal(i); return string(b) }

type diagnosticExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertDiagnostic(ctx context.Context, db diagnosticExecer, e DiagnosticEvent, key string) error {
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO diagnostic_events(event_key,at,room_id,game_id,version,kind,source,session_id,input_id,ref,text,action,code,duration_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, key, e.At, e.RoomID, e.GameID, e.Version, e.Kind, e.Source, e.SessionID, e.InputID, e.Ref, e.Text, e.Action, e.Code, e.DurationMS)
	return err
}

// Diagnostic is best-effort for transport events; game mutations are recorded atomically in save().
func (s *Store) Diagnostic(e DiagnosticEvent) {
	if s.storage == nil {
		return
	}
	c := s.storage.diagnosticConfig()
	if !c.Enabled {
		return
	}
	if !c.Transcripts && (e.Kind == "user_text" || e.Kind == "assistant_text" || e.Kind == "tts") {
		e.Text = ""
	}
	e.Text = DiagnosticText(e.Text)
	e.Ref = diagnosticRef(e.Ref)
	e.InputID = diagnosticRef(e.InputID)
	e.Action = diagnosticRef(e.Action)
	e.Code = diagnosticRef(e.Code)
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// No FK to rooms: ended/replaced/discarded games remain diagnosable until retention expiry.
	key := "transport:" + game.ID()
	if e.Source == "browser" {
		key = "browser:" + e.GameID + ":" + e.Code + ":" + timeKey(int(time.Now().Unix()/10))
	}
	if err := insertDiagnostic(ctx, s.storage.db, e, key); err != nil {
		s.storage.diagnosticFailures.Add(1)
		slog.Warn("diagnostic event unavailable", "kind", e.Kind)
	}
}
func (s *Store) DiagnosticStatus() map[string]any {
	if s.storage == nil {
		return map[string]any{"enabled": false}
	}
	c := s.storage.diagnosticConfig()
	return map[string]any{"enabled": c.Enabled, "transcripts": c.Transcripts, "retention_hours": int(c.Retention.Hours()), "write_failures": s.storage.diagnosticFailures.Load()}
}
func (s *Store) PruneDiagnostics(ctx context.Context) error {
	if s.storage == nil {
		return nil
	}
	c := s.storage.diagnosticConfig()
	if c.Retention <= 0 {
		c.Retention = DefaultRetention
	}
	cutoff := time.Now().UTC().Add(-c.Retention).Format(time.RFC3339Nano)
	tx, err := s.storage.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Remove old text even if its match is still active. Old inactive matches follow.
	if _, err = tx.ExecContext(ctx, "DELETE FROM diagnostic_events WHERE at < ?", cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM diagnostic_matches WHERE updated_at < ?", cutoff); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DiagnosticMatches(ctx context.Context, q DiagnosticQuery) (DiagnosticPage, error) {
	out := DiagnosticPage{Matches: []DiagnosticMatch{}}
	if s.storage == nil {
		return out, errors.New("diagnostics unavailable")
	}
	if q.Limit < 1 || q.Limit > 50 {
		q.Limit = 25
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	cutoff := time.Now().UTC().Add(-s.storage.diagnosticConfig().Retention).Format(time.RFC3339Nano)
	where := " WHERE updated_at >= ?"
	args := []any{cutoff}
	if q.Search != "" {
		where += " AND (instr(room_id,?)>0 OR instr(game_id,?)>0 OR instr(name,?)>0)"
		args = append(args, q.Search, q.Search, q.Search)
	}
	if q.Status != "" {
		where += " AND status=?"
		args = append(args, q.Status)
	}
	if err := s.storage.db.QueryRowContext(ctx, "SELECT count(*) FROM diagnostic_matches"+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.storage.db.QueryContext(ctx, `SELECT game_id,room_id,name,status,version,seq,network,recorded_from,updated_at,first_version FROM diagnostic_matches`+where+" ORDER BY updated_at DESC,game_id DESC LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m DiagnosticMatch
		if err = rows.Scan(&m.GameID, &m.RoomID, &m.Name, &m.Status, &m.Version, &m.Seq, &m.Network, &m.RecordedFrom, &m.UpdatedAt, &m.FirstVersion); err != nil {
			return out, err
		}
		out.Matches = append(out.Matches, m)
	}
	return out, rows.Err()
}
func (s *Store) DiagnosticMatch(ctx context.Context, id string) (DiagnosticMatch, error) {
	var m DiagnosticMatch
	var raw []byte
	if s.storage == nil {
		return m, sql.ErrNoRows
	}
	cutoff := time.Now().UTC().Add(-s.storage.diagnosticConfig().Retention).Format(time.RFC3339Nano)
	err := s.storage.db.QueryRowContext(ctx, `SELECT game_id,room_id,name,status,version,seq,network,recorded_from,updated_at,first_version,snapshot FROM diagnostic_matches WHERE game_id=? AND updated_at >= ?`, id, cutoff).Scan(&m.GameID, &m.RoomID, &m.Name, &m.Status, &m.Version, &m.Seq, &m.Network, &m.RecordedFrom, &m.UpdatedAt, &m.FirstVersion, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &m.State)
	}
	return m, err
}
func (s *Store) DiagnosticTimeline(ctx context.Context, id string, q DiagnosticQuery) (DiagnosticEvents, error) {
	out := DiagnosticEvents{Events: []DiagnosticEvent{}}
	if _, err := s.DiagnosticMatch(ctx, id); err != nil {
		return out, err
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	cutoff := time.Now().UTC().Add(-s.storage.diagnosticConfig().Retention).Format(time.RFC3339Nano)
	where := " WHERE game_id=? AND at >= ?"
	args := []any{id, cutoff}
	if q.Before > 0 {
		where += " AND id < ?"
		args = append(args, q.Before)
	}
	if q.Kind != "" {
		if q.Kind == "voice" {
			where += " AND source IN ('realtime','tts','voice','spectator')"
		} else {
			where += " AND kind=?"
			args = append(args, q.Kind)
		}
	}
	rows, err := s.storage.db.QueryContext(ctx, `SELECT id,at,room_id,game_id,version,kind,source,session_id,input_id,ref,text,action,code,duration_ms FROM diagnostic_events`+where+" ORDER BY id DESC LIMIT ?", append(args, q.Limit+1)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e DiagnosticEvent
		if err = rows.Scan(&e.ID, &e.At, &e.RoomID, &e.GameID, &e.Version, &e.Kind, &e.Source, &e.SessionID, &e.InputID, &e.Ref, &e.Text, &e.Action, &e.Code, &e.DurationMS); err != nil {
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	if len(out.Events) > q.Limit {
		out.Events = out.Events[:q.Limit]
		out.NextBefore = out.Events[len(out.Events)-1].ID
	}
	return out, rows.Err()
}
