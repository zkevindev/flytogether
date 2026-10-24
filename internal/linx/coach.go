package linx

import (
	"encoding/json"
	"strings"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

// Follow-up selection is resolved against the server proposal, never model
// memory. Pronouns without a current proposal cannot fall through to a roll.
func planCoachAction(r room.GameSession, text string) (voiceAction, bool) {
	question := strings.HasSuffix(normalizeWake(text), "呢") || strings.ContainsAny(text, "?？") || strings.Contains(text, "吗") || strings.Contains(text, "能否") || strings.Contains(text, "能不能") || strings.Contains(text, "是不是")
	text = spoken(text)
	view := r.Snapshot()
	read := func(notice string) (voiceAction, bool) {
		return voiceAction{Name: "get_game_state", Notice: notice}, true
	}
	for _, word := range []string{"不要", "别", "不想", "取消", "先不", "再想想", "不走", "不确认", "不是", "等等", "稍等"} {
		if strings.Contains(text, word) {
			if view.Coach != nil && (strings.Contains(text, "走") || strings.Contains(text, "这个") || strings.Contains(text, "建议") || strings.Contains(text, "推荐") || strings.Contains(text, "取消") || strings.Contains(text, "再想想")) {
				return voiceAction{Name: "coach_move", Choice: "cancel"}, true
			}
			if view.Coach != nil {
				return read("没有执行操作。" + coachCue(view))
			}
			return read("先不操作，没有走棋或掷骰。")
		}
	}
	// Observed ASR homophone, only meaningful while comparing a live proposal.
	if text == "晾衣架" && view.Coach != nil {
		return voiceAction{Name: "coach_move", Choice: "next"}, true
	}
	choice := ""
	switch text {
	case "另一架", "另一架飞机", "换一架", "换一个", "换个", "看看另一架", "那另一架", "那另一架飞机":
		choice = "next"
	case "刚才那个", "就刚才那个", "还是刚才那个", "上一个", "回到刚才那个":
		choice = "previous"
	}
	if strings.Contains(text, "安全一点") || strings.Contains(text, "稳妥一点") || strings.Contains(text, "选安全") || strings.Contains(text, "更安全") {
		choice = "safe"
	}
	// Explicit prop advice keeps the existing no-consumption path.
	if (strings.Contains(text, "帮我选") || strings.Contains(text, "推荐") || strings.Contains(text, "建议走")) && !strings.Contains(text, "道具") && !strings.Contains(text, "卡") && !strings.Contains(text, "骰") {
		choice = "best"
	}
	if choice != "" {
		return voiceAction{Name: "coach_move", Choice: choice}, true
	}
	confirm := text == "确认飞行" || text == "就它" || text == "就这个" || text == "就这架" || text == "走这架" || text == "那就它" || text == "那就这个"
	if confirm || (agree(text) && (view.Coach != nil || view.CoachExpired)) {
		if question {
			return read("只是询问，还没有确认，也没有操作。" + coachCue(view))
		}
		if view.Coach == nil {
			return read("没有可确认的当前建议，棋局可能已变化。请先说“帮我选”，再明确说“就它”。没有走棋或掷骰。")
		}
		return voiceAction{Name: "confirm_coach", CoachID: view.Coach.ID}, true
	}
	// "那就兔子号" names the plane explicitly, but remains version-bound
	// when it confirms the highlighted proposal. Other explicit moves use the
	// existing named-plane resolver and authoritative legal-move validation.
	if view.Coach != nil && (strings.HasPrefix(text, "那就") || strings.HasPrefix(text, "就走") || strings.HasPrefix(text, "就")) && strings.Contains(text, view.Coach.Move.Label) && !question {
		return voiceAction{Name: "confirm_coach", CoachID: view.Coach.ID}, true
	}
	for _, prefix := range []string{"那就", "那就走", "就走", "就"} {
		for _, label := range game.Labels {
			if text != prefix+label {
				continue
			}
			if question {
				return read("只是询问，没有走棋。" + coachCue(view))
			}
			var state game.State
			if json.Unmarshal(view.State, &state) == nil {
				for _, move := range state.Legal {
					if move.Label == label {
						return voiceAction{Name: "move_piece", PieceID: move.PieceID}, true
					}
				}
			}
			return read("这架飞机本次不能走，请选择当前高亮的飞机。")
		}
	}
	return voiceAction{}, false
}

func coachCue(view room.View) string {
	if view.Coach == nil {
		return "请先说“帮我选”查看当前建议。"
	}
	c := view.Coach
	return "建议" + c.Move.Label + "：" + c.Reason + "。" + c.Risk + "棋盘已标出落点，只是建议，还没有移动飞机。说“就它”确认，或“另一架呢”比较。风险按当前棋局估算，不保证安全。"
}
