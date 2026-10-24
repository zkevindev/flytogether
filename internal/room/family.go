package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zkevindev/flytogether/internal/game"
)

type Profile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}
type ResultPlayer struct {
	ProfileID string          `json:"profile_id,omitempty"`
	Name      string          `json:"name"`
	Avatar    string          `json:"avatar,omitempty"`
	AI        bool            `json:"ai"`
	Won       bool            `json:"won"`
	Stats     game.MatchStats `json:"stats"`
}
type MatchResult struct {
	GameID      string         `json:"game_id"`
	CompletedAt time.Time      `json:"completed_at"`
	RoomName    string         `json:"room_name"`
	Board       string         `json:"board"`
	Mode        string         `json:"mode"`
	Preset      string         `json:"preset"`
	Difficulty  string         `json:"difficulty"`
	Network     bool           `json:"network"`
	Props       bool           `json:"props"`
	Players     []ResultPlayer `json:"players"`
}
type Family struct {
	Profiles []Profile     `json:"profiles"`
	Matches  []MatchResult `json:"matches"`
	Total    int           `json:"total"`
}

func validName(name string) bool {
	if n := utf8.RuneCountInString(name); n < 1 || n > 10 {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func familyFailure() error {
	return fail("SAVE_UNAVAILABLE", "家庭记录暂时不可用，请稍后重试")
}
func (s *Store) Family(session string) (Family, error) {
	f := Family{Profiles: []Profile{}, Matches: []MatchResult{}}
	if s.storage == nil || session == "" {
		return f, familyFailure()
	}
	rows, err := s.storage.db.Query("SELECT id,name,avatar FROM family_profiles WHERE session=? ORDER BY rowid", session)
	if err != nil {
		return f, familyFailure()
	}
	for rows.Next() {
		var p Profile
		if err = rows.Scan(&p.ID, &p.Name, &p.Avatar); err != nil {
			break
		}
		f.Profiles = append(f.Profiles, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return f, familyFailure()
	}
	if err = s.storage.db.QueryRow("SELECT COUNT(*) FROM family_history WHERE session=? AND payload IS NOT NULL", session).Scan(&f.Total); err != nil {
		return f, familyFailure()
	}
	rows, err = s.storage.db.Query("SELECT payload FROM family_history WHERE session=? AND payload IS NOT NULL ORDER BY completed_at DESC,game_id DESC LIMIT 50", session)
	if err != nil {
		return f, familyFailure()
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		var match MatchResult
		if err = rows.Scan(&data); err != nil {
			return f, familyFailure()
		}
		if json.Unmarshal(data, &match) != nil {
			return f, familyFailure()
		}
		f.Matches = append(f.Matches, match)
	}
	if rows.Err() != nil {
		return f, familyFailure()
	}
	return f, nil
}
func (s *Store) SaveProfile(session string, p Profile) (Profile, error) {
	p.Name = strings.TrimSpace(p.Name)
	if session == "" || !validName(p.Name) || p.Avatar == "" || !game.ValidAvatar(p.Avatar) {
		return p, fail("INVALID_PROFILE", "昵称需要1到10个字，并选择一个头像")
	}
	if s.storage == nil {
		return p, familyFailure()
	}
	tx, err := s.storage.db.Begin()
	if err != nil {
		return p, familyFailure()
	}
	defer tx.Rollback()
	if p.ID == "" {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM family_profiles WHERE session=?", session).Scan(&count); err != nil {
			return p, familyFailure()
		}
		if count >= 12 {
			return p, fail("PROFILE_LIMIT", "最多保存12位家庭飞行员")
		}
		p.ID = game.ID()
		_, err = tx.Exec("INSERT INTO family_profiles(session,id,name,avatar) VALUES(?,?,?,?)", session, p.ID, p.Name, p.Avatar)
	} else {
		var result sql.Result
		result, err = tx.Exec("UPDATE family_profiles SET name=?,avatar=? WHERE session=? AND id=?", p.Name, p.Avatar, session, p.ID)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return p, fail("PROFILE_NOT_FOUND", "档案不存在，请重新选择")
			}
		}
	}
	if err != nil || tx.Commit() != nil {
		return p, familyFailure()
	}
	return p, nil
}
func (s *Store) DeleteProfile(session, id string) error {
	if s.storage == nil {
		return familyFailure()
	}
	_, err := s.storage.db.Exec("DELETE FROM family_profiles WHERE session=? AND id=?", session, id)
	if err != nil {
		return familyFailure()
	}
	return nil
}
func (s *Store) ClearHistory(session string) error {
	if s.storage == nil {
		return familyFailure()
	}
	// Keep only a game-ID receipt, so a later room save cannot recreate cleared data.
	_, err := s.storage.db.Exec("UPDATE family_history SET payload=NULL WHERE session=?", session)
	if err != nil {
		return familyFailure()
	}
	return nil
}
func (s *Store) profile(session, id string) (Profile, error) {
	var p Profile
	if s.storage == nil {
		return p, familyFailure()
	}
	err := s.storage.db.QueryRow("SELECT id,name,avatar FROM family_profiles WHERE session=? AND id=?", session, id).Scan(&p.ID, &p.Name, &p.Avatar)
	if err == sql.ErrNoRows {
		return p, fail("PROFILE_NOT_FOUND", "档案不存在，请重新选择")
	}
	if err != nil {
		return p, familyFailure()
	}
	return p, nil
}

// Resolve profile IDs in this browser's namespace; clients cannot name another browser's profile.
func (s *Store) FamilyOptions(session string, o game.Options) (game.Options, error) {
	if len(o.ProfileIDs) > len(o.Names) || len(o.Avatars) > len(o.Names) {
		return o, fail("INVALID_PROFILE", "档案与席位不匹配")
	}
	o.Names = append([]string(nil), o.Names...)
	ids, avatars := make([]string, len(o.Names)), make([]string, len(o.Names))
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for i := range o.Names {
		if i < len(o.Avatars) {
			avatars[i] = o.Avatars[i]
		}
		if !game.ValidAvatar(avatars[i]) {
			return o, fail("INVALID_PROFILE", "请选择内置头像")
		}
		if i < len(o.ProfileIDs) {
			ids[i] = o.ProfileIDs[i]
		}
		if o.Network && i > 0 || o.AI && i == len(o.Names)-1 {
			ids[i], avatars[i] = "", ""
		}
		if ids[i] != "" {
			if seenIDs[ids[i]] {
				return o, fail("INVALID_PROFILE", "同一档案一局只能入座一次")
			}
			p, err := s.profile(session, ids[i])
			if err != nil {
				return o, err
			}
			o.Names[i], avatars[i] = p.Name, p.Avatar
			seenIDs[ids[i]] = true
		}
		if seenNames[o.Names[i]] {
			return o, fail("INVALID_NAME", "请为每位玩家使用不同昵称")
		}
		seenNames[o.Names[i]] = true
	}
	o.ProfileIDs, o.Avatars = ids, avatars
	return o, nil
}
func archiveResult(ctx context.Context, tx *sql.Tx, p roomRecord) error {
	g := p.State
	if g == nil || g.Demo || g.Phase != "finished" || g.Winner == "" {
		return nil
	}
	recipients := map[string][]int{}
	if p.Invite == "" {
		for i, player := range g.Players {
			if !player.AI {
				recipients[p.Owner] = append(recipients[p.Owner], i)
			}
		}
	} else {
		for key, m := range p.Members {
			recipients[key] = []int{m.Index}
		}
	}
	for session, indices := range recipients {
		if session == "" {
			continue
		}
		result := MatchResult{GameID: g.ID, CompletedAt: p.SavedAt, RoomName: p.Name, Board: g.Board.Name, Mode: g.Mode, Preset: g.Preset, Difficulty: g.Difficulty, Network: p.Invite != "", Props: g.PropsEnabled, Players: []ResultPlayer{}}
		for i, player := range g.Players {
			entry := ResultPlayer{Name: player.Name, Avatar: player.Avatar, AI: player.AI, Won: player.ID == g.Winner || g.Winner == "team", Stats: player.Stats}
			for _, index := range indices {
				if index == i && i < len(p.Options.ProfileIDs) {
					entry.ProfileID = p.Options.ProfileIDs[i]
				}
			}
			result.Players = append(result.Players, entry)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO family_history(session,game_id,completed_at,payload) VALUES(?,?,?,?) ON CONFLICT(session,game_id) DO NOTHING", session, g.ID, result.CompletedAt.Format(time.RFC3339Nano), data); err != nil {
			return err
		}
	}
	return nil
}
