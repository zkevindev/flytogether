package game

import "fmt"

// Mission is a small, optional team objective. Progress is derived only from
// committed game statistics and is stored in the room snapshot with the match.
type Mission struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Progress     int      `json:"progress"`
	Target       int      `json:"target"`
	Contributors []string `json:"contributors"`
	Status       string   `json:"status"`
}

type missionSpec struct {
	title, description string
	qualifies          func(Player) bool
	requiresProps      bool
}

var missionSpecs = map[string]missionSpec{
	"launch-formation": {"集结星际编队", "每位飞行员至少让一架飞机离开机库。", func(p Player) bool { return p.Stats.Takeoffs > 0 }, false},
	"signal-network":   {"建立全员联络", "每位飞行员至少完成 3 次掷骰。", func(p Player) bool { return p.Stats.Rolls >= 3 }, false},
	"supply-team":      {"共享补给网络", "每位飞行员至少使用一次道具。", func(p Player) bool { return p.Stats.PropsUsed > 0 }, true},
}

func MissionChoices(propsEnabled bool) []Mission {
	ids := []string{"launch-formation", "signal-network", "supply-team"}
	out := make([]Mission, 0, len(ids))
	for _, id := range ids {
		spec := missionSpecs[id]
		if spec.requiresProps && !propsEnabled {
			continue
		}
		out = append(out, Mission{ID: id, Title: spec.title, Description: spec.description, Status: "available"})
	}
	return out
}

func newMission(id string) *Mission {
	spec, ok := missionSpecs[id]
	if !ok {
		return nil
	}
	return &Mission{ID: id, Title: spec.title, Description: spec.description, Status: "active", Contributors: []string{}}
}

// RefreshMission must be called as part of the same room commit as the action
// that changed stats, before persistence and websocket publication.
func (s *State) RefreshMission(roomStatus string) {
	if s.Mission == nil || s.Mission.Status != "active" {
		return
	}
	spec, ok := missionSpecs[s.Mission.ID]
	if !ok {
		return
	}
	contributors := make([]string, 0, len(s.Players))
	for _, player := range s.Players {
		if spec.qualifies(player) {
			contributors = append(contributors, player.ID)
		}
	}
	s.Mission.Contributors = contributors
	s.Mission.Progress = len(contributors)
	s.Mission.Target = len(s.Players)
	if len(s.Players) > 0 && len(contributors) == len(s.Players) {
		s.Mission.Status = "completed"
	} else if s.Phase == "finished" || roomStatus == "ended" {
		s.Mission.Status = "incomplete"
	}
}

func validateMission(s *State) error {
	if s.ThemeID != "" && s.ThemeID != "star-rescue" {
		return fmt.Errorf("unsupported theme")
	}
	if s.Mission == nil {
		return nil
	}
	spec, ok := missionSpecs[s.Mission.ID]
	if !ok || s.Mode != "coop" || spec.requiresProps && !s.PropsEnabled || s.Mission.Title != spec.title || s.Mission.Description != spec.description {
		return fmt.Errorf("invalid mission")
	}
	if s.Mission.Status != "active" && s.Mission.Status != "completed" && s.Mission.Status != "incomplete" {
		return fmt.Errorf("invalid mission status")
	}
	if s.Mission.Progress < 0 || s.Mission.Target != len(s.Players) || s.Mission.Progress > s.Mission.Target || s.Mission.Progress != len(s.Mission.Contributors) {
		return fmt.Errorf("invalid mission progress")
	}
	seen := map[string]bool{}
	for _, id := range s.Mission.Contributors {
		found := false
		for _, p := range s.Players {
			if p.ID == id {
				found = true
				break
			}
		}
		if !found || seen[id] {
			return fmt.Errorf("invalid mission contributor")
		}
		seen[id] = true
	}
	return nil
}
