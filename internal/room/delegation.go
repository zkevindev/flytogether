package room

import (
	"github.com/zkevindev/flytogether/internal/game"
	"time"
)

const DefaultDelegationDelay = time.Minute

func (s *Store) SetDelegationDelay(delay time.Duration) {
	if delay <= 0 {
		delay = DefaultDelegationDelay
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delegationDelay = delay
	for _, r := range s.rooms {
		r.mu.Lock()
		r.delegationDelay = delay
		for key, m := range r.members {
			m.offlineSince = time.Time{}
			r.members[key] = m
		}
		r.mu.Unlock()
	}
}
func (r *Room) delegationWait() time.Duration {
	if r.delegationDelay > 0 {
		return r.delegationDelay
	}
	return DefaultDelegationDelay
}
func (r *Room) humanOnline() bool {
	for _, session := range r.listeners {
		if r.isMember(session) {
			return true
		}
	}
	return false
}
func (r *Room) delegated(index int) bool {
	for _, m := range r.members {
		if m.index == index {
			return m.delegated
		}
	}
	return false
}

// Only a lost authenticated game subscription counts as offline. A hidden tab is still online.
func (r *Room) updateDelegationPresence(now time.Time) bool {
	changed := false
	active := r.invite != "" && r.status == "playing" && r.state.Phase != "finished" && r.humanOnline()
	for session, m := range r.members {
		before := m.offlineSince
		if !active || !m.autoDelegate || m.delegated || r.online(session) {
			m.offlineSince = time.Time{}
		} else if m.offlineSince.IsZero() {
			m.offlineSince = now
		}
		changed = changed || !before.Equal(m.offlineSince)
		r.members[session] = m
	}
	return changed
}
func (r *Room) delegationEvent(session, reason string, delegated bool) {
	m := r.members[session]
	m.delegated = delegated
	m.delegationReason = reason
	m.offlineSince = time.Time{}
	r.members[session] = m
	text := r.state.Players[m.index].Name
	if delegated {
		if reason == "offline" {
			text += "断线等待结束，按本人预先选择开启托管"
		} else {
			text += "暂离并开启托管"
		}
		text += "；只掷骰与走棋，不使用道具。"
	} else {
		text += "已回来接管，继续当前棋局，已掷点数保留。"
	}
	r.state.Version++
	m = r.members[session]
	m.delegationVersion = 0
	if delegated {
		m.delegationVersion = r.state.Version
	}
	r.members[session] = m
	r.state.History = append(r.state.History, game.Event{ID: game.ID(), Kind: "delegation", Text: text, Player: r.state.Players[m.index].ID})
	if len(r.state.History) > 24 {
		r.state.History = r.state.History[len(r.state.History)-24:]
	}
	r.invalidateCoach()
	// Any pending tools for this seat lose their lease, including a late result after reclaim.
	if r.voiceActor == session || m.index == r.state.Current {
		r.voiceLease = ""
		r.voiceActor = ""
		r.grant = ""
		r.grantActor = ""
		r.busy = false
		r.used = false
		r.voice = "disconnected"
	}
}
func (r *Room) changeDelegation(c Command, voice bool) (View, error) {
	if r.invite == "" || voice {
		return View{}, fail("INVALID_ACTION", "请通过本人联网席位设置暂离托管")
	}
	m, ok := r.members[c.Actor]
	if !ok || !r.online(c.Actor) {
		return View{}, fail("NOT_AUTHORIZED", "请先回到房间并等待连接恢复")
	}
	if c.TargetPlayerID != "" || c.PieceID != "" || c.PropID != "" {
		return View{}, fail("INVALID_COMMAND", "只能设置自己的席位，无需指定玩家或飞机")
	}
	if r.status == "ended" || r.state.Phase == "finished" {
		return View{}, fail("GAME_FINISHED", "本局已结束，不能再调整托管")
	}
	if len(r.cache) >= 4096 {
		return View{}, fail("ROOM_LIMIT", "本局操作较多，请结束后开始新局")
	}
	switch c.Action {
	case "delegate":
		if r.status == "waiting" {
			return View{}, fail("GAME_NOT_ACTIVE", "开始游戏后才能暂离托管")
		}
		if m.delegated {
			return View{}, fail("ALREADY_DELEGATED", "当前已经由托管代玩")
		}
		r.delegationEvent(c.Actor, "manual", true)
	case "reclaim":
		if !m.delegated {
			return View{}, fail("NOT_DELEGATED", "当前由你本人操作，无需接管")
		}
		if c.Version < m.delegationVersion {
			return View{}, fail("STALE_STATE", "这次接管属于更早的托管，请重新确认")
		}
		r.delegationEvent(c.Actor, "", false)
	case "auto_delegate_on", "auto_delegate_off":
		m.autoDelegate = c.Action == "auto_delegate_on"
		m.offlineSince = time.Time{}
		r.members[c.Actor] = m
		text := r.state.Players[m.index].Name + "已关闭本局断线托管，断线后保留席位等待。"
		if m.autoDelegate {
			text = r.state.Players[m.index].Name + "已允许本局断线托管，持续断线经过等待期后才会代玩。"
		}
		r.state.Version++
		r.state.History = append(r.state.History, game.Event{ID: game.ID(), Kind: "delegation_policy", Text: text, Player: r.state.Players[m.index].ID})
		if len(r.state.History) > 24 {
			r.state.History = r.state.History[len(r.state.History)-24:]
		}
		r.invalidateCoach()
	default:
		return View{}, fail("INVALID_ACTION", "不支持这个托管操作")
	}
	r.updateDelegationPresence(time.Now())
	return r.finish(&c, saveChange{})
}
func (r *Room) refreshDelegationAt(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	changed := r.updateDelegationPresence(now)
	activated := false
	waiting := false
	for session, m := range r.members {
		waiting = waiting || !m.offlineSince.IsZero()
		if !m.offlineSince.IsZero() && !now.Before(m.offlineSince.Add(r.delegationWait())) {
			r.delegationEvent(session, "offline", true)
			activated = true
		}
	}
	if activated {
		if _, err := r.finish(nil, saveChange{}); err != nil {
			r.restore(before)
			r.publish()
		}
	} else if changed || waiting {
		r.publish()
	}
}
