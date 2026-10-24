package game

// Presets select fixed rules for a whole match. They never bias the random dice.
type RuleSpec struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Summary    string `json:"summary"`
	LaunchDice []int  `json:"launch_dice"`
}

func ValidPreset(id string) bool {
	return id == "" || id == "custom" || id == "gentle" || id == "classic"
}
func ResolveOptions(o Options) Options {
	switch o.Preset {
	case "gentle":
		o.Mode = "quick"
		o.Props = true
	case "classic":
		o.Mode = "classic"
		o.Props = false
	default:
		o.Preset = "custom"
	}
	return o
}
func (s *State) Rules() RuleSpec {
	switch s.Preset {
	case "gentle":
		return RuleSpec{"gentle", "亲子轻松", "5或6起飞；只有6可再掷；每人2架，先到1架获胜；起飞卡可直接把一架机库飞机送到起飞位；加速卡1、幸运骰1、护盾2、救援1、团队护航1，无攻击道具。", []int{5, 6}}
	case "classic":
		return RuleSpec{"classic", "经典飞行", "6点起飞并可再掷；每人4架，全部抵达获胜；无道具，终点需要恰好点数。", []int{6}}
	default:
		summary := "6点起飞并可再掷；每人2架，先到1架获胜。"
		if s.Mode == "classic" {
			summary = "6点起飞并可再掷；每人4架，全部抵达获胜。"
		}
		if s.PropsEnabled {
			summary += "道具库存见本局口袋；起飞卡可直接让一架机库飞机到起飞位；新局八种道具各1张。"
		} else {
			summary += "无道具。"
		}
		return RuleSpec{"custom", "自定义家庭规则", summary, []int{6}}
	}
}
func (s *State) CanLaunch(die int) bool { return die == 6 || (s.Preset == "gentle" && die == 5) }
func (s *State) LaunchRule() string {
	if s.Preset == "gentle" {
		return "五点或六点"
	}
	return "六点"
}
