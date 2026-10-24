package game

import (
	"fmt"
	"strings"
)

var PropNames = map[string]string{"launch": "起飞卡", "boost": "加速卡", "luck": "幸运骰", "bomb": "炸弹", "shield": "护盾", "boomerang": "回旋镖", "rescue": "救援卡", "escort": "团队护航"}

// Props share one slot per roll; targeted effects commit immediately.
func (s *State) UseProp(id string) error { return s.UsePropOn(id, "") }

func (s *State) UsePropOn(id, pieceID string) error {
	if !s.PropsEnabled {
		return fmt.Errorf("这一局是经典无道具模式")
	}
	name, ok := PropNames[id]
	if !ok {
		return fmt.Errorf("未知的道具")
	}
	if s.Phase != "roll" {
		return fmt.Errorf("请在掷骰子之前使用道具")
	}
	if s.ActiveProp != "" {
		return fmt.Errorf("本次掷骰已经选择了道具，不能叠加使用")
	}
	if s.Preset == "gentle" && (id == "bomb" || id == "boomerang") {
		return fmt.Errorf("亲子轻松模式不提供攻击道具")
	}
	p := &s.Players[s.Current]
	if p.Props[id] <= 0 {
		return fmt.Errorf("你的%s已经用完了", name)
	}
	if id == "boost" {
		airborne := false
		for _, piece := range p.Pieces {
			airborne = airborne || (piece.Progress >= 0 && piece.Progress < s.Board.Finish)
		}
		if !airborne {
			return fmt.Errorf("加速卡需要至少一架已离开机库的飞机")
		}
	}
	if id == "launch" {
		hangar := false
		for _, piece := range p.Pieces {
			hangar = hangar || piece.Progress < 0
		}
		if !hangar {
			return fmt.Errorf("所有飞机都已离开机库，无需使用起飞卡")
		}
	}
	if id == "bomb" || id == "shield" || id == "boomerang" || id == "rescue" || id == "escort" {
		if id == "escort" && pieceID != "" {
			return fmt.Errorf("团队护航无需指定飞机")
		}
		targets := s.PropTargets(id)
		effects := []PropEffect{}
		descriptions := []string{}
		for i := range s.Players {
			for j := range s.Players[i].Pieces {
				target := &s.Players[i].Pieces[j]
				allowed := false
				for _, candidate := range targets {
					allowed = allowed || candidate == target.ID
				}
				if !allowed || (id != "escort" && target.ID != pieceID) {
					continue
				}
				from, kind := target.Progress, id
				text := s.Players[i].Name + "的" + target.Label
				switch {
				case id == "shield" || id == "escort":
					target.Shield = true
					text += "获得护盾，抵挡一次攻击或撞机"
				case id == "rescue":
					target.Progress, target.Shield = 0, false
					text += "安全撤回起飞位，护盾解除"
				case target.Shield:
					target.Shield = false
					kind = "blocked"
					text += "的护盾抵挡攻击，飞机留在原地"
				case id == "boomerang":
					target.Progress = max(1, target.Progress-3)
					text += fmt.Sprintf("后退%d格，不触发跳跃、飞行或撞机", from-target.Progress)
				default:
					target.Progress = -1
					p.Stats.Captures++
					text += "被送回机场"
				}
				effects = append(effects, PropEffect{PieceID: target.ID, From: from, To: target.Progress, Kind: kind})
				descriptions = append(descriptions, text)
			}
		}
		if len(effects) == 0 {
			return fmt.Errorf("请为%s选择合法公共航线目标：攻击选对手，救援和保护选自己", name)
		}
		p.Stats.PropsUsed++
		p.Props[id]--
		s.ActiveProp = id
		s.Version++
		description := strings.Join(descriptions, "；")
		if id == "escort" {
			labels := []string{}
			for _, effect := range effects {
				for _, piece := range p.Pieces {
					if piece.ID == effect.PieceID {
						labels = append(labels, piece.Label)
					}
				}
			}
			description = strings.Join(labels, "、") + "获得护盾，抵挡一次攻击或撞机"
		}
		s.add("prop", p.Name+"使用了"+name+"："+description+"。请继续掷骰子。", pieceID)
		s.History[len(s.History)-1].PropID = id
		s.History[len(s.History)-1].Effects = effects
		return nil
	}
	if pieceID != "" {
		return fmt.Errorf("这张道具无需指定飞机")
	}
	p.Stats.PropsUsed++
	p.Props[id]--
	s.ActiveProp = id
	s.Version++
	s.add("prop", p.Name+"使用了"+name+"，下一次掷骰生效。", "")
	s.History[len(s.History)-1].PropID = id
	return nil
}

func (s *State) SuggestedProp() string {
	id, _ := s.SuggestedPropTarget()
	return id
}

func (s *State) SuggestedPropTarget() (string, string) {
	if !s.PropsEnabled || s.Phase != "roll" || s.ActiveProp != "" {
		return "", ""
	}
	p := s.Players[s.Current]
	if p.Props["bomb"] > 0 {
		best, progress := "", -1
		for i, other := range s.Players {
			if i == s.Current {
				continue
			}
			for _, piece := range other.Pieces {
				if piece.Progress > progress && piece.Progress > 0 && piece.Progress <= s.Board.RingLength && !piece.Shield {
					best, progress = piece.ID, piece.Progress
				}
			}
		}
		if best != "" {
			return "bomb", best
		}
	}
	if p.Props["shield"] > 0 && len(s.PropTargets("bomb")) > 0 {
		for _, piece := range p.Pieces {
			if piece.Progress >= s.Board.RingLength/2 && piece.Progress <= s.Board.RingLength && !piece.Shield {
				return "shield", piece.ID
			}
		}
	}
	if p.Props["boomerang"] > 0 {
		for _, id := range s.PropTargets("boomerang") {
			for _, other := range s.Players {
				for _, piece := range other.Pieces {
					if piece.ID == id && piece.Progress > 3 && !piece.Shield {
						return "boomerang", id
					}
				}
			}
		}
	}
	if p.Props["escort"] > 0 && len(s.PropTargets("escort")) >= 2 {
		return "escort", ""
	}
	if p.Props["rescue"] > 0 {
		for _, id := range s.PropTargets("rescue") {
			for _, piece := range p.Pieces {
				if piece.ID == id && piece.Progress <= 3 && !piece.Shield && len(s.PropTargets("bomb")) > 0 {
					return "rescue", id
				}
			}
		}
	}
	return s.SupportProp(), ""
}

// Normal difficulty uses movement aids; hard adds targeted attack and defense.
func (s *State) SupportProp() string {
	if !s.PropsEnabled || s.Phase != "roll" || s.ActiveProp != "" {
		return ""
	}
	p := s.Players[s.Current]
	airborne, canBoost := false, false
	for _, piece := range p.Pieces {
		airborne = airborne || (piece.Progress >= 0 && piece.Progress < s.Board.Finish)
		canBoost = canBoost || (piece.Progress >= 0 && piece.Progress < s.Board.Finish-8)
	}
	if !airborne && p.Props["launch"] > 0 {
		return "launch"
	}
	if canBoost && p.Props["boost"] > 0 {
		return "boost"
	}
	if p.Props["luck"] > 0 {
		return "luck"
	}
	return ""
}

// PropTargets never includes hangars, launch pads or protected home lanes.
func (s *State) PropTargets(id string) []string {
	out := []string{}
	attack := id == "bomb" || id == "boomerang"
	own := id == "shield" || id == "rescue" || id == "escort"
	if !attack && !own {
		return out
	}
	for i, player := range s.Players {
		if (attack && i == s.Current) || (own && i != s.Current) {
			continue
		}
		for _, piece := range player.Pieces {
			if piece.Progress > 0 && piece.Progress <= s.Board.RingLength && ((id != "shield" && id != "escort") || !piece.Shield) {
				out = append(out, piece.ID)
			}
		}
	}
	return out
}
