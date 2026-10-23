package game

import "fmt"

var PropNames = map[string]string{"launch": "起飞卡", "boost": "加速卡", "luck": "幸运骰"}

// A prop arms exactly one following roll; it never creates a synthetic six.
func (s *State) UseProp(id string) error {
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
	p.Props[id]--
	s.ActiveProp = id
	s.Version++
	s.add("prop", p.Name+"使用了"+name+"，下一次掷骰生效。", "")
	return nil
}

func (s *State) SuggestedProp() string {
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
