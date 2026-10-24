package room

import (
	"fmt"
	"github.com/zkevindev/flytogether/internal/game"
	"testing"
)

func TestNetworkBotOccupiesSeatAndDoesNotNeedAConnection(t *testing.T) {
	for _, count := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			st := NewStore()
			names := make([]string, count)
			for i := range names {
				names[i] = fmt.Sprintf("飞行员%d", i+1)
			}
			names[count-1] = "灵矽机器人"
			r, e := st.Create("host", game.Options{Names: names, Network: true, AI: true, Difficulty: "hard", Props: true})
			if e != nil {
				t.Fatal(e)
			}
			view := r.SnapshotFor("host")
			bot := view.Network.Seats[count-1]
			if !bot.Robot || !bot.Occupied || !bot.Ready || !bot.Online {
				t.Fatal("bot not ready without websocket", bot)
			}
			_, closeHost := r.SubscribeFor("host")
			defer closeHost()
			for i := 1; i < count-1; i++ {
				key := fmt.Sprint("guest", i)
				if _, e = st.Join(key, r.ID, view.Network.Invite, fmt.Sprint("朋友", i)); e != nil {
					t.Fatal(e)
				}
				_, e = networkCommand(t, r, "host", "start")
				errorCode(t, e, "PLAYERS_NOT_READY")
				_, closeGuest := r.SubscribeFor(key)
				defer closeGuest()
				if _, e = r.SetReady(key, true); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = st.Join("stranger", r.ID, view.Network.Invite, "新朋友"); e == nil {
				t.Fatal("claimed robot seat")
			}
			if _, e = networkCommand(t, r, "host", "start"); e != nil {
				t.Fatal(e)
			}
			r.mu.Lock()
			r.state.Current = count - 1
			before := r.state.Version
			r.mu.Unlock()
			_, e = networkCommand(t, r, "host", "roll")
			errorCode(t, e, "NOT_YOUR_TURN")
			if _, e = r.VoiceFor("host"); e == nil {
				t.Fatal("human voice acquired bot's turn")
			}
			r.AI()
			after := roomState(t, r)
			if after.Version <= before {
				t.Fatal("bot did not act")
			}
			if _, e = networkCommand(t, r, "host", "pause"); e != nil {
				t.Fatal(e)
			}
			paused := roomState(t, r)
			for i := 0; i < 4; i++ {
				r.AI()
			}
			if roomState(t, r).Version != paused.Version {
				t.Fatal("bot moved while paused")
			}
			if _, e = networkCommand(t, r, "host", "resume"); e != nil {
				t.Fatal(e)
			}
			if _, e = networkCommand(t, r, "host", "end"); e != nil {
				t.Fatal(e)
			}
			if _, e = networkCommand(t, r, "host", "replay"); e != nil {
				t.Fatal(e)
			}
			next := r.SnapshotFor("host")
			s := roomState(t, r)
			if !s.Players[count-1].AI || s.Difficulty != "hard" || next.Status != "waiting" {
				t.Fatal("replay lost bot/config")
			}
			if !next.Network.Seats[count-1].Ready || !next.Network.Seats[count-1].Robot || !next.Network.Seats[count-1].Occupied {
				t.Fatal("replay bot lost readiness")
			}
			for _, p := range s.Players {
				if p.Stats != (game.MatchStats{}) {
					t.Fatal("stats not reset")
				}
			}
			if count > 2 && next.Network.Seats[1].Ready {
				t.Fatal("human guest should prepare again")
			}
		})
	}
}
func TestNetworkBotNameCannotBeClaimedByHuman(t *testing.T) {
	st := NewStore()
	r, _ := st.Create("host", game.Options{Names: []string{"房主", "待入座", "灵矽机器人"}, Network: true, AI: true})
	_, e := st.Join("guest", r.ID, r.SnapshotFor("host").Network.Invite, "灵矽机器人")
	errorCode(t, e, "NAME_TAKEN")
	if _, e = st.Join("guest", r.ID, r.SnapshotFor("host").Network.Invite, "朋友"); e != nil {
		t.Fatal(e)
	}
	if r.SnapshotFor("guest").Me.PlayerID != "player-1" {
		t.Fatal("wrong seat")
	}
}
