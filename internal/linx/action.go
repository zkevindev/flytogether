package linx

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

type voiceAction struct {
	Name    string
	PieceID string
	PropID  string
	Notice  string
	Cheer   int
	Choice  string
	CoachID string
}

// Cheer is a spoken dice call. It never changes the roll; it is resolved after a real roll event.
type Cheer struct {
	Die   int
	After string
}

// Resolve explicit game commands before narration so success always follows a
// committed game action. Questions and ambiguous selections never choose a piece.
func planVoiceAction(r *room.Room, text string) (voiceAction, bool) {
	if action, handled := planCoachAction(r, text); handled {
		return action, true
	}
	var state game.State
	if json.Unmarshal(r.Snapshot().State, &state) != nil {
		return voiceAction{}, false
	}
	return resolveVoiceAction(state, text)
}

func resolveVoiceAction(state game.State, text string) (voiceAction, bool) {
	text = spoken(text)
	if len(state.Players) == 0 || state.Current < 0 || state.Current >= len(state.Players) {
		return voiceAction{}, false
	}
	if text == "" || text == "你好" || text == "在吗" || text == "嗨" || text == "喂" {
		return voiceAction{Name: "get_game_state", Notice: hostCue(state)}, true
	}
	if strings.Contains(text, "推荐") || strings.Contains(text, "建议") || strings.Contains(text, "帮我选") {
		return voiceAction{Name: "get_game_state", Notice: tacticalAdvice(state)}, true
	}
	for _, phrase := range []string{"不要", "不想", "别用", "别炸", "别加", "不用", "不使用", "不炸", "不加", "别让", "别走", "别移动", "别起飞", "取消"} {
		if strings.Contains(text, phrase) {
			return voiceAction{Name: "get_game_state", Notice: "先不这么做。" + hostCue(state)}, true
		}
	}
	for _, phrase := range []string{"为什么", "怎么", "能不能", "可不可以", "能否", "哪", "规则", "怎么办", "该我", "现在怎么样"} {
		if strings.Contains(text, phrase) {
			return voiceAction{Name: "get_game_state", Notice: hostCue(state)}, true
		}
	}
	if strings.Contains(text, "可以") && !agree(text) {
		return voiceAction{Name: "get_game_state", Notice: hostCue(state)}, true
	}
	if n, ok := calledDie(text); ok && state.Phase != "roll" {
		return voiceAction{Name: "get_game_state", Notice: "这把已经掷过了，是" + strconv.Itoa(state.Dice) + "点。" + hostCue(state), Cheer: n}, true
	}
	var props []string
	for _, id := range []string{"launch", "boost", "luck", "bomb", "shield"} {
		hit := strings.Contains(text, game.PropNames[id]) || (id == "luck" && (strings.Contains(text, "幸运投") || strings.Contains(text, "幸运头")))
		if id == "bomb" && state.Phase == "roll" && bombVerb(text) {
			hit = true
		}
		if hit {
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
		if id == "bomb" && bombVerb(text) {
			explicit = true
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
	cheer, called := calledDie(text)
	if rollVerb(text) || (called && state.Phase == "roll") {
		return voiceAction{Name: "roll_dice", Cheer: cheer}, true
	}
	if agree(text) {
		switch state.Phase {
		case "roll":
			return voiceAction{Name: "roll_dice"}, true
		case "move":
			if len(state.Legal) == 1 {
				return voiceAction{Name: "move_piece", PieceID: state.Legal[0].PieceID}, true
			}
			return voiceAction{Name: "get_game_state", Notice: "还没选飞机。" + hostCue(state)}, true
		default:
			return voiceAction{Name: "get_game_state", Notice: hostCue(state)}, true
		}
	}
	move := false
	for _, verb := range []string{"起飞", "移动", "前进", "走", "撞", "打回", "送回"} {
		move = move || strings.Contains(text, verb)
	}
	player := state.Players[state.Current]
	var named []game.Piece
	for _, piece := range player.Pieces {
		if strings.Contains(text, normalizeWake(piece.Label)) {
			named = append(named, piece)
		}
	}
	// A bare piece name answers the preceding selection question during a move.
	bare := state.Phase == "move" && len(named) == 1 && text == normalizeWake(named[0].Label)
	if len(named) == 1 && (move || bare) {
		if strings.Contains(text, "起飞") && named[0].Progress >= 0 {
			return voiceAction{Name: "move_piece", Notice: "这架飞机已经离开机库。" + hostCue(state)}, true
		}
		return voiceAction{Name: "move_piece", PieceID: named[0].ID}, true
	}
	if len(named) > 1 && (move || state.Phase == "move") {
		return voiceAction{Name: "move_piece", Notice: "一次只能操作一架飞机，请指定一架。"}, true
	}
	if delegated(text) {
		if state.Phase == "move" {
			if best := state.BestMove(); best.PieceID != "" {
				return voiceAction{Name: "move_piece", PieceID: best.PieceID}, true
			}
		}
		return voiceAction{Name: "get_game_state", Notice: hostCue(state)}, true
	}
	if candidates, relevant := intentMoves(state, text); relevant {
		return resolveCandidates(state, candidates, intentMiss(state, text))
	}
	if !move {
		return voiceAction{}, false
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
	return resolveCandidates(state, candidates, "")
}

func spoken(text string) string {
	text = normalizeWake(text)
	return strings.TrimRight(text, "吗吧呀啊呢哦哈嘛啦")
}

func agree(text string) bool {
	switch text {
	case "好", "好的", "行", "可以", "来", "来吧", "飞", "飞吧", "出发", "走起", "就这样", "就它", "就这个":
		return true
	default:
		return false
	}
}

func rollVerb(text string) bool {
	if !strings.Contains(text, "骰子") && !strings.Contains(text, "骰") {
		return false
	}
	for _, verb := range []string{"掷", "投", "扔", "摇", "丢"} {
		if strings.Contains(text, verb) {
			return true
		}
	}
	return false
}

func bombVerb(text string) bool {
	return strings.Contains(strings.ReplaceAll(text, "炸弹", ""), "炸")
}

func delegated(text string) bool {
	for _, phrase := range []string{"你来走", "帮我走", "走最好", "你决定", "你来选"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func calledDie(text string) (int, bool) {
	text = normalizeWake(text)
	nums := []struct {
		word string
		n    int
	}{{"1", 1}, {"2", 2}, {"3", 3}, {"4", 4}, {"5", 5}, {"6", 6}, {"一", 1}, {"二", 2}, {"两", 2}, {"三", 3}, {"四", 4}, {"五", 5}, {"六", 6}}
	for _, prefix := range []string{"我要", "想要", "赌", "来个", "喊"} {
		at := strings.Index(text, prefix)
		if at < 0 {
			continue
		}
		rest := text[at+len(prefix):]
		for _, num := range nums {
			if strings.HasPrefix(rest, num.word) {
				return num.n, true
			}
		}
	}
	return 0, false
}

func intentMoves(state game.State, text string) ([]game.Move, bool) {
	switch {
	case strings.Contains(text, "最前") || strings.Contains(text, "领头") || strings.Contains(text, "最快"):
		return leading(state.Legal, true), true
	case strings.Contains(text, "最后") || strings.Contains(text, "垫底"):
		return leading(state.Legal, false), true
	case strings.Contains(text, "回家") || strings.Contains(text, "终点") || strings.Contains(text, "冲线"):
		out := []game.Move{}
		for _, option := range state.Legal {
			if option.Finish {
				out = append(out, option)
			}
		}
		return out, true
	case strings.Contains(text, "撞") || strings.Contains(text, "打回") || strings.Contains(text, "送回") || (state.Phase == "move" && bombVerb(text)):
		out := []game.Move{}
		mentioned := len(mentionedOpponents(state, text)) > 0 || mentionedLabel(text)
		for _, option := range state.Legal {
			if len(option.Captured) == 0 {
				continue
			}
			if mentioned && !captureMatches(state, option, text) {
				continue
			}
			out = append(out, option)
		}
		return out, true
	default:
		return nil, false
	}
}

func leading(moves []game.Move, front bool) []game.Move {
	if len(moves) == 0 {
		return nil
	}
	best := moves[0].From
	for _, option := range moves[1:] {
		if (front && option.From > best) || (!front && option.From < best) {
			best = option.From
		}
	}
	out := []game.Move{}
	for _, option := range moves {
		if option.From == best {
			out = append(out, option)
		}
	}
	return out
}

func mentionedOpponents(state game.State, text string) []int {
	out := []int{}
	for i, player := range state.Players {
		if i == state.Current {
			continue
		}
		if strings.Contains(text, normalizeWake(player.Name)) {
			out = append(out, i)
		}
	}
	return out
}

func mentionedLabel(text string) bool {
	for _, label := range game.Labels {
		if strings.Contains(text, normalizeWake(label)) {
			return true
		}
	}
	return false
}

func captureMatches(state game.State, option game.Move, text string) bool {
	opponents := mentionedOpponents(state, text)
	wantLabel := mentionedLabel(text)
	for _, id := range option.Captured {
		player, piece, ok := findPiece(state, id)
		if !ok {
			continue
		}
		ownerOK := len(opponents) == 0
		for _, index := range opponents {
			ownerOK = ownerOK || state.Players[index].ID == player.ID
		}
		labelOK := !wantLabel || strings.Contains(text, normalizeWake(piece.Label))
		if ownerOK && labelOK {
			return true
		}
	}
	return false
}

func findPiece(state game.State, id string) (game.Player, game.Piece, bool) {
	for _, player := range state.Players {
		for _, piece := range player.Pieces {
			if piece.ID == id {
				return player, piece, true
			}
		}
	}
	return game.Player{}, game.Piece{}, false
}

func resolveCandidates(state game.State, candidates []game.Move, empty string) (voiceAction, bool) {
	if len(candidates) == 1 {
		return voiceAction{Name: "move_piece", PieceID: candidates[0].PieceID}, true
	}
	if len(candidates) > 1 {
		labels := []string{}
		for _, option := range candidates {
			labels = append(labels, option.Label)
		}
		return voiceAction{Name: "move_piece", Notice: "还没走棋。可以说其中一架：" + strings.Join(labels, "、") + "。"}, true
	}
	if empty == "" {
		empty = hostCue(state)
		if state.Phase == "roll" {
			empty = "现在还不能走棋。" + empty
		}
	}
	return voiceAction{Name: "move_piece", Notice: empty}, true
}

func intentMiss(state game.State, text string) string {
	if strings.Contains(text, "撞") || strings.Contains(text, "打回") || strings.Contains(text, "送回") || bombVerb(text) {
		if notice := blockedNotice(state); notice != "" {
			return notice
		}
		who := "对方"
		for _, index := range mentionedOpponents(state, text) {
			who = state.Players[index].Name
		}
		if state.Phase == "roll" {
			return "现在还撞不到" + who + "。" + hostCue(state)
		}
		return "这把撞不到" + who + "。" + hostCue(state)
	}
	if strings.Contains(text, "回家") || strings.Contains(text, "终点") || strings.Contains(text, "冲线") {
		return "这次没有飞机能正好到家。" + hostCue(state)
	}
	return hostCue(state)
}

func blockedNotice(state game.State) string {
	for _, option := range state.Legal {
		if len(option.Blocked) == 0 {
			continue
		}
		player, piece, ok := findPiece(state, option.Blocked[0])
		if !ok {
			continue
		}
		return player.Name + "的" + piece.Label + "有护盾，撞上去只会把盾打掉。可以说「走" + option.Label + "」。"
	}
	return ""
}

func hostCue(state game.State) string {
	if len(state.Players) == 0 || state.Current < 0 || state.Current >= len(state.Players) {
		return "棋局还在准备。"
	}
	if state.Phase == "finished" {
		name := "有人"
		for _, player := range state.Players {
			if player.ID == state.Winner {
				name = player.Name
			}
		}
		return name + "赢了这一局。想再飞，就重新开一局。"
	}
	player := state.Players[state.Current]
	if player.AI {
		return "轮到" + player.Name + "了，等它飞完再叫我。"
	}
	if state.Phase == "move" {
		return moveCue(state, player.Name)
	}
	return rollCue(state, player.Name)
}

func moveCue(state game.State, name string) string {
	if len(state.Legal) == 0 {
		return name + "，这枚骰子没有飞机能走。"
	}
	if len(state.Legal) == 1 {
		option := state.Legal[0]
		return name + "掷出" + strconv.Itoa(state.Dice) + "点，" + option.Label + "会" + option.Summary + "。回「好」就飞。"
	}
	for _, option := range state.Legal {
		if len(option.Captured) == 0 {
			continue
		}
		return name + "，" + option.Label + "能把" + capturedNames(state, option) + "撞回去。说「撞」，或者说「最前面」。"
	}
	for _, option := range state.Legal {
		if option.Finish {
			return name + "，" + option.Label + "可以到家。说「回家」，或者说另一架的名字。"
		}
	}
	labels := []string{}
	for _, option := range state.Legal {
		labels = append(labels, option.Label)
	}
	return name + "掷出" + strconv.Itoa(state.Dice) + "点，" + strings.Join(labels, "和") + "都能走。说「最前面」，或者说飞机名字。"
}

func capturedNames(state game.State, option game.Move) string {
	names := []string{}
	for _, id := range option.Captured {
		player, piece, ok := findPiece(state, id)
		if !ok {
			continue
		}
		names = append(names, player.Name+"的"+piece.Label)
	}
	if len(names) == 0 {
		return "对方"
	}
	return strings.Join(names, "和")
}

func rollCue(state game.State, name string) string {
	id, target := state.SuggestedPropTarget()
	if player, piece, ok := findPiece(state, target); ok && (id == "bomb" || id == "shield") {
		if id == "bomb" {
			return name + "，" + player.Name + "的" + piece.Label + "跑在前面。要炸就说「炸" + player.Name + "」，或者说「好」直接掷。"
		}
		return name + "，" + piece.Label + "已经飞得挺远。说「加护盾」，或者说「好」直接掷。"
	}
	switch id {
	case "launch":
		return name + "，飞机还在机场。说「用起飞卡」，或者说「好」，我来掷。"
	case "boost":
		return name + "，说「用加速卡」能多走两格，或者说「好」直接掷。"
	case "luck":
		return name + "，说「用幸运骰」会掷两次取大的，或者说「好」直接掷。"
	default:
		return name + "，骰子给你。先喊一个点数，比如「我要六点」，或者说「好」，我来掷。"
	}
}

func (c *Cheer) take(state game.State) string {
	if c == nil || c.Die == 0 {
		return ""
	}
	rolled := false
	for _, event := range eventsAfter(state.History, c.After) {
		if event.Kind == "rolled" {
			rolled = true
		}
	}
	if !rolled {
		return ""
	}
	want, got := c.Die, state.Dice
	c.Die = 0
	if got == want {
		return "你喊中了，就是" + strconv.Itoa(got) + "点。"
	}
	return "你喊的是" + strconv.Itoa(want) + "点，实际是" + strconv.Itoa(got) + "点。"
}

func eventsAfter(history []game.Event, id string) []game.Event {
	if len(history) == 0 || id == "" {
		return nil
	}
	for i, event := range history {
		if event.ID == id {
			if i+1 >= len(history) {
				return nil
			}
			return history[i+1:]
		}
	}
	return history[len(history)-1:]
}

func lastEventID(state game.State) string {
	if len(state.History) == 0 {
		return ""
	}
	return state.History[len(state.History)-1].ID
}

// nextHostLine is what the agent says after a click or an AI turn. seen is the last event already spoken.
func nextHostLine(state game.State, seen string) (string, string) {
	last := lastEventID(state)
	if seen == "" || last == "" || last == seen {
		return "", last
	}
	fresh := eventsAfter(state.History, seen)
	if len(fresh) == 0 {
		return "", last
	}
	dramas := []string{}
	rolled := false
	for _, event := range fresh {
		if event.Kind == "rolled" {
			rolled = true
		}
		if event.Kind == "won" || event.Kind == "bonus" || event.Kind == "prop" || (event.Kind == "moved" && dramatic(event.Text)) {
			dramas = append(dramas, strings.TrimRight(event.Text, "。"))
		}
	}
	if len(dramas) > 2 {
		dramas = dramas[len(dramas)-2:]
	}
	human := state.Current >= 0 && state.Current < len(state.Players) && !state.Players[state.Current].AI
	cue := ""
	if human && (state.Phase == "roll" || state.Phase == "move" || state.Phase == "finished") {
		cue = hostCue(state)
	}
	if len(dramas) == 0 && !rolled && state.Phase != "roll" {
		return "", last
	}
	if len(dramas) == 0 && !human {
		return "", last
	}
	if len(dramas) > 0 && cue != "" {
		return strings.Join(dramas, "。") + "。" + cue, last
	}
	if cue != "" {
		return cue, last
	}
	if len(dramas) > 0 {
		return strings.Join(dramas, "。") + "。", last
	}
	return "", last
}

func dramatic(text string) bool {
	for _, word := range []string{"回机场", "飞行", "跳跃", "终点", "护盾", "赢"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
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

// narrationFacts separates this command's committed events from historical dice.
func narrationFacts(state game.State, after string) string {
	events := eventsAfter(state.History, after)
	var facts []string
	rolled := false
	rollLine := ""
	for _, event := range events {
		facts = append(facts, event.Text)
		rolled = rolled || event.Kind == "rolled"
		if event.Kind == "rolled" {
			rollLine = event.Text
		}
	}
	if !rolled {
		return "本轮没有发生新的掷骰，不得声称刚投出了几点；棋局中的dice可能是历史点数。只能说明本轮实际操作、历史结果或当前阶段。"
	}
	return "本轮已提交的真实事件（点数只取这些新事件，不取历史结果）：" + strings.Join(facts, "；") + "。必须首先完整播报这句真实结果，不得只说已生效或省略点数：“" + rollLine + "”。"
}
