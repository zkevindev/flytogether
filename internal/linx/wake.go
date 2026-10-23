package linx

import (
	"strings"
	"time"
	"unicode"

	"github.com/zkevindev/flytogether/internal/room"
)

const wakeWindow = 30 * time.Second

type wakeGate struct {
	settings room.VoiceSettings
	until    time.Time
}

func normalizeWake(text string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			return unicode.ToLower(c)
		}
		return -1
	}, text)
}

func (g *wakeGate) awake(now time.Time) bool {
	return !g.settings.WakeEnabled || now.Before(g.until)
}

func (g *wakeGate) accept(text string, now time.Time) bool {
	if normalizeWake(text) == "" {
		return false
	}
	matched := strings.Contains(normalizeWake(text), normalizeWake(g.settings.WakeWord))
	// These common transcription variants refer to the same default spoken name.
	if normalizeWake(g.settings.WakeWord) == "小灵同学" {
		for _, alias := range []string{"小玲同学", "小林同学", "小零同学", "小凌同学", "小铃同学", "小琳同学", "小宁同学"} {
			matched = matched || strings.Contains(normalizeWake(text), alias)
		}
	}
	if !g.awake(now) && !matched {
		return false
	}
	g.until = now.Add(wakeWindow)
	return true
}

func (g *wakeGate) finish(now time.Time) { g.until = now.Add(wakeWindow) }
func (g *wakeGate) sleep()               { g.until = time.Time{} }
