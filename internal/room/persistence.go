package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
	_ "modernc.org/sqlite"
)

const saveSchema = 1

// Version 6 persists delegation consent/control. Older binaries must not ignore seat control state.
const databaseSchema = 6
const DefaultRetention = 7 * 24 * time.Hour

type savedMember struct {
	Delegated         bool   `json:"delegated,omitempty"`
	AutoDelegate      bool   `json:"auto_delegate,omitempty"`
	DelegationReason  string `json:"delegation_reason,omitempty"`
	DelegationVersion int    `json:"delegation_version,omitempty"`
	Index             int    `json:"index"`
	Ready             bool   `json:"ready"`
}
type roomRecord struct {
	HostChange    *HostChange            `json:"host_change,omitempty"`
	Schema        int                    `json:"schema"`
	ID            string                 `json:"id"`
	Owner         string                 `json:"owner"`
	Invite        string                 `json:"invite,omitempty"`
	Members       map[string]savedMember `json:"members,omitempty"`
	Names         map[string]string      `json:"names"`
	Options       game.Options           `json:"options"`
	Name          string                 `json:"name"`
	Status        string                 `json:"status"`
	State         *game.State            `json:"state"`
	Seq           int                    `json:"seq"`
	Updated       time.Time              `json:"updated"`
	SavedAt       time.Time              `json:"saved_at"`
	VoiceSettings VoiceSettings          `json:"voice_settings"`
}
type savedCommand struct {
	Input Command `json:"input"`
	Actor string  `json:"actor"`
}
type saveChange struct {
	Session       string
	Forget        bool
	DeleteRoom    string
	ResetCommands bool
}
type database struct {
	db                 *sql.DB
	diagnostics        atomic.Pointer[DiagnosticConfig]
	diagnosticFailures atomic.Uint64
}

func (r *Room) record() roomRecord {
	members := map[string]savedMember{}
	for key, m := range r.members {
		members[key] = savedMember{Index: m.index, Ready: m.ready, Delegated: m.delegated, AutoDelegate: m.autoDelegate, DelegationReason: m.delegationReason, DelegationVersion: m.delegationVersion}
	}
	return roomRecord{HostChange: r.hostChange, Schema: saveSchema, ID: r.ID, Owner: r.owner, Invite: r.invite, Members: members, Names: r.names, Options: r.options, Name: r.name, Status: r.status, State: r.state, Seq: r.seq, Updated: r.updated, SavedAt: r.savedAt, VoiceSettings: r.voiceSettings}
}
func (r *Room) readRecord(p roomRecord) {
	r.hostChange = p.HostChange
	r.ID, r.owner, r.invite, r.names, r.options = p.ID, p.Owner, p.Invite, p.Names, p.Options
	r.name, r.status, r.state, r.seq, r.updated, r.savedAt = p.Name, p.Status, p.State, p.Seq, p.Updated, p.SavedAt
	r.voiceSettings = p.VoiceSettings
	r.members = map[string]member{}
	for key, m := range p.Members {
		r.members[key] = member{index: m.Index, ready: m.Ready, delegated: m.Delegated, autoDelegate: m.AutoDelegate, delegationReason: m.DelegationReason, delegationVersion: m.DelegationVersion}
		if m.Delegated && m.DelegationVersion == 0 {
			member := r.members[key]
			member.delegationVersion = p.State.Version
			r.members[key] = member
		}
	}
}

// Runtime channels, leases and proposals are deliberately absent from durable records.
type rollbackPoint struct {
	data                                                   []byte
	cache                                                  map[string]cached
	coach                                                  *Coach
	previous                                               string
	expired                                                bool
	voice, grant, grantGame, grantActor, voiceActor, lease string
	grantVersion                                           int
	busy, used, restored                                   bool
	hostOfflineSince                                       time.Time
	hostClaimable                                          bool
	offlineSince                                           map[string]time.Time
}

func (r *Room) beforeMutation() *rollbackPoint {
	if r.storage == nil {
		return nil
	}
	data, _ := json.Marshal(r.record())
	offline := map[string]time.Time{}
	for key, m := range r.members {
		offline[key] = m.offlineSince
	}
	return &rollbackPoint{data, r.cache, r.coach, r.coachPrevious, r.coachExpired, r.voice, r.grant, r.grantGame, r.grantActor, r.voiceActor, r.voiceLease, r.grantVersion, r.busy, r.used, r.restored, r.hostOfflineSince, r.hostClaimable, offline}
}
func (r *Room) rollback(p *rollbackPoint, err *error) {
	if p == nil || *err == nil {
		return
	}
	r.restore(p)
}
func (r *Room) restore(p *rollbackPoint) {
	if p == nil {
		return
	}
	var rec roomRecord
	_ = json.Unmarshal(p.data, &rec)
	r.readRecord(rec)
	r.cache, r.coach, r.coachPrevious, r.coachExpired = p.cache, p.coach, p.previous, p.expired
	r.voice, r.grant, r.grantGame, r.grantActor, r.voiceActor, r.voiceLease = p.voice, p.grant, p.grantGame, p.grantActor, p.voiceActor, p.lease
	r.grantVersion, r.busy, r.used, r.restored = p.grantVersion, p.busy, p.used, p.restored
	r.hostOfflineSince, r.hostClaimable = p.hostOfflineSince, p.hostClaimable
	for key, m := range r.members {
		m.offlineSince = p.offlineSince[key]
		r.members[key] = m
	}
}

// finish commits both game and idempotency key before any client can observe success.
// Called with the room lock held. The database never acquires a store/room lock.
func (r *Room) finish(c *Command, change saveChange) (View, error) {
	r.seq++
	r.updated = time.Now().UTC()
	if r.storage != nil {
		r.savedAt = r.updated
		if err := r.storage.save(r.record(), c, change); err != nil {
			r.saveError = true
			slog.Error("game save failed", "operation", "commit")
			return View{}, fail("SAVE_UNAVAILABLE", "暂时无法保存，本次操作未生效，请稍后重试")
		}
	}
	r.saveError = false
	actor := ""
	if c != nil {
		actor = c.Actor
		r.cache[c.ID] = cached{Input: *c}
	}
	r.broadcast()
	return r.personalized(actor), nil
}

func OpenStore(path string, retention time.Duration) (*Store, error) {
	if path == "" {
		return nil, errors.New("存档路径不能为空")
	}
	if retention <= 0 {
		retention = DefaultRetention
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	// Create with restricted permissions before SQLite can create journal sidecars.
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	file.Close()
	if err = os.Chmod(abs, 0600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	failOpen := func(e error) (*Store, error) { db.Close(); return nil, e }
	for _, pragma := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON"} {
		if _, err = db.Exec(pragma); err != nil {
			return failOpen(err)
		}
	}
	var check string
	if err = db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return failOpen(errors.New("存档数据库完整性检查失败，请保留文件并从备份恢复"))
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return failOpen(err)
	}
	if version > databaseSchema {
		return failOpen(errors.New("存档由更新版本创建，已保留，请使用对应版本启动"))
	}
	tx, err := db.Begin()
	if err != nil {
		return failOpen(err)
	}
	for _, query := range []string{
		"CREATE TABLE IF NOT EXISTS rooms (id TEXT PRIMARY KEY, payload BLOB NOT NULL, checksum TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE)",
		"CREATE TABLE IF NOT EXISTS commands (room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE, id TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY(room_id,id))",
		"CREATE TABLE IF NOT EXISTS quarantine (id INTEGER PRIMARY KEY, room_id TEXT NOT NULL, payload BLOB NOT NULL, reason TEXT NOT NULL, created_at TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS voice_usage (day TEXT NOT NULL, kind TEXT NOT NULL, used INTEGER NOT NULL CHECK(used > 0), PRIMARY KEY(day,kind))",
		"CREATE TABLE IF NOT EXISTS family_profiles (session TEXT NOT NULL, id TEXT NOT NULL, name TEXT NOT NULL, avatar TEXT NOT NULL, PRIMARY KEY(session,id))",
		"CREATE TABLE IF NOT EXISTS family_history (session TEXT NOT NULL, game_id TEXT NOT NULL, completed_at TEXT NOT NULL, payload BLOB, PRIMARY KEY(session,game_id))",
		`CREATE TABLE IF NOT EXISTS diagnostic_matches (game_id TEXT PRIMARY KEY,room_id TEXT NOT NULL,name TEXT NOT NULL,status TEXT NOT NULL,version INTEGER NOT NULL,seq INTEGER NOT NULL,network INTEGER NOT NULL,recorded_from TEXT NOT NULL,updated_at TEXT NOT NULL,first_version INTEGER NOT NULL,snapshot BLOB NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS diagnostic_matches_updated ON diagnostic_matches(updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_events (id INTEGER PRIMARY KEY AUTOINCREMENT,event_key TEXT NOT NULL UNIQUE,at TEXT NOT NULL,room_id TEXT NOT NULL,game_id TEXT NOT NULL,version INTEGER NOT NULL,kind TEXT NOT NULL,source TEXT NOT NULL,session_id TEXT NOT NULL,input_id TEXT NOT NULL,ref TEXT NOT NULL,text TEXT NOT NULL,action TEXT NOT NULL,code TEXT NOT NULL,duration_ms INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS diagnostic_events_game ON diagnostic_events(game_id,id DESC)`,
		`CREATE INDEX IF NOT EXISTS diagnostic_events_at ON diagnostic_events(at)`,
		"PRAGMA user_version=6",
	} {
		if _, err = tx.Exec(query); err != nil {
			tx.Rollback()
			return failOpen(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return failOpen(err)
	}
	s := NewStore()
	s.storage = &database{db: db}
	s.retention = retention
	if err = s.load(); err != nil {
		return failOpen(err)
	}
	return s, nil
}
func (s *Store) Close() error {
	if s.storage == nil {
		return nil
	}
	return s.storage.db.Close()
}
func (d *database) save(p roomRecord, c *Command, change saveChange) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if change.DeleteRoom != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM rooms WHERE id=?", change.DeleteRoom); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO rooms(id,payload,checksum) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,checksum=excluded.checksum", p.ID, data, hex.EncodeToString(sum[:])); err != nil {
		return err
	}
	if change.ResetCommands {
		if _, err = tx.ExecContext(ctx, "DELETE FROM commands WHERE room_id=?", p.ID); err != nil {
			return err
		}
	}
	if c != nil && c.Token == "" {
		commandData, _ := json.Marshal(savedCommand{*c, c.Actor})
		if _, err = tx.ExecContext(ctx, "INSERT INTO commands(room_id,id,payload) VALUES(?,?,?) ON CONFLICT(room_id,id) DO UPDATE SET payload=excluded.payload", p.ID, c.ID, commandData); err != nil {
			return err
		}
	}
	if change.Session != "" {
		if change.Forget {
			_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE id=? AND room_id=?", change.Session, p.ID)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO sessions(id,room_id) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET room_id=excluded.room_id", change.Session, p.ID)
		}
		if err != nil {
			return err
		}
	}
	if err = archiveResult(ctx, tx, p); err != nil {
		return err
	}
	if err = d.saveDiagnostics(ctx, tx, p, c, false); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *database) remove(id string) error {
	_, err := d.db.Exec("DELETE FROM rooms WHERE id=?", id)
	return err
}
func (s *Store) load() error {
	rows, err := s.storage.db.Query("SELECT id,payload,checksum FROM rooms")
	if err != nil {
		return err
	}
	type raw struct {
		id       string
		data     []byte
		checksum string
	}
	var records []raw
	for rows.Next() {
		var p raw
		if err = rows.Scan(&p.id, &p.data, &p.checksum); err != nil {
			rows.Close()
			return err
		}
		records = append(records, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, raw := range records {
		sum := sha256.Sum256(raw.data)
		var p roomRecord
		decodeErr := json.Unmarshal(raw.data, &p)
		if decodeErr != nil || raw.checksum != hex.EncodeToString(sum[:]) || p.ID != raw.id || !validRecord(p) {
			if err = s.quarantine(raw.id, raw.data); err != nil {
				return err
			}
			continue
		}
		if time.Since(p.Updated) > s.retention {
			if err = s.storage.remove(p.ID); err != nil {
				return err
			}
			continue
		}
		r := &Room{instance: game.ID(), storage: s.storage, cache: map[string]cached{}, listeners: map[chan []byte]string{}, voice: "disconnected", voiceSettings: DefaultVoiceSettings()}
		r.readRecord(p)
		if r.status == "playing" && r.state.Phase != "finished" {
			r.status = "paused"
		}
		r.restored = r.status == "paused" && r.state.Phase != "finished"
		r.voiceSettings.WakeEnabled = false
		if r.voiceSettings.WakeWord == "" {
			r.voiceSettings = DefaultVoiceSettings()
		}
		r.coachExpired = true
		if r.status == "waiting" {
			for key, m := range r.members {
				m.ready = key == r.owner
				r.members[key] = m
			}
		}
		commands, e := s.storage.db.Query("SELECT id,payload FROM commands WHERE room_id=?", r.ID)
		if e != nil {
			return e
		}
		for commands.Next() {
			var id string
			var data []byte
			if e = commands.Scan(&id, &data); e != nil {
				commands.Close()
				return e
			}
			var c savedCommand
			if json.Unmarshal(data, &c) != nil || c.Input.ID != id || c.Input.Token != "" {
				r.cache[id] = cached{Unavailable: true}
				continue
			}
			c.Input.Actor = c.Actor
			r.cache[id] = cached{Input: c.Input}
		}
		e = commands.Err()
		commands.Close()
		if e != nil {
			return e
		}
		// Store the safe paused lifecycle immediately, so a second restart stays paused.
		if e = s.storage.save(r.record(), nil, saveChange{}); e != nil {
			return e
		}
		s.rooms[r.ID] = r
	}
	sessions, err := s.storage.db.Query("SELECT id,room_id FROM sessions")
	if err != nil {
		return err
	}
	defer sessions.Close()
	for sessions.Next() {
		var key, id string
		if err = sessions.Scan(&key, &id); err != nil {
			return err
		}
		if r := s.rooms[id]; r != nil && r.isMember(key) {
			s.sessions[key] = id
		}
	}
	return sessions.Err()
}
func (s *Store) quarantine(id string, data []byte) error {
	tx, err := s.storage.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO quarantine(room_id,payload,reason,created_at) VALUES(?,?,?,?)", id, data, "invalid or unsupported room record", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM rooms WHERE id=?", id); err != nil {
		return err
	}
	slog.Warn("invalid saved room quarantined")
	return tx.Commit()
}
func validRecord(p roomRecord) bool {
	if p.Schema != saveSchema || p.ID == "" || p.Owner == "" || p.State == nil || len(p.Options.Names) < 2 || len(p.Options.Names) > 4 {
		return false
	}
	switch p.Status {
	case "waiting", "playing", "paused", "ended":
	default:
		return false
	}
	if err := p.State.Validate(); err != nil {
		return false
	}
	if len(p.Options.Names) != len(p.State.Players) || p.Seq < 0 || p.Updated.IsZero() || p.SavedAt.IsZero() {
		return false
	}
	if p.Invite != "" {
		owner, ok := p.Members[p.Owner]
		if !ok || owner.Index < 0 || owner.Index >= len(p.State.Players) || p.State.Players[owner.Index].AI {
			return false
		}
		used := map[int]bool{}
		for key, m := range p.Members {
			if key == "" || m.Index < 0 || m.Index >= len(p.State.Players) || p.State.Players[m.Index].AI || used[m.Index] || p.Names[key] == "" {
				return false
			}
			if (m.Delegated && m.DelegationReason != "manual" && m.DelegationReason != "offline") || (!m.Delegated && (m.DelegationReason != "" || m.DelegationVersion != 0)) || (m.Delegated && (m.DelegationVersion < 0 || m.DelegationVersion > p.State.Version)) {
				return false
			}
			used[m.Index] = true
		}
	}
	return true
}
func (s *Store) Discard(session, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[id]
	if r == nil {
		return fail("NOT_AUTHORIZED", "房间不存在或已删除")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.isMember(session) || r.owner != session {
		return fail("HOST_ONLY", "只有房主可以删除这份存档")
	}
	if s.storage != nil {
		if err := s.storage.remove(id); err != nil {
			return fail("SAVE_UNAVAILABLE", "删除没有成功，存档仍保留，请稍后重试")
		}
	}
	if r.status != "ended" {
		_ = r.control("end")
	}
	r.savedAt = time.Time{}
	r.publish()
	delete(s.rooms, id)
	for key, current := range s.sessions {
		if current == id {
			delete(s.sessions, key)
		}
	}
	return nil
}
