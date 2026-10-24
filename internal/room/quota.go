package room

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrVoiceBudget = errors.New("今天的语音额度已用完，仍可点击棋盘继续游戏")

// ReserveVoice counts upstream connection attempts, including failed attempts.
// It persists across restarts and has no audio, transcript or credential data.
func (s *Store) ReserveVoice(ctx context.Context, kind string, limit int) error {
	if s.storage == nil || limit < 1 || (kind != "realtime" && kind != "tts") {
		return errors.New("语音额度暂时不可用，请使用点击操作")
	}
	day := time.Now().UTC().Format("2006-01-02")
	var used int
	err := s.storage.db.QueryRowContext(ctx, `INSERT INTO voice_usage(day,kind,used) VALUES(?,?,1)
 ON CONFLICT(day,kind) DO UPDATE SET used=used+1 WHERE used < ? RETURNING used`, day, kind, limit).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVoiceBudget
	}
	if err != nil {
		return errors.New("语音额度暂时不可用，请使用点击操作")
	}
	return nil
}
