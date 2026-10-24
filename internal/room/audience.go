package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zkevindev/flytogether/internal/game"
)

// Audience is the public watch channel. Seat invites, cookies and action tokens never appear here.
type Audience struct {
	Enabled     bool          `json:"enabled"`
	Token       string        `json:"token,omitempty"`
	Count       int           `json:"count"`
	Online      int           `json:"online"`
	Capacity    int           `json:"capacity"`
	ChatEnabled bool          `json:"chat_enabled"`
	Me          *AudienceMe   `json:"me,omitempty"`
	Messages    []ChatMessage `json:"messages,omitempty"`
}

type AudienceMe struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type ChatMessage struct {
	ID             string    `json:"id"`
	At             time.Time `json:"at"`
	FromID         string    `json:"from_id"`
	Name           string    `json:"name"`
	Role           string    `json:"role"`
	Kind           string    `json:"kind"`
	Emoji          string    `json:"emoji,omitempty"`
	Text           string    `json:"text,omitempty"`
	TargetPlayerID string    `json:"target_player_id,omitempty"`
}

type spectator struct {
	Name     string
	PublicID string
	Joined   time.Time
	Blocked  []string
}

type savedSpectator struct {
	Name     string    `json:"name"`
	PublicID string    `json:"public_id"`
	Joined   time.Time `json:"joined"`
	Blocked  []string  `json:"blocked,omitempty"`
}

var cheerEmoji = map[string]bool{"💪": true, "👏": true, "✨": true, "🔥": true, "💛": true, "😄": true}

func copyLeases(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func audiencePublicID(session string) string {
	sum := sha256.Sum256([]byte("fly-audience-v1:" + session))
	return "w" + hex.EncodeToString(sum[:6])
}

func validDisplayName(name string) bool {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 10 {
		return false
	}
	for _, c := range name {
		if c < 32 {
			return false
		}
	}
	return true
}

func validPublicID(id string) bool {
	if strings.HasPrefix(id, "player-") && len(id) == len("player-0") && id[len(id)-1] >= '0' && id[len(id)-1] <= '3' {
		return true
	}
	if len(id) != 13 || !strings.HasPrefix(id, "w") {
		return false
	}
	for _, c := range id[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func sanitizeIDs(ids []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validPublicID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func sanitizeChat(in []ChatMessage) []ChatMessage {
	out := []ChatMessage{}
	seen := map[string]bool{}
	for _, m := range in {
		if m.ID == "" || seen[m.ID] || len(m.ID) > 80 || !validPublicID(m.FromID) || !validDisplayName(m.Name) {
			continue
		}
		if m.Role != "player" && m.Role != "host" && m.Role != "spectator" {
			continue
		}
		if m.Kind == "emoji" {
			if !cheerEmoji[m.Emoji] {
				continue
			}
			m.Text = ""
		} else if m.Kind == "text" {
			text, ok := cleanChatText(m.Text)
			if !ok {
				continue
			}
			m.Text, m.Emoji = text, ""
		} else {
			continue
		}
		if m.TargetPlayerID != "" && !strings.HasPrefix(m.TargetPlayerID, "player-") {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return out
}

func cleanChatText(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > 40 {
		return "", false
	}
	for _, c := range text {
		if c < 32 || c == '<' || c == '>' || c == '&' {
			return "", false
		}
	}
	return text, true
}

func (r *Room) isSpectator(session string) bool {
	_, ok := r.spectators[session]
	return ok
}

func (r *Room) HasAccess(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isMember(session) || r.isSpectator(session)
}

func (r *Room) spectatorCap() int {
	if r.spectatorLimit > 0 {
		return r.spectatorLimit
	}
	return 8
}

func (r *Room) joinLimit() int {
	if r.joinRate > 0 {
		return r.joinRate
	}
	return 20
}

func (r *Room) spectatorsOnline() int {
	seen := map[string]bool{}
	for _, session := range r.listeners {
		if r.isSpectator(session) {
			seen[session] = true
		}
	}
	return len(seen)
}

func (r *Room) audienceBody(session string, spectator bool) *Audience {
	if r.invite == "" {
		return nil
	}
	body := &Audience{Enabled: r.spectateOn, Count: len(r.spectators), Online: r.spectatorsOnline(), Capacity: r.spectatorCap(), ChatEnabled: r.chatOn, Messages: r.visibleChat(session)}
	if !spectator && session == r.owner && r.spectateToken != "" {
		body.Token = r.spectateToken
	}
	if sp, ok := r.spectators[session]; ok {
		body.Me = &AudienceMe{ID: sp.PublicID, Name: sp.Name, Role: "spectator"}
	}
	return body
}

func (r *Room) audienceView(session string) View {
	v := r.view()
	v.Voice = "disconnected"
	v.ProfileIDs = nil
	v.Coach = nil
	v.CoachExpired = false
	v.Me = nil
	if v.Network != nil {
		v.Network.Invite = ""
		v.Network.CanClaimHost = false
		v.Network.HostClaimAt = nil
		v.Network.HostClaimWaitSeconds = 0
	}
	v.Audience = r.audienceBody(session, true)
	return v
}

func (r *Room) blockedSet(session string) map[string]bool {
	out := map[string]bool{}
	if sp, ok := r.spectators[session]; ok {
		for _, id := range sp.Blocked {
			out[id] = true
		}
	}
	if m, ok := r.members[session]; ok {
		for _, id := range m.blocked {
			out[id] = true
		}
	}
	return out
}

func (r *Room) visibleChat(session string) []ChatMessage {
	blocked := r.blockedSet(session)
	out := make([]ChatMessage, 0, len(r.chat))
	for _, m := range r.chat {
		if blocked[m.FromID] {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (r *Room) queueNotice(session, kind, message string) {
	payload, _ := json.Marshal(map[string]string{"type": kind, "message": message})
	for ch, owner := range r.listeners {
		if session != "" && owner != session {
			continue
		}
		if session == "" && !r.isSpectator(owner) {
			continue
		}
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- payload:
		default:
		}
	}
}

func (r *Room) revokeSpectatorsLocked(message string) {
	r.queueNotice("", "audience_closed", message)
	r.spectators = map[string]spectator{}
	r.spectatorLeases = map[string]string{}
}

func (s *Store) SetAudienceLimits(capacity, joinsPerMinute int) {
	if capacity < 1 {
		capacity = 8
	}
	if joinsPerMinute < 1 {
		joinsPerMinute = 20
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spectatorCapacity = capacity
	s.spectatorJoinRate = joinsPerMinute
	for _, r := range s.rooms {
		r.mu.Lock()
		r.spectatorLimit = capacity
		r.joinRate = joinsPerMinute
		r.mu.Unlock()
	}
}

func (s *Store) AudienceRoom(session, id string) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session == "" {
		return nil
	}
	if id == "" {
		id = s.audiences[session]
	}
	r := s.rooms[id]
	if r == nil || !r.HasSpectator(session) {
		return nil
	}
	return r
}

func (r *Room) HasSpectator(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isSpectator(session)
}

func (s *Store) Watch(session, id, token, name string) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[id]
	if r == nil {
		return nil, fail("INVALID_WATCH", "观赛链接已失效，请向房主获取新链接")
	}
	if err := r.watch(session, token, name); err != nil {
		return nil, err
	}
	s.audiences[session] = id
	return r, nil
}

func (r *Room) watch(session, token, name string) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	defer r.rollback(before, &err)
	if r.invite == "" || r.status == "ended" {
		return fail("INVALID_WATCH", "观赛链接已失效，请向房主获取新链接")
	}
	if r.isMember(session) {
		return fail("SEAT_KEPT", "你已在这局入座，观赛链接不会改变席位")
	}
	if sp, ok := r.spectators[session]; ok {
		if sp.PublicID == "" {
			sp.PublicID = audiencePublicID(session)
			r.spectators[session] = sp
		}
		_, err = r.finish(nil, saveChange{})
		return err
	}
	if !r.spectateOn || token == "" || token != r.spectateToken {
		if !r.spectateOn && token != "" && token == r.spectateToken {
			return fail("SPECTATE_CLOSED", "房主已关闭观赛")
		}
		return fail("INVALID_WATCH", "观赛链接已失效，请向房主获取新链接")
	}
	name = strings.TrimSpace(name)
	if !validDisplayName(name) {
		return fail("INVALID_NAME", "昵称需要 1 到 10 个字")
	}
	for _, player := range r.state.Players {
		if player.Name == name {
			return fail("NAME_TAKEN", "这个昵称已被选手使用，请换一个")
		}
	}
	for _, sp := range r.spectators {
		if sp.Name == name {
			return fail("NAME_TAKEN", "这个昵称已被其他观众使用，请换一个")
		}
	}
	if len(r.spectators) >= r.spectatorCap() {
		return fail("AUDIENCE_FULL", "观赛人数已满，请稍后再试")
	}
	now := time.Now()
	previous := append([]time.Time(nil), r.joinAt...)
	fresh := []time.Time{}
	for _, at := range previous {
		if now.Sub(at) < time.Minute {
			fresh = append(fresh, at)
		}
	}
	r.joinAt = fresh
	if len(r.joinAt) >= r.joinLimit() {
		return fail("AUDIENCE_RATE", "加入观赛太频繁，请稍后再试")
	}
	r.spectators[session] = spectator{Name: name, PublicID: audiencePublicID(session), Joined: now.UTC()}
	if _, err = r.finish(nil, saveChange{}); err != nil {
		return err
	}
	r.joinAt = append(r.joinAt, now)
	return nil
}

func (s *Store) LeaveAudience(session, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[id]
	if r == nil {
		return fail("NOT_AUTHORIZED", "房间已过期")
	}
	if err := r.leaveAudience(session); err != nil {
		return err
	}
	if s.audiences[session] == id {
		delete(s.audiences, session)
	}
	return nil
}

func (r *Room) leaveAudience(session string) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	defer r.rollback(before, &err)
	if !r.isSpectator(session) {
		return fail("NOT_AUTHORIZED", "你不在观赛名单中")
	}
	r.queueNotice(session, "audience_closed", "你已退出观赛")
	delete(r.spectators, session)
	delete(r.spectatorLeases, session)
	_, err = r.finish(nil, saveChange{})
	return err
}

func (r *Room) ConfigureAudience(actor string, enabled *bool, rotate bool, chat *bool) (result View, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	defer r.rollback(before, &err)
	if err = r.actorOK(actor, true); err != nil {
		return View{}, err
	}
	if r.invite == "" {
		return View{}, fail("NETWORK_SETTINGS", "同屏房间不开放观赛链接")
	}
	if enabled != nil {
		r.spectateOn = *enabled
		if r.spectateOn && r.spectateToken == "" {
			r.spectateToken = game.ID()
		}
		if !r.spectateOn {
			r.revokeSpectatorsLocked("房主已关闭观赛，棋局不受影响")
		}
	}
	if rotate {
		r.spectateToken = game.ID()
	}
	if chat != nil {
		r.chatOn = *chat
	}
	if _, err = r.finish(nil, saveChange{}); err != nil {
		return View{}, err
	}
	return r.personalized(actor), nil
}

func (s *Store) ConfigureAudience(session, id string, enabled *bool, rotate bool, chat *bool) (View, error) {
	s.mu.Lock()
	r := s.rooms[id]
	s.mu.Unlock()
	if r == nil || !r.HasMember(session) {
		return View{}, fail("NOT_AUTHORIZED", "请先入座")
	}
	view, err := r.ConfigureAudience(session, enabled, rotate, chat)
	if err != nil {
		return View{}, err
	}
	if enabled != nil && !*enabled {
		s.mu.Lock()
		for key, rid := range s.audiences {
			if rid == id {
				delete(s.audiences, key)
			}
		}
		s.mu.Unlock()
	}
	return view, nil
}

type ChatInput struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Emoji          string `json:"emoji"`
	Text           string `json:"text"`
	TargetPlayerID string `json:"target_player_id"`
}

func (s *Store) PostChat(session, id string, in ChatInput) (View, error) {
	s.mu.Lock()
	r := s.rooms[id]
	s.mu.Unlock()
	if r == nil {
		return View{}, fail("NOT_AUTHORIZED", "房间已过期")
	}
	return r.postChat(session, in)
}

func (r *Room) postChat(session string, in ChatInput) (result View, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	defer r.rollback(before, &err)
	msg, err := r.chatMessage(session, in)
	if err != nil {
		return View{}, err
	}
	for _, old := range r.chat {
		if old.ID == msg.ID {
			return r.personalized(session), nil
		}
	}
	now := time.Now()
	fresh := []time.Time{}
	for _, at := range r.chatAt[session] {
		if now.Sub(at) < 10*time.Second {
			fresh = append(fresh, at)
		}
	}
	if len(fresh) >= 5 {
		return View{}, fail("CHAT_RATE", "发送太频繁，请稍等几秒")
	}
	r.chat = append(r.chat, msg)
	if len(r.chat) > 40 {
		r.chat = r.chat[len(r.chat)-40:]
	}
	if _, err = r.finish(nil, saveChange{}); err != nil {
		return View{}, err
	}
	if r.chatAt == nil {
		r.chatAt = map[string][]time.Time{}
	}
	r.chatAt[session] = append(fresh, now)
	return r.personalized(session), nil
}

func (r *Room) chatMessage(session string, in ChatInput) (ChatMessage, error) {
	if r.invite == "" || r.status == "ended" {
		return ChatMessage{}, fail("NOT_AUTHORIZED", "这个房间不能发送互动")
	}
	msg := ChatMessage{ID: strings.TrimSpace(in.ID), At: time.Now().UTC(), Kind: in.Kind, TargetPlayerID: strings.TrimSpace(in.TargetPlayerID)}
	if msg.ID == "" {
		msg.ID = game.ID()
	}
	if len(msg.ID) > 80 {
		return ChatMessage{}, fail("INVALID_CHAT", "消息编号无效")
	}
	if msg.TargetPlayerID != "" {
		found := false
		for _, player := range r.state.Players {
			if player.ID == msg.TargetPlayerID && !player.AI {
				found = true
			}
		}
		if !found {
			return ChatMessage{}, fail("INVALID_CHAT", "请选择正在比赛的选手")
		}
	}
	if in.Kind == "emoji" {
		if !cheerEmoji[in.Emoji] {
			return ChatMessage{}, fail("INVALID_CHAT", "请使用加油、鼓掌或漂亮等快捷表情")
		}
		msg.Emoji = in.Emoji
	} else if in.Kind == "text" {
		text, ok := cleanChatText(in.Text)
		if !ok {
			return ChatMessage{}, fail("INVALID_CHAT", "文字需要 1 到 40 个字，且不能包含标记符号")
		}
		msg.Text = text
	} else {
		return ChatMessage{}, fail("INVALID_CHAT", "互动只支持快捷表情或简短文字")
	}
	if sp, ok := r.spectators[session]; ok {
		if !r.chatOn {
			return ChatMessage{}, fail("CHAT_DISABLED", "房主已关闭观众发言，仍可安静观赛和询问自己的灵矽")
		}
		msg.FromID, msg.Name, msg.Role = sp.PublicID, sp.Name, "spectator"
		return msg, nil
	}
	m, ok := r.members[session]
	if !ok {
		return ChatMessage{}, fail("NOT_AUTHORIZED", "请先入座或加入观赛")
	}
	msg.FromID = r.state.Players[m.index].ID
	msg.Name = r.state.Players[m.index].Name
	msg.Role = "player"
	if session == r.owner {
		msg.Role = "host"
	}
	return msg, nil
}

func (r *Room) BlockChat(session, fromID string, blocked bool) (result View, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.beforeMutation()
	defer r.rollback(before, &err)
	if !validPublicID(fromID) {
		return View{}, fail("INVALID_CHAT", "无法识别要屏蔽的发言人")
	}
	var list []string
	switch {
	case r.isSpectator(session):
		list = r.spectators[session].Blocked
	case r.isMember(session):
		list = r.members[session].blocked
	default:
		return View{}, fail("NOT_AUTHORIZED", "请先入座或加入观赛")
	}
	next := []string{}
	for _, id := range list {
		if id != fromID {
			next = append(next, id)
		}
	}
	if blocked {
		next = append(next, fromID)
	}
	next = sanitizeIDs(next)
	if sp, ok := r.spectators[session]; ok {
		sp.Blocked = next
		r.spectators[session] = sp
	}
	if m, ok := r.members[session]; ok {
		m.blocked = next
		r.members[session] = m
	}
	if _, err = r.finish(nil, saveChange{}); err != nil {
		return View{}, err
	}
	return r.personalized(session), nil
}

// SpectatorVoice is an independent read-only Realtime session.
// It never takes the player intercom lease or publishes room voice state.
type SpectatorVoice struct {
	r            *Room
	actor, lease string
	gameID       string
}

func (r *Room) VoiceForSpectator(session string) (*SpectatorVoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.spectateOn || !r.isSpectator(session) {
		return nil, fail("SPECTATE_CLOSED", "观赛助手已关闭，仍可安静观看棋局")
	}
	if r.status == "ended" {
		return nil, fail("GAME_NOT_ACTIVE", "房间已结束")
	}
	if r.spectatorLeases == nil {
		r.spectatorLeases = map[string]string{}
	}
	if r.spectatorLeases[session] != "" {
		return nil, fail("VOICE_BUSY", "这个观众已有灵矽连接，请先关闭原来的提问")
	}
	lease := game.ID()
	r.spectatorLeases[session] = lease
	return &SpectatorVoice{r: r, actor: session, lease: lease, gameID: r.state.ID}, nil
}

func (v *SpectatorVoice) SpectatorVoice() bool { return true }
func (v *SpectatorVoice) Close() {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	if v.r.spectatorLeases[v.actor] == v.lease {
		delete(v.r.spectatorLeases, v.actor)
	}
}
func (v *SpectatorVoice) Snapshot() View { return v.r.SnapshotFor(v.actor) }
func (v *SpectatorVoice) VoiceSettings() VoiceSettings {
	settings := v.r.VoiceSettings()
	settings.WakeEnabled = false
	return settings
}
func (v *SpectatorVoice) BeginSpeech() error {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	return v.validLocked()
}
func (v *SpectatorVoice) validLocked() error {
	if !v.r.spectateOn || !v.r.isSpectator(v.actor) {
		return fail("SPECTATE_CLOSED", "观赛助手已关闭，仍可刷新查看棋局")
	}
	if v.r.status == "ended" {
		return fail("GAME_NOT_ACTIVE", "这一局已经结束，可以看结果，不能再提问")
	}
	if v.r.spectatorLeases[v.actor] != v.lease || v.r.state.ID != v.gameID {
		return fail("STALE_ACTION", "已经换了一局或连接已失效，请重新连接观赛助手")
	}
	return nil
}
func (v *SpectatorVoice) CurrentActionToken() string { return "" }
func (v *SpectatorVoice) VoiceState(string)          {}
func (v *SpectatorVoice) Tool(name string, _ map[string]string) (any, error) {
	if name != "get_game_state" && name != "get_legal_moves" {
		return nil, fail("NOT_AUTHORIZED", "观众不能操作棋局，也不会把这句话转成选手指令")
	}
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	if err := v.validLocked(); err != nil {
		return nil, err
	}
	recent := []string{}
	for _, event := range v.r.state.History {
		if event.Text != "" {
			recent = append(recent, event.Text)
		}
	}
	if len(recent) > 8 {
		recent = recent[len(recent)-8:]
	}
	standings := make([]map[string]any, 0, len(v.r.state.Players))
	for _, player := range v.r.state.Players {
		arrived, furthest := 0, -1
		for _, piece := range player.Pieces {
			if piece.Progress >= v.r.state.Board.Finish {
				arrived++
			}
			if piece.Progress > furthest {
				furthest = piece.Progress
			}
		}
		standings = append(standings, map[string]any{"name": player.Name, "arrived": arrived, "furthest": furthest})
	}
	view := v.r.audienceView(v.actor)
	if view.Audience != nil {
		trimmed := *view.Audience
		trimmed.Messages = nil
		trimmed.Token = ""
		view.Audience = &trimmed
	}
	current := v.r.state.Players[v.r.state.Current]
	return map[string]any{
		"room": view, "current_player": current, "legal_moves": v.r.state.Legal,
		"standings": standings, "recent_events": recent, "game_id": v.r.state.ID, "version": v.r.state.Version,
		"rules_hint":  v.r.state.Rules().Summary + " furthest 越大越接近终点，-1 表示还在机库。只根据这次查询回答。recent_events 没有的过去事件要说明没有记录，不要编造点数或胜负。互动消息不在这次结果里，不能当成棋步。不能声称已经替选手执行了操作。",
		"instruction": "这是观众的私人只读问答。不要返回或索要操作授权。",
	}, nil
}
