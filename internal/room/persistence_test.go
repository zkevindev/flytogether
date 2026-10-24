package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zkevindev/flytogether/internal/game"
)

func openTestSaves(t *testing.T, path string) *Store {
	t.Helper()
	s, e := OpenStore(path, DefaultRetention)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func savedDo(t *testing.T, r *Room, actor, action string, extra func(*Command)) Command {
	t.Helper()
	g := current(r)
	c := Command{ID: game.ID(), GameID: g.ID, Version: g.Version, Action: action}
	if extra != nil {
		extra(&c)
	}
	if _, e := r.DoAs(c, actor); e != nil {
		t.Fatal(e)
	}
	return c
}
func TestPersistentPropDiceCoachAndIdempotencyAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, e := s.Create("browser", game.Options{Names: []string{"孩子", "家长"}, Props: true, Demo: true})
	if e != nil {
		t.Fatal(e)
	}
	savedDo(t, r, "browser", "prop", func(c *Command) { c.PropID = "boost" })
	roll := savedDo(t, r, "browser", "roll", nil)
	if current(r).Phase != "move" {
		t.Fatal("demo must have two legal choices")
	}
	savedDo(t, r, "browser", "coach", func(c *Command) { c.Choice = "best" })
	if e = r.BeginSpeech(); e != nil {
		t.Fatal(e)
	}
	token := r.CurrentActionToken()
	before := r.SnapshotFor("browser")
	if before.SavedAt == nil {
		t.Fatal("no save time")
	}
	s.Close()
	reopened := openTestSaves(t, path)
	restored := reopened.Get("browser", r.ID)
	if restored == nil {
		t.Fatal("session lost")
	}
	next := restored.SnapshotFor("browser")
	if string(before.State) != string(next.State) || next.Status != "paused" || !next.Restored || next.Instance == before.Instance || next.Voice != "disconnected" || next.Coach != nil {
		t.Fatal("restart did not safely restore exact board", next.Status)
	}
	if restored.CurrentActionToken() != "" {
		t.Fatal("restored voice token")
	}
	if _, e = restored.DoAs(roll, "browser"); e != nil {
		t.Fatal("retry should read saved result", e)
	}
	if string(next.State) != string(restored.Snapshot().State) {
		t.Fatal("retry rerolled")
	}
	if _, e = restored.Tool("roll_dice", map[string]string{"action_token": token}); e == nil {
		t.Fatal("old voice grant accepted")
	}
	if e = restored.BeginSpeech(); e == nil {
		t.Fatal("speech while restored paused")
	}
	savedDo(t, restored, "browser", "resume", nil)
	g := current(restored)
	savedDo(t, restored, "browser", "move", func(c *Command) { c.PieceID = g.Legal[0].PieceID })
	reopened.Close()
	twice := openTestSaves(t, path)
	again := twice.Get("browser", r.ID)
	if again == nil || again.Snapshot().Status != "paused" || !again.Snapshot().Restored {
		t.Fatal("second restart auto resumed")
	}
}

func TestCoopMissionPersistsAndSameConfigReplayResetsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missions.db")
	s := openTestSaves(t, path)
	r, err := s.Create("owner", game.Options{Names: []string{"孩子", "家长"}, Mode: "coop", Props: true, ThemeID: "star-rescue", MissionID: "launch-formation"})
	if err != nil { t.Fatal(err) }
	state := current(r)
	command := Command{ID: "mission-launch", GameID: state.ID, Version: state.Version, Action: "prop", PropID: "launch", PieceID: "p0-0"}
	first, err := r.DoAs(command, "owner")
	if err != nil { t.Fatal(err) }
	if _, err = r.DoAs(command, "owner"); err != nil { t.Fatalf("idempotent retry failed: %v", err) }
	var played game.State
	if err = json.Unmarshal(first.State, &played); err != nil { t.Fatal(err) }
	if played.Mission.Progress != 1 || played.Mission.Status != "active" || played.ThemeID != "star-rescue" { t.Fatalf("committed mission state: %+v", played.Mission) }
	roomID := r.ID
	s.Close()
	s = openTestSaves(t, path)
	restored := s.Get("owner", roomID)
	if restored == nil { t.Fatal("saved room not restored") }
	var loaded game.State
	if err = json.Unmarshal(restored.Snapshot().State, &loaded); err != nil { t.Fatal(err) }
	if loaded.ThemeID != "star-rescue" || loaded.Mission == nil || loaded.Mission.Progress != 1 || loaded.Mission.Contributors[0] != "player-0" { t.Fatalf("persisted mission/theme missing: %+v", loaded.Mission) }
	ended, err := restored.Do(Command{ID:"end-mission", GameID:loaded.ID, Version:loaded.Version, Action:"end"}, false)
	if err != nil { t.Fatal(err) }
	var afterEnd game.State
	_ = json.Unmarshal(ended.State, &afterEnd)
	if afterEnd.Mission.Status != "incomplete" { t.Fatalf("end should mark mission incomplete: %+v", afterEnd.Mission) }
	replay, err := restored.Do(Command{ID:"replay-mission", GameID:afterEnd.ID, Version:afterEnd.Version, Action:"replay"}, false)
	if err != nil { t.Fatal(err) }
	var replayed game.State
	_ = json.Unmarshal(replay.State, &replayed)
	if replayed.ThemeID != "star-rescue" || replayed.Mission == nil || replayed.Mission.ID != "launch-formation" || replayed.Mission.Progress != 0 || replayed.Mission.Status != "active" { t.Fatalf("same-config replay lost or carried mission progress: %+v", replayed.Mission) }
}
func TestPersistentFailureRollsBackAndDoesNotBroadcast(t *testing.T) {
	s := openTestSaves(t, filepath.Join(t.TempDir(), "games.db"))
	r, _ := s.Create("browser", game.Options{Names: []string{"甲", "乙"}, Props: true})
	ch, close := r.SubscribeFor("browser")
	defer close()
	<-ch
	before := r.Snapshot()
	g := current(r)
	c := Command{ID: game.ID(), GameID: g.ID, Version: g.Version, Action: "prop", PropID: "luck"}
	if _, e := s.storage.db.Exec("PRAGMA query_only=ON"); e != nil {
		t.Fatal(e)
	}
	_, e := r.DoAs(c, "browser")
	errorCode(t, e, "SAVE_UNAVAILABLE")
	if string(before.State) != string(r.Snapshot().State) || r.Snapshot().Seq != before.Seq || len(r.cache) != 0 {
		t.Fatal("failed write changed board or cache")
	}
	select {
	case <-ch:
		t.Fatal("uncommitted state broadcast")
	default:
	}
	_, _ = s.storage.db.Exec("PRAGMA query_only=OFF")
	if _, e = r.DoAs(c, "browser"); e != nil {
		t.Fatal(e)
	}
	after := current(r)
	if after.Players[0].Props["luck"] != 0 || after.Players[0].Stats.Rolls != 1 {
		t.Fatal("retry did not commit exactly once")
	}
}
func TestPersistentMembershipAndBotFreeze(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _ := s.Create("host", game.Options{Names: []string{"房主", "待入座", "机器人"}, Network: true, AI: true, Props: true, Difficulty: "hard"})
	invite := r.SnapshotFor("host").Network.Invite
	if _, e := s.Join("guest", r.ID, invite, "朋友"); e != nil {
		t.Fatal(e)
	}
	_, ch := r.SubscribeFor("host")
	defer ch()
	_, cg := r.SubscribeFor("guest")
	defer cg()
	_, _ = r.SetReady("guest", true)
	savedDo(t, r, "host", "start", nil)
	// Exercise the same commit boundary with a bot-current snapshot, then reopen.
	r.mu.Lock()
	r.state.Current = 2
	_, e := r.finish(nil, saveChange{})
	r.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	before := current(r)
	s.Close()
	next := openTestSaves(t, path)
	bot := next.Get("host", r.ID)
	if next.Get("guest", r.ID) != bot || bot.SnapshotFor("guest").Me.PlayerID != "player-1" || bot.SnapshotFor("host").Network.Invite != invite {
		t.Fatal("membership/invite lost")
	}
	for i := 0; i < 4; i++ {
		bot.AI()
	}
	if current(bot).Version != before.Version {
		t.Fatal("bot ran before host resume")
	}
	savedDo(t, bot, "host", "resume", nil)
	bot.AI()
	if current(bot).Version <= before.Version {
		t.Fatal("bot did not resume")
	}
	if _, e = next.Join("outsider", r.ID, invite, "陌生人"); e == nil {
		t.Fatal("new user took existing seat")
	}
}
func TestPersistentJoinAndReplaceAreAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _ := s.Create("host", game.Options{Names: []string{"房主", "空位"}, Network: true})
	_, _ = s.storage.db.Exec("PRAGMA query_only=ON")
	if _, e := s.Join("guest", r.ID, r.Snapshot().Network.Invite, "朋友"); e == nil {
		t.Fatal("join acknowledged without disk")
	}
	if r.HasMember("guest") || s.Get("guest", "") != nil {
		t.Fatal("failed join leaked membership")
	}
	if _, e := s.CreateNew("host", game.Options{Names: []string{"新房主", "家人"}}); e == nil {
		t.Fatal("replace without disk")
	}
	if s.Get("host", "") != r || r.Snapshot().Status != "waiting" {
		t.Fatal("failed replace destroyed old room")
	}
	_, _ = s.storage.db.Exec("PRAGMA query_only=OFF")
	if _, e := s.Join("guest", r.ID, r.Snapshot().Network.Invite, "朋友"); e != nil {
		t.Fatal(e)
	}
	if e := s.Leave("guest", r.ID); e != nil {
		t.Fatal(e)
	}
	s.Close()
	next := openTestSaves(t, path)
	if next.Get("guest", r.ID) != nil {
		t.Fatal("left member returned")
	}
	if e := next.Discard("guest", r.ID); e == nil {
		t.Fatal("guest deleted host save")
	}
	if e := next.Discard("host", r.ID); e != nil {
		t.Fatal(e)
	}
	next.Close()
	final := openTestSaves(t, path)
	if final.Get("host", r.ID) != nil {
		t.Fatal("deleted save resurrected")
	}
}
func TestInvalidSnapshotQuarantinedAndFutureDatabasePreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _ := s.Create("owner", game.Options{Names: []string{"甲", "乙"}})
	_, e := s.storage.db.Exec("UPDATE rooms SET payload=? WHERE id=?", []byte(`{"schema":1,"state":null}`), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	next := openTestSaves(t, path)
	if len(next.rooms) != 0 {
		t.Fatal("corrupt room restored")
	}
	var count int
	_ = next.storage.db.QueryRow("SELECT count(*) FROM quarantine").Scan(&count)
	if count != 1 {
		t.Fatal("bad data not preserved")
	}
	_, _ = next.storage.db.Exec("PRAGMA user_version=99")
	next.Close()
	if _, e = OpenStore(path, DefaultRetention); e == nil {
		t.Fatal("silently downgraded future schema")
	}
	stat, e := os.Stat(path)
	if e != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}
func TestSaveRetentionDoesNotResetAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	s := openTestSaves(t, path)
	r, _ := s.Create("owner", game.Options{Names: []string{"甲", "乙"}})
	p := r.record()
	p.Updated = time.Now().Add(-8 * 24 * time.Hour)
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	_, _ = s.storage.db.Exec("UPDATE rooms SET payload=?,checksum=? WHERE id=?", data, hex.EncodeToString(sum[:]), r.ID)
	s.Close()
	next := openTestSaves(t, path)
	if len(next.rooms) != 0 || next.Get("owner", "") != nil {
		t.Fatal("expired save restored")
	}
}

func TestExpandedPropsSurviveRestartAndRetry(t *testing.T) {
	for _, id := range []string{"boomerang", "rescue", "escort"} {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "games.db")
			s := openTestSaves(t, path)
			r, e := s.Create("browser", game.Options{Names: []string{"孩子", "家长"}, Props: true, Demo: true})
			if e != nil {
				t.Fatal(e)
			}
			c := savedDo(t, r, "browser", "prop", func(c *Command) {
				c.PropID = id
				if id == "boomerang" {
					c.PieceID = "p1-0"
				} else if id == "rescue" {
					c.PieceID = "p0-0"
				}
			})
			before := r.Snapshot()
			s.Close()
			again := openTestSaves(t, path)
			restored := again.Get("browser", r.ID)
			if restored == nil {
				t.Fatal("new prop rejected on restore")
			}
			if string(restored.Snapshot().State) != string(before.State) {
				t.Fatal("effect/inventory lost on restart")
			}
			if _, e = restored.DoAs(c, "browser"); e != nil {
				t.Fatal(e)
			}
			if string(restored.Snapshot().State) != string(before.State) {
				t.Fatal("retry repeated effect")
			}
		})
	}
}
