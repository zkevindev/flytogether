package game

import "testing"

func TestCoopMissionProgressIsIdempotentAndCompletesFromCommittedStats(t *testing.T) {
	s := New(Options{Mode: "coop", Names: []string{"孩子", "家长"}, MissionID: "launch-formation"})
	if s.ThemeID != "star-rescue" || s.Mission == nil || s.Mission.Target != 2 || s.Mission.Status != "active" {
		t.Fatalf("mission not initialized with theme and target: %+v", s)
	}
	s.Players[0].Stats.Takeoffs = 1
	s.RefreshMission("playing")
	s.RefreshMission("playing")
	if s.Mission.Progress != 1 || len(s.Mission.Contributors) != 1 || s.Mission.Status != "active" {
		t.Fatalf("first pilot progress should be counted once: %+v", s.Mission)
	}
	s.Players[1].Stats.Takeoffs = 1
	s.RefreshMission("playing")
	if s.Mission.Progress != 2 || s.Mission.Status != "completed" {
		t.Fatalf("all pilots should complete mission: %+v", s.Mission)
	}
	s.Players[0].Stats.Takeoffs = 4
	s.RefreshMission("playing")
	if s.Mission.Progress != 2 || s.Mission.Status != "completed" {
		t.Fatalf("repeated stats must not duplicate progress or undo completion: %+v", s.Mission)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid mission state failed validation: %v", err)
	}
}

func TestMissionChoicesAndTerminalStatus(t *testing.T) {
	if got := MissionChoices(false); len(got) != 2 {
		t.Fatalf("props-off mission choices = %d, want 2", len(got))
	}
	s := New(Options{Mode: "coop", Names: []string{"甲", "乙"}, MissionID: "signal-network"})
	s.Players[0].Stats.Rolls = 3
	s.RefreshMission("playing")
	if s.Mission.Progress != 1 {
		t.Fatalf("progress = %d, want 1", s.Mission.Progress)
	}
	s.RefreshMission("ended")
	if s.Mission.Status != "incomplete" {
		t.Fatalf("ended incomplete task status = %s", s.Mission.Status)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("terminal mission state failed validation: %v", err)
	}
}

func TestCoopWithoutMissionKeepsNormalCoopRules(t *testing.T) {
	s := New(Options{Mode: "coop", Names: []string{"甲", "乙"}})
	if s.ThemeID != "star-rescue" || s.Mission != nil || s.Mode != "coop" || s.Ruleset != "family-coop-v1" {
		t.Fatalf("co-op without an optional mission must retain its standard rules: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("co-op without a mission must be valid: %v", err)
	}
}

func TestUnfinishedMissionFailsWhenCoopGameFinishes(t *testing.T) {
	s := New(Options{Mode: "coop", Names: []string{"甲", "乙"}, MissionID: "signal-network"})
	s.Players[0].Stats.Rolls = 2
	s.Players[1].Stats.Rolls = 3
	s.Phase = "finished"
	s.Winner = "team"
	s.Players[0].Pieces[0].Progress = s.Board.Finish
	s.Players[1].Pieces[0].Progress = s.Board.Finish
	s.RefreshMission("playing")
	if s.Mission.Status != "incomplete" || s.Mission.Progress != 1 {
		t.Fatalf("unfinished mission should be marked incomplete at natural game end: %+v", s.Mission)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("terminal incomplete mission must be valid: %v", err)
	}
}

func TestMissionValidationRejectsTamperedState(t *testing.T) {
	s := New(Options{Mode: "coop", Names: []string{"甲", "乙"}, MissionID: "launch-formation"})
	s.Mission.Contributors = []string{"not-a-seat"}
	s.Mission.Progress = 1
	if err := s.Validate(); err == nil {
		t.Fatal("accepted non-seat mission contributor")
	}
}
