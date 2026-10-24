package linx

import (
	"encoding/json"
	"strings"

	"github.com/zkevindev/flytogether/internal/room"
)

// Action replies describe committed events; voice style may change delivery, not facts.
func actionNarration(r room.GameSession, a voiceAction, before, output string) string {
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(output), &failure)
	if failure.Error != "" {
		return "没有执行这个操作。" + failure.Error + "。"
	}
	if a.Notice != "" {
		return a.Notice
	}
	state, _, ok := roomGame(r)
	if !ok {
		return "暂时无法读取棋局，请查看页面。"
	}
	if a.Name == "use_prop" && a.PropID == "launch" {
		return "起飞卡已生效，接下来掷骰，任意点数都能让机库里的飞机起飞到起飞位。"
	}
	lines := []string{}
	for _, e := range eventsAfter(state.History, before) {
		switch e.Kind {
		case "rolled", "moved", "prop", "won", "bonus":
			lines = append(lines, strings.TrimRight(e.Text, "。"))
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	if len(lines) > 0 {
		return strings.Join(lines, "。") + "。" + hostCue(state)
	}
	return hostCue(state)
}
func readFactsInstruction(script string, p CompanionPreferences) string {
	delivery := "用温暖亲切的语气。"
	if p.Style == "coach" {
		delivery = "用耐心、清楚的教练语气。"
	}
	if p.Style == "concise" {
		delivery = "用简洁利落的语气。"
	}
	return "本次只做中文语音朗读，不再作答、推理或调用工具。逐字读出【】中的已核实文案，不增删事实、点数、位置、条件或昵称，不替换玩家称呼。风格只影响语气，不改文案。" + delivery + "朗读文案：【" + script + "】"
}
