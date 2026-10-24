package room

import (
	"github.com/zkevindev/flytogether/internal/game"
	"time"
)

const DefaultHostReclaimDelay = time.Minute

// HostChange is a public room-management notice; it contains no membership credentials.
type HostChange struct {
	ID     string    `json:"id"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func (s *Store) SetHostReclaimDelay(delay time.Duration) {
	if delay <= 0 {
		delay = DefaultHostReclaimDelay
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostReclaimDelay = delay
	for _, r := range s.rooms {
		r.mu.Lock()
		r.hostReclaimDelay = delay
		r.hostOfflineSince = time.Time{}
		r.hostClaimable = false
		r.mu.Unlock()
	}
}
func (r *Room) hostDelay() time.Duration {
	if r.hostReclaimDelay > 0 {
		return r.hostReclaimDelay
	}
	return DefaultHostReclaimDelay
}

// Must hold r.mu. Only subscribed, authenticated human members start the grace period.
func (r *Room) updateHostPresence(now time.Time) bool {
	before, ready := r.hostOfflineSince, r.hostClaimable
	activeMember := false
	for _, session := range r.listeners {
		if r.isMember(session) {
			activeMember = true
			break
		}
	}
	if r.invite == "" || r.status == "ended" || r.state.Phase == "finished" || r.online(r.owner) || !activeMember {
		r.hostOfflineSince = time.Time{}
		r.hostClaimable = false
	} else {
		if r.hostOfflineSince.IsZero() {
			r.hostOfflineSince = now
		}
		r.hostClaimable = !now.Before(r.hostOfflineSince.Add(r.hostDelay()))
	}
	return !before.Equal(r.hostOfflineSince) || ready != r.hostClaimable
}
func (r *Room) refreshHostPresence() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateHostPresence(time.Now()) {
		r.publish()
	}
}

// Called after membership/idempotency/version checks, with r.mu held.
func (r *Room) changeHost(c Command, voice bool) (View, error) {
	if r.invite == "" || voice {
		return View{}, fail("INVALID_ACTION", "请通过联网房间管理调整房主")
	}
	if r.status == "ended" || r.state.Phase == "finished" {
		return View{}, fail("INVALID_TRANSITION", "本局已结束，请由房主组织再玩")
	}
	target := c.Actor
	reason := "reclaimed"
	if c.Action == "transfer_host" {
		target = ""
		reason = "transferred"
		for session, m := range r.members {
			if r.state.Players[m.index].ID == c.TargetPlayerID {
				target = session
				break
			}
		}
		if target == "" || target == r.owner || !r.online(target) {
			return View{}, fail("INVALID_HOST_TARGET", "请选择另一位已入座且在线的真人")
		}
	} else {
		if c.TargetPlayerID != "" {
			return View{}, fail("INVALID_COMMAND", "接任房主无需指定其他玩家")
		}
		r.updateHostPresence(time.Now())
		if c.Actor == r.owner || r.online(r.owner) {
			return View{}, fail("HOST_ONLINE", "房主仍在线，无需接任")
		}
		if !r.online(c.Actor) {
			return View{}, fail("NOT_AUTHORIZED", "请先回到联网房间并等待连接恢复")
		}
		if !r.hostClaimable {
			return View{}, fail("HOST_GRACE_PERIOD", "请等待房主重连，倒计时结束后可接任")
		}
	}
	if len(r.cache) >= 4096 {
		return View{}, fail("ROOM_LIMIT", "这局操作次数较多，请结束后开始新局")
	}
	previous := r.state.Players[r.members[r.owner].index].Name
	r.owner = target
	if r.status == "waiting" {
		m := r.members[target]
		m.ready = true
		r.members[target] = m
	}
	r.hostChange = &HostChange{ID: game.ID(), From: previous, To: r.state.Players[r.members[target].index].Name, Reason: reason, At: time.Now().UTC()}
	r.updateHostPresence(time.Now())
	// Management rights are independent of board versions and active speech grants.
	return r.finish(&c, saveChange{})
}
