package linx

import (
	"encoding/json"
	"strings"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

type voiceAction struct {
	Name    string
	PieceID string
	PropID  string
	Notice  string
}

// Resolve explicit game commands before narration so success always follows a
// committed game action. Questions and ambiguous selections never choose a piece.
func planVoiceAction(r *room.Room, text string) (voiceAction, bool) {
	var state game.State
	if json.Unmarshal(r.Snapshot().State, &state) != nil {
		return voiceAction{}, false
	}
	return resolveVoiceAction(state, text)
}

func resolveVoiceAction(state game.State, text string) (voiceAction, bool) {
	text = normalizeWake(text)
	if len(state.Players) == 0 || state.Current < 0 || state.Current >= len(state.Players) {
		return voiceAction{}, false
	}
	if strings.Contains(text, "推荐") || strings.Contains(text, "建议") || strings.Contains(text, "帮我选") {
		return voiceAction{Name: "get_game_state", Notice: tacticalAdvice(state)}, true
	}
	for _, phrase := range []string{"不要", "不想", "别用", "别炸", "别加", "不用", "不使用", "不炸", "不加", "推荐", "建议", "别让", "别走", "别移动", "别起飞", "取消", "为什么", "怎么", "能不能", "可不可以", "能否", "吗", "哪", "规则"} {
		if strings.Contains(text, phrase) {
			return voiceAction{Name: "get_game_state", Notice: "这句话是否定、询问或规则讨论，没有执行游戏动作。请根据当前棋局回答，不要宣称已掷骰或移动。"}, true
		}
	}
	var props []string
	for _, id := range []string{"launch", "boost", "luck", "bomb", "shield"} {
		if strings.Contains(text, game.PropNames[id]) || (id == "luck" && strings.Contains(text, "幸运投")) {
			props = append(props, id)
		}
	}
	if len(props) > 1 {
		return voiceAction{Name: "get_game_state", Notice: "一次只能用一张道具，请选择起飞卡、加速卡、幸运骰、炸弹或护盾中的一张。"}, true
	}
	if len(props) == 1 {
		id := props[0]
		explicit := text == normalizeWake(game.PropNames[id])
		for _, verb := range []string{"使用", "用", "来一张", "来个", "加", "装", "套", "攻击", "释放"} {
			explicit = explicit || strings.Contains(text, verb)
		}
		if !explicit {
			return voiceAction{Name: "get_game_state", Notice: "只是提及道具，没有明确使用指令。本轮不操作，请回答问题或给出建议。"}, true
		}
		if id == "bomb" || id == "shield" {
			if (id == "bomb" && (strings.Contains(text, "我的") || strings.Contains(text, "自己"))) || (id == "shield" && strings.Contains(text, "对方")) {
				return voiceAction{Name: "get_game_state", Notice: "目标所属玩家不合法，没有使用道具。炸弹只能选对手，护盾只能保护自己。"}, true
			}
			labels := 0
			for _, label := range game.Labels {
				if strings.Contains(text, normalizeWake(label)) {
					labels++
				}
			}
			if labels > 1 {
				return voiceAction{Name: "get_game_state", Notice: "一次只能选择一架目标飞机，没有使用道具，请明确一位玩家的一架飞机。"}, true
			}
			allowed := state.PropTargets(id)
			var named []string
			// Match an explicit owner before matching labels shared by all players.
			owners := []int{}
			for i, p := range state.Players {
				if strings.Contains(text, normalizeWake(p.Name)) {
					owners = append(owners, i)
				}
			}
			for i, p := range state.Players {
				if len(owners) > 0 {
					found := false
					for _, owner := range owners {
						found = found || owner == i
					}
					if !found {
						continue
					}
				}
				for _, piece := range p.Pieces {
					for _, target := range allowed {
						if target == piece.ID && strings.Contains(text, normalizeWake(piece.Label)) {
							named = append(named, target)
						}
					}
				}
			}
			if len(named) == 1 {
				return voiceAction{Name: "use_prop", PropID: id, PieceID: named[0]}, true
			}
			choices := []string{}
			for _, p := range state.Players {
				for _, piece := range p.Pieces {
					for _, target := range allowed {
						if target == piece.ID {
							choices = append(choices, p.Name+"的"+piece.Label)
						}
					}
				}
			}
			notice := "还没有使用" + game.PropNames[id] + "。"
			if len(choices) == 0 {
				notice += "现在没有可选的航线飞机，请先掷骰子。"
			} else {
				notice += "请选择一架飞机，明确说出玩家和飞机昵称。可选：" + strings.Join(choices, "、") + "。"
			}
			return voiceAction{Name: "get_game_state", Notice: notice}, true
		}
		return voiceAction{Name: "use_prop", PropID: id}, true
	}
	if strings.Contains(text, "骰子") {
		for _, verb := range []string{"掷", "投", "扔", "摇", "丢"} {
			if strings.Contains(text, verb) {
				return voiceAction{Name: "roll_dice"}, true
			}
		}
	}
	move := false
	for _, verb := range []string{"起飞", "移动", "前进", "走", "撞"} {
		move = move || strings.Contains(text, verb)
	}
	if len(state.Players) == 0 || state.Current < 0 || state.Current >= len(state.Players) {
		return voiceAction{}, false
	}
	player := state.Players[state.Current]
	var named []game.Piece
	for _, piece := range player.Pieces {
		if strings.Contains(text, normalizeWake(piece.Label)) {
			named = append(named, piece)
		}
	}
	// A bare piece name answers the preceding selection question during a move.
	if !move && !(state.Phase == "move" && len(named) == 1 && text == normalizeWake(named[0].Label)) {
		return voiceAction{}, false
	}
	if len(named) == 1 {
		if strings.Contains(text, "起飞") && named[0].Progress >= 0 {
			return voiceAction{Name: "move_piece", Notice: "这架飞机已经离开机库，没有再次起飞或移动。请询问玩家是否要移动它。"}, true
		}
		return voiceAction{Name: "move_piece", PieceID: named[0].ID}, true
	}
	if len(named) > 1 {
		return voiceAction{Name: "move_piece", Notice: "一次只能操作一架飞机，请让玩家指定一架。"}, true
	}
	candidates := state.Legal
	if strings.Contains(text, "起飞") {
		candidates = nil
		for _, option := range state.Legal {
			if option.Launch {
				candidates = append(candidates, option)
			}
		}
	}
	if strings.Contains(text, "撞") {
		filtered := []game.Move{}
		for _, option := range candidates {
			if len(option.Captured) > 0 {
				filtered = append(filtered, option)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 1 {
		return voiceAction{Name: "move_piece", PieceID: candidates[0].PieceID}, true
	}
	if len(candidates) > 1 {
		labels := []string{}
		for _, option := range candidates {
			labels = append(labels, option.Label)
		}
		return voiceAction{Name: "move_piece", Notice: "尚未执行操作。请玩家从这些合法飞机中选择一架：" + strings.Join(labels, "、")}, true
	}
	reason := "当前没有符合指令的合法走法。请根据棋局说明原因。"
	if state.Phase == "roll" {
		reason = "尚未执行移动。当前需要先掷骰子；经典起飞需要六点，已经启用起飞卡则允许任意点数起飞。请引导玩家明确发出掷骰指令。"
	}
	return voiceAction{Name: "move_piece", Notice: reason}, true
}

func (g *wakeGate) commandText(text string) string {
	text = normalizeWake(text)
	words := []string{normalizeWake(g.settings.WakeWord)}
	if words[0] == "小灵同学" {
		words = append(words, "小玲同学", "小林同学", "小零同学", "小凌同学", "小铃同学", "小琳同学", "小宁同学")
	}
	for _, word := range words {
		if strings.Contains(text, word) {
			return strings.Replace(text, word, "", 1)
		}
	}
	return text
}

func actionOutput(r *room.Room, notice string) string {
	value, _ := r.Tool("get_game_state", nil)
	if state, ok := value.(map[string]any); ok {
		delete(state, "action_token")
	}
	result, _ := json.Marshal(map[string]any{"notice": notice, "state": value, "instruction": "本轮没有执行任何游戏动作。根据玩家的问题回答、解释真实限制或询问选择，不能宣称操作成功。"})
	return string(result)
}

func tacticalAdvice(state game.State) string {
	if state.Phase == "finished" {
		return "这局已经结束，可以和家人再开一局。没有执行操作。"
	}
	if state.Phase == "move" {
		move := state.BestMove()
		if move.PieceID != "" {
			return "建议走" + move.Label + "，可以" + move.Summary + "。这只是建议，请明确说走哪架飞机后才会执行。"
		}
	}
	id, target := state.SuggestedPropTarget()
	for _, p := range state.Players {
		for _, piece := range p.Pieces {
			if piece.ID == target {
				if id == "bomb" {
					return "建议用炸弹攻击" + p.Name + "的" + piece.Label + "，把对手送回机场。还没有使用，请明确说使用炸弹和目标。"
				}
				if id == "shield" {
					return "建议给" + p.Name + "的" + piece.Label + "加护盾，保住已经飞过的路程。还没有使用，等你明确选择。"
				}
			}
		}
	}
	switch id {
	case "launch":
		return "建议使用起飞卡，这样掷到任意点数都能起飞。还没有使用，请说使用起飞卡。"
	case "boost":
		return "建议使用加速卡，让已经起飞的飞机多走两格。还没有使用，等你明确选择。"
	case "luck":
		return "建议使用幸运骰，掷两次取较大值。还没有使用，等你明确选择。"
	}
	return "本回合没有合适的道具可用，先掷骰子，再选高亮飞机。还没有执行操作。"
}
