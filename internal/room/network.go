package room

import (
	"strings"
	"unicode/utf8"

	"github.com/zkevindev/flytogether/internal/game"
)

type Seat struct {
	Robot    bool   `json:"robot,omitempty"`
	PlayerID string `json:"player_id"`
	Occupied bool   `json:"occupied"`
	Ready    bool   `json:"ready"`
	Online   bool   `json:"online"`
}
type Lobby struct {
	Invite string `json:"invite"`
	Seats  []Seat `json:"seats"`
}
type Me struct {
	PlayerID string `json:"player_id"`
	Host     bool   `json:"host"`
}
type member struct {
	index int
	ready bool
}

// Credentials and the membership map never enter a room snapshot.
func (r *Room) initOwner(session string, network bool) {
	r.owner = session
	r.names = map[string]string{session: r.state.Players[0].Name}
	if network {
		r.status = "waiting"
		r.invite = game.ID()
		r.members = map[string]member{session: {index: 0, ready: true}}
	}
}
func (r *Room) isMember(session string) bool {
	if r.invite == "" {
		return session != "" && session == r.owner
	}
	_, ok := r.members[session]
	return ok
}
func (r *Room) HasMember(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isMember(session)
}
func (r *Room) personalized(session string) View {
	v := r.view()
	if r.invite != "" {
		if m, ok := r.members[session]; ok {
			v.Me = &Me{PlayerID: r.state.Players[m.index].ID, Host: session == r.owner}
		} else {
			v.Network = nil
		}
	}
	return v
}
func (r *Room) SnapshotFor(session string) View {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.personalized(session)
}
func (r *Room) actorOK(session string, host bool) error {
	if r.invite == "" {
		return nil
	}
	m, ok := r.members[session]
	if !ok {
		return fail("NOT_AUTHORIZED", "请先通过邀请链接入座")
	}
	if host {
		if session != r.owner {
			return fail("HOST_ONLY", "这个操作需要房主来完成")
		}
		return nil
	}
	if m.index != r.state.Current {
		return fail("NOT_YOUR_TURN", "还没轮到你的席位，请等待当前玩家")
	}
	return nil
}
func (r *Room) readyToStart() error {
	if r.invite == "" {
		return nil
	}
	for i, player := range r.state.Players {
		if player.AI {
			continue
		}
		found := false
		for session, m := range r.members {
			if m.index == i {
				found = true
				if !m.ready || !r.online(session) {
					return fail("PLAYERS_NOT_READY", "请等所有玩家入座、在线并准备好")
				}
			}
		}
		if !found {
			return fail("PLAYERS_NOT_READY", "还有空席位，分享邀请链接叫朋友加入吧")
		}
	}
	return nil
}
func (r *Room) online(session string) bool {
	for _, s := range r.listeners {
		if s == session {
			return true
		}
	}
	return false
}
func (r *Room) lobbyView() *Lobby {
	if r.invite == "" {
		return nil
	}
	l := &Lobby{Invite: r.invite, Seats: make([]Seat, len(r.state.Players))}
	for i, p := range r.state.Players {
		l.Seats[i] = Seat{PlayerID: p.ID, Robot: p.AI, Occupied: p.AI, Ready: p.AI, Online: p.AI}
	}
	for session, m := range r.members {
		l.Seats[m.index] = Seat{PlayerID: r.state.Players[m.index].ID, Occupied: true, Ready: m.ready, Online: r.online(session)}
	}
	return l
}
func (r *Room) Join(session, invite, name string) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invite == "" || invite != r.invite || r.status == "ended" {
		return View{}, fail("INVALID_INVITE", "邀请链接已失效，请向房主获取新链接")
	}
	if r.isMember(session) {
		return r.personalized(session), nil
	}
	if r.status != "waiting" {
		return View{}, fail("GAME_STARTED", "游戏已开始，新玩家请等下一局")
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 10 {
		return View{}, fail("INVALID_NAME", "昵称需要 1 到 10 个字")
	}
	for _, c := range name {
		if c < 32 {
			return View{}, fail("INVALID_NAME", "昵称包含不支持的字符")
		}
	}
	used := map[int]bool{}
	for i, player := range r.state.Players {
		if player.AI {
			used[i] = true
			if player.Name == name {
				return View{}, fail("NAME_TAKEN", "这个昵称已被机器人使用，请换一个")
			}
		}
	}
	for _, m := range r.members {
		used[m.index] = true
		if r.state.Players[m.index].Name == name {
			return View{}, fail("NAME_TAKEN", "这个昵称已被使用，请换一个")
		}
	}
	for i := range r.state.Players {
		if !used[i] {
			r.members[session] = member{index: i}
			r.names[session] = name
			r.state.Players[i].Name = name
			r.state.Version++
			r.publish()
			return r.personalized(session), nil
		}
	}
	return View{}, fail("ROOM_FULL", "房间已满，请联系房主")
}
func (r *Room) SetReady(session string, ready bool) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[session]
	if !ok || r.invite == "" {
		return View{}, fail("NOT_AUTHORIZED", "请先入座")
	}
	if r.status != "waiting" {
		return View{}, fail("GAME_STARTED", "只可在准备区修改准备状态")
	}
	if session == r.owner {
		ready = true
	}
	m.ready = ready
	r.members[session] = m
	r.publish()
	return r.personalized(session), nil
}
func (r *Room) Leave(session string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invite == "" || !r.isMember(session) {
		return fail("NOT_AUTHORIZED", "不在此房间中")
	}
	if session == r.owner {
		return fail("HOST_ONLY", "房主请使用结束游戏关闭房间")
	}
	if r.status != "waiting" {
		return fail("GAME_STARTED", "开局后保留席位，可返回首页再重连")
	}
	delete(r.members, session)
	r.state.Version++
	r.publish()
	return nil
}
func (s *Store) Join(session, id, invite, name string) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[id]
	if r == nil {
		return nil, fail("INVALID_INVITE", "房间不存在或已过期")
	}
	if _, e := r.Join(session, invite, name); e != nil {
		return nil, e
	}
	s.sessions[session] = id
	return r, nil
}

// VoiceRoom binds every tool and speech grant to the authenticated browser.
// An old socket cannot clear a newer socket's voice state after a turn changes.
type VoiceRoom struct {
	r            *Room
	actor, lease string
}
type GameSession interface {
	Snapshot() View
	VoiceSettings() VoiceSettings
	BeginSpeech() error
	CurrentActionToken() string
	VoiceState(string)
	Tool(string, map[string]string) (any, error)
}

func (r *Room) VoiceFor(session string) (*VoiceRoom, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.actorOK(session, false); err != nil {
		return nil, err
	}
	if r.status != "playing" || r.state.Phase == "finished" {
		return nil, fail("GAME_NOT_ACTIVE", "请先开始或继续游戏")
	}
	if r.voiceLease != "" {
		return nil, fail("VOICE_BUSY", "已有对讲连接，请先关闭原设备的麦克风")
	}
	lease := game.ID()
	r.voiceLease = lease
	r.voiceActor = session
	return &VoiceRoom{r, session, lease}, nil
}
func (v *VoiceRoom) Close() {
	v.VoiceState("disconnected")
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	if v.r.voiceLease == v.lease {
		v.r.voiceLease = ""
		v.r.voiceActor = ""
	}
}
func (v *VoiceRoom) Snapshot() View               { return v.r.SnapshotFor(v.actor) }
func (v *VoiceRoom) VoiceSettings() VoiceSettings { return v.r.VoiceSettings() }
func (v *VoiceRoom) BeginSpeech() error           { return v.r.beginSpeech(v.actor, v.lease) }
func (v *VoiceRoom) CurrentActionToken() string {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	if v.r.voiceLease != v.lease || v.r.grantActor != v.actor || v.r.used {
		return ""
	}
	return v.r.grant
}
func (v *VoiceRoom) VoiceState(state string) {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	if v.r.voiceLease == v.lease {
		v.r.voiceState(state)
	}
}
func (v *VoiceRoom) Tool(name string, args map[string]string) (any, error) {
	return v.r.tool(name, args, v.actor)
}

func (s *Store) Leave(session, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[id]
	if r == nil {
		return fail("NOT_AUTHORIZED", "房间已过期")
	}
	if err := r.Leave(session); err != nil {
		return err
	}
	if s.sessions[session] == id {
		delete(s.sessions, session)
	}
	return nil
}
