package linx

import (
	"errors"
	"strings"

	"github.com/zkevindev/flytogether/internal/game"
)

// CompanionPreferences belongs to a browser, never to all seats of a room.
type CompanionPreferences struct {
	Style     string `json:"style"`
	Frequency string `json:"frequency"`
}

func (p CompanionPreferences) Normalized() (CompanionPreferences, error) {
	if p.Style == "" {
		p.Style = "warm"
	}
	if p.Frequency == "" {
		p.Frequency = "normal"
	}
	if p.Style != "warm" && p.Style != "coach" && p.Style != "concise" {
		return p, errors.New("请选择温暖、教练或简洁风格")
	}
	if p.Frequency != "normal" && p.Frequency != "highlights" && p.Frequency != "quiet" {
		return p, errors.New("请选择每回合、关键时刻或只问才答")
	}
	return p, nil
}
func (p CompanionPreferences) Instructions() string {
	switch p.Style {
	case "coach":
		return "用耐心教练的语气，先说真实结果；必要时补一条当前规则的小提示，不替玩家做选择，不夸大安全性。"
	case "concise":
		return "语气简洁，通常只说一句真实结果或当前需要的操作，不额外寒暄、鼓励或重复菜单。"
	default:
		return "语气温暖平等，遇到失利不责怪、不嘲讽，可以简短鼓励，不替玩家决定。"
	}
}
func importantMove(state game.State) bool {
	m := state.LastMove
	return m != nil && (m.Finish || len(m.Captured) > 0 || len(m.Blocked) > 0 || m.Fly || m.Jump)
}
func companionTip(state game.State) string {
	if state.Phase == "move" {
		return "先看亮起的飞机和落点，确认后再飞。"
	}
	if state.PropsEnabled && state.ActiveProp == "launch" {
		return "起飞卡已经生效，这次任意点数都能起飞。"
	}
	if state.Players[state.Current].Pieces[0].Progress < 0 {
		if state.PropsEnabled && state.Players[state.Current].Props["launch"] > 0 {
			return "机库里的飞机需要掷到六点，或先使用起飞卡。"
		}
		return "机库里的飞机需要掷到六点才能起飞。"
	}
	return "抵达终点需要恰好的点数，超过终点不能走。"
}
func (p CompanionPreferences) Opening(state game.State) string {
	if p.Frequency == "quiet" || (p.Frequency == "highlights" && state.Turn != 1 && state.Phase != "finished") {
		return ""
	}
	line := hostCue(state)
	if p.Style == "coach" && state.Turn == 1 {
		line += "先看落点再确认，不着急。"
	}
	return line
}
func (p CompanionPreferences) Next(state game.State, seen string) (string, string) {
	line, mark := nextHostLine(state, seen)
	if p.Frequency == "quiet" {
		return "", mark
	}
	if p.Frequency == "highlights" {
		significant := false
		for _, e := range eventsAfter(state.History, seen) {
			if e.Kind == "won" || (e.Kind == "moved" && dramatic(e.Text)) {
				significant = true
			}
		}
		if !significant {
			return "", mark
		}
	}
	if line != "" && p.Style == "coach" && state.Turn%4 == 1 && state.Phase != "finished" {
		line += companionTip(state)
	}
	return line, mark
}

// Announcement is deterministic, uses committed game facts, and never runs game tools.
func (p CompanionPreferences) Announcement(state game.State) (string, string) {
	key, line := TurnAnnouncement(state)
	if key == "" || p.Frequency == "quiet" {
		return "", ""
	}
	if p.Frequency == "highlights" && state.Turn != 1 && state.Phase != "finished" {
		if !importantMove(state) {
			return "", ""
		}
		for i := len(state.History) - 1; i >= 0; i-- {
			if state.History[i].Kind == "moved" {
				line = state.History[i].Text + "。" + line
				break
			}
		}
	}
	if p.Style == "concise" {
		line = strings.ReplaceAll(line, "掷骰子啦！", "掷骰子。")
	} else if p.Style == "coach" && state.Phase != "finished" && (state.Turn == 1 || state.Turn%4 == 1) {
		line += companionTip(state)
	}
	return key, line
}
