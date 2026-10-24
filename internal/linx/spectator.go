package linx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zkevindev/flytogether/internal/game"
)

// Common board questions have exact answers. Let the provider read these
// facts aloud without embellishing them with remembered or inferred moves.
func spectatorNarration(text, fresh string) (string, bool) {
	var facts struct {
		Room struct {
			Status string     `json:"status"`
			State  game.State `json:"state"`
		} `json:"room"`
		Recent []string `json:"recent_events"`
	}
	if json.Unmarshal([]byte(fresh), &facts) != nil || facts.Room.State.ID == "" {
		return "", false
	}
	s := facts.Room.State
	if (strings.Contains(text, "帮") && (strings.Contains(text, "使用") || strings.Contains(text, "掷骰") || strings.Contains(text, "移动"))) || strings.Contains(text, "暂停游戏") || strings.Contains(text, "结束游戏") {
		return "我是你的观赛助手，不能替选手掷骰、移动、用卡或控制房间。你可以问当前回合、谁更接近终点，或刚才发生了什么。", true
	}
	if strings.Contains(text, "轮到谁") || strings.Contains(text, "谁的回合") {
		if s.Winner != "" {
			if s.Winner == "team" {
				return "星河救援已经成功，全队每位飞行员都送达了一架飞机。", true
			}
			for _, p := range s.Players {
				if p.ID == s.Winner {
					return p.Name + "已经获胜，这一局比赛结束了。", true
				}
			}
		}
		if s.Current < 0 || s.Current >= len(s.Players) {
			return "", false
		}
		p := s.Players[s.Current]
		switch facts.Room.Status {
		case "waiting":
			return "这一局还在等待开局，由房主开始。", true
		case "paused":
			return "比赛暂停中，继续后轮到" + p.Name + "。", true
		case "ended":
			return "房主已经结束这一局，可以查看最后的棋盘。", true
		}
		if s.Phase == "move" {
			return fmt.Sprintf("现在轮到%s，已经掷出%d点，正在选择飞机。", p.Name, s.Dice), true
		}
		return "现在轮到" + p.Name + "掷骰子。", true
	}
	if strings.Contains(text, "谁领先") || strings.Contains(text, "谁更接近终点") {
		lines := []string{}
		for _, p := range s.Players {
			arrived, furthest := 0, -1
			for _, piece := range p.Pieces {
				if piece.Progress >= s.Board.Finish {
					arrived++
				}
				if piece.Progress > furthest {
					furthest = piece.Progress
				}
			}
			if furthest < 0 {
				lines = append(lines, p.Name+"的飞机还都在机库")
			} else {
				lines = append(lines, fmt.Sprintf("%s有%d架抵达终点，最远飞机走到第%d格", p.Name, arrived, furthest))
			}
		}
		return strings.Join(lines, "；") + "。这是当前局面，之后的走棋和道具还会改变进度。", true
	}
	if strings.Contains(text, "刚才发生了什么") || strings.Contains(text, "最后一步") {
		if len(facts.Recent) == 0 {
			return "目前没有记录到最近的棋步，我不能猜测之前发生了什么。", true
		}
		return "最近记录的棋步是：" + facts.Recent[len(facts.Recent)-1], true
	}
	return "", false
}
