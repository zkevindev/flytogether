package game

import "fmt"

var PropNames = map[string]string{"launch": "起飞卡", "boost": "加速卡", "luck": "幸运骰", "bomb": "炸弹", "shield": "护盾"}

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
	if id == "bomb" || id == "shield" {
		targets := s.PropTargets(id)
		var target *Piece
		var owner string
		for i := range s.Players {
			for j := range s.Players[i].Pieces {
				piece := &s.Players[i].Pieces[j]
				for _, allowed := range targets {
					if piece.ID == pieceID && allowed == pieceID {
						target, owner = piece, s.Players[i].Name
					}
				}
			}
		}
		if target == nil {
			return fmt.Errorf("请指定%s的合法目标：炸弹选对手公共航线飞机，护盾选自己未受保护的公共航线飞机", name)
		}
		effect := owner + "的" + target.Label
		if id == "shield" {
			target.Shield = true
			effect += "获得护盾，抵挡一次炸弹或撞机"
		} else if target.Shield {
			target.Shield = false
			effect += "的护盾被炸弹抵消，飞机留在原地"
		} else {
			target.Progress = -1
			p.Stats.Captures++
			effect += "被送回机场"
		}
		p.Stats.PropsUsed++
		p.Props[id]--
		s.ActiveProp = id
		s.Version++
		s.add("prop", p.Name+"使用了"+name+"："+effect+"。请继续掷骰子。", pieceID)
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
	return s.SupportProp(), ""
}

// Normal difficulty uses only movement aids; hard adds targeted attack/defense.
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
	for i, player := range s.Players {
		if (id == "bomb" && i == s.Current) || (id == "shield" && i != s.Current) || (id != "bomb" && id != "shield") {
			continue
		}
		for _, piece := range player.Pieces {
			if piece.Progress > 0 && piece.Progress <= s.Board.RingLength && (id != "shield" || !piece.Shield) {
				out = append(out, piece.ID)
			}
		}
	}
	return out
}
