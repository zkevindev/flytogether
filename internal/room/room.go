package room

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zkevindev/flytogether/internal/game"
)

type View struct {
	Network       *Lobby          `json:"network,omitempty"`
	Me            *Me             `json:"me,omitempty"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	Seq           int             `json:"seq"`
	State         json.RawMessage `json:"state"`
	Voice         string          `json:"voice"`
	VoiceSettings VoiceSettings   `json:"voice_settings"`
	Coach         *Coach          `json:"coach,omitempty"`
	CoachExpired  bool            `json:"coach_expired,omitempty"`
}

type VoiceSettings struct {
	WakeEnabled bool   `json:"wake_enabled"`
	WakeWord    string `json:"wake_word"`
}

func DefaultVoiceSettings() VoiceSettings {
	return VoiceSettings{WakeEnabled: false, WakeWord: "小灵同学"}
}

type Command struct {
	Actor   string `json:"-"`
	ID      string `json:"command_id"`
	GameID  string `json:"game_id"`
	Version int    `json:"expected_version"`
	Action  string `json:"action"`
	PieceID string `json:"piece_id,omitempty"`
	PropID  string `json:"prop_id,omitempty"`
	Token   string `json:"action_token,omitempty"`
	Choice  string `json:"choice,omitempty"`
	CoachID string `json:"coach_id,omitempty"`
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string        { return e.Message }
func fail(code, message string) error { return &Error{code, message} }

type cached struct {
	Input  Command
	Output View
}

type Room struct {
	owner         string
	invite        string
	members       map[string]member
	names         map[string]string
	voiceActor    string
	voiceLease    string
	grantActor    string
	options       game.Options
	mu            sync.Mutex
	ID            string
	name          string
	status        string
	state         *game.State
	seq           int
	voice         string
	voiceSettings VoiceSettings
	busy          bool
	grant         string
	grantVersion  int
	grantGame     string
	used          bool
	cache         map[string]cached
	listeners     map[chan []byte]string
	updated       time.Time
	coach         *Coach
	coachPrevious string
	coachExpired  bool
}

func New(o game.Options) *Room {
	status := "playing"
	if o.Waiting {
		status = "waiting"
	}
	name := strings.TrimSpace(o.RoomName)
	if name == "" {
		name = "家庭机场"
	}
	return &Room{options: o, name: name, status: status, ID: game.ID(), state: game.New(o), voice: "disconnected", voiceSettings: DefaultVoiceSettings(), cache: map[string]cached{},
		listeners: map[chan []byte]string{}, updated: time.Now()}
}

func (r *Room) view() View {
	b, _ := json.Marshal(r.state)
	return View{ID: r.ID, Name: r.name, Status: r.status, Seq: r.seq, State: b, Voice: r.voice, VoiceSettings: r.voiceSettings, Coach: r.coach, CoachExpired: r.coachExpired, Network: r.lobbyView()}
}

func (r *Room) VoiceSettings() VoiceSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.voiceSettings
}

func (r *Room) SetVoiceSettings(settings VoiceSettings) (View, error) {
	settings.WakeWord = strings.TrimSpace(settings.WakeWord)
	if count := utf8.RuneCountInString(settings.WakeWord); count < 2 || count > 16 {
		return View{}, fail("INVALID_WAKE_WORD", "唤醒词需要 2 到 16 个字")
	}
	for _, c := range settings.WakeWord {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != ' ' {
			return View{}, fail("INVALID_WAKE_WORD", "唤醒词请使用中文、字母或数字")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidateCoach()
	r.voiceSettings = settings
	r.grant, r.busy = "", false
	r.publish()
	return r.view(), nil
}
func (r *Room) Snapshot() View { r.mu.Lock(); defer r.mu.Unlock(); return r.view() }
func (r *Room) publish() {
	r.seq++
	r.updated = time.Now()
	for ch, session := range r.listeners {
		b, _ := json.Marshal(map[string]any{"type": "state", "room": r.personalized(session)})
		select {
		case ch <- b:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- b:
			default:
			}
		}
	}
}
func (r *Room) Subscribe() (chan []byte, func()) { return r.SubscribeFor("") }
func (r *Room) SubscribeFor(session string) (chan []byte, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan []byte, 1)
	r.listeners[ch] = session
	r.publish()
	return ch, func() { r.mu.Lock(); delete(r.listeners, ch); r.publish(); r.mu.Unlock() }
}

func (r *Room) Restart(o game.Options) (View, error) { return r.RestartAs(o, "") }
func (r *Room) RestartAs(o game.Options, actor string) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.actorOK(actor, true); err != nil {
		return View{}, err
	}
	if r.invite != "" {
		return View{}, fail("NETWORK_SETTINGS", "联网房间请使用同配置再玩，调整设置请创建新房间")
	}
	if r.busy {
		return View{}, fail("VOICE_BUSY", "请等这次语音操作结束，再开始新游戏")
	}
	r.invalidateCoach()
	r.options = o
	r.state = game.New(o)
	r.status = "playing"
	if o.Waiting {
		r.status = "waiting"
	}
	if strings.TrimSpace(o.RoomName) != "" {
		r.name = strings.TrimSpace(o.RoomName)
	}
	r.voice = "disconnected"
	r.cache = map[string]cached{}
	r.grant = ""
	r.used = false
	r.publish()
	return r.view(), nil
}

func (r *Room) DoAs(c Command, actor string) (View, error) { c.Actor = actor; return r.Do(c, false) }
func (r *Room) Do(c Command, voice bool) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invite != "" {
		host := c.Action == "start" || c.Action == "pause" || c.Action == "resume" || c.Action == "end" || c.Action == "replay"
		// Retried commands remain bound to the original actor, even after the turn advanced.
		if !r.isMember(c.Actor) {
			return View{}, fail("NOT_AUTHORIZED", "请先入座")
		}
		if host && c.Actor != r.owner {
			return View{}, fail("HOST_ONLY", "这个操作需要房主来完成")
		}
	}
	if old, ok := r.cache[c.ID]; ok {
		if old.Input != c {
			return View{}, fail("COMMAND_CONFLICT", "同一个操作编号不能重复用于不同动作")
		}
		v := old.Output
		if r.invite != "" {
			v = r.personalized(c.Actor)
		}
		return v, nil
	}
	if c.ID == "" || len(c.ID) > 160 {
		return View{}, fail("INVALID_COMMAND", "操作编号无效")
	}
	if c.GameID != r.state.ID || (c.Version != r.state.Version && c.Action != "pause" && c.Action != "end") {
		return View{}, fail("STALE_STATE", "棋局已经更新，请重新选择")
	}
	if c.Action == "replay" {
		if voice || r.status == "playing" && r.state.Phase != "finished" {
			return View{}, fail("INVALID_TRANSITION", "请结束当前对局后再玩")
		}
		o := r.options
		o.Demo = false
		o.Waiting = r.invite != ""
		r.state = game.New(o)
		if r.invite != "" {
			for session, m := range r.members {
				r.state.Players[m.index].Name = r.names[session]
				m.ready = session == r.owner
				r.members[session] = m
			}
			r.status = "waiting"
		} else {
			r.status = "playing"
		}
		r.invalidateCoach()
		r.grant = ""
		r.busy = false
		r.voice = "disconnected"
		r.voiceLease = ""
		r.voiceActor = ""
		r.publish()
		v := r.personalized(c.Actor)
		r.cache = map[string]cached{c.ID: {c, v}}
		return v, nil
	}
	if c.Action == "start" || c.Action == "pause" || c.Action == "resume" || c.Action == "end" {
		if voice {
			return View{}, fail("INVALID_ACTION", "请通过房间按钮控制游戏")
		}
		if c.Action == "start" {
			if err := r.readyToStart(); err != nil {
				return View{}, err
			}
		}
		if err := r.control(c.Action); err != nil {
			return View{}, err
		}
		r.invalidateCoach()
		r.publish()
		v := r.personalized(c.Actor)
		r.cache[c.ID] = cached{c, v}
		return v, nil
	}
	if err := r.actorOK(c.Actor, false); err != nil {
		return View{}, err
	}
	if r.status != "playing" {
		return View{}, fail("GAME_NOT_ACTIVE", "请先开始或继续游戏，当前不能操作棋局")
	}
	if r.state.Phase == "finished" {
		return View{}, fail("GAME_FINISHED", "这局已经结束，来一局新的吧")
	}
	if r.state.Players[r.state.Current].AI {
		return View{}, fail("AI_TURN", "轮到灵矽了，请稍等一下")
	}
	if !voice && r.busy {
		return View{}, fail("VOICE_BUSY", "正在处理这次语音，请稍等")
	}
	if voice && (c.Token == "" || c.Token != r.grant || r.used || r.grantGame != r.state.ID || r.grantVersion != r.state.Version || r.grantActor != c.Actor) {
		return View{}, fail("STALE_ACTION", "请重新查看棋局，再说一次操作")
	}
	if len(r.cache) >= 4096 {
		return View{}, fail("ROOM_LIMIT", "这局操作次数较多，请开始新的一局")
	}
	var err error
	switch c.Action {
	case "coach":
		err = r.propose(c.Choice)
	case "confirm_coach":
		if r.coach == nil || c.CoachID == "" || r.coach.ID != c.CoachID || r.coach.GameID != r.state.ID || r.coach.Version != r.state.Version || r.coach.PlayerID != r.state.Players[r.state.Current].ID {
			return View{}, fail("STALE_COACH", "建议已经变化，请重新点或说“帮我选”")
		}
		err = r.state.Move(r.coach.Move.PieceID)
	case "roll":
		err = r.state.RollRandom()
	case "prop":
		err = r.state.UsePropOn(c.PropID, c.PieceID)
		// The lucky dice is the throw itself, not a card waiting for another
		// command. Commit both real rolls under this lock and idempotency key.
		if err == nil && c.PropID == "luck" {
			err = r.state.RollRandom()
		}
	case "move":
		err = r.state.Move(c.PieceID)
	default:
		return View{}, fail("INVALID_ACTION", "不支持这个操作")
	}
	if err != nil {
		return View{}, fail("ILLEGAL_MOVE", err.Error())
	}
	if r.invite != "" && r.members[c.Actor].index != r.state.Current {
		r.voiceLease = ""
		r.voiceActor = ""
		r.busy = false
		r.grant = ""
		r.voice = "disconnected"
	}
	if c.Action != "coach" {
		r.invalidateCoach()
		r.coachExpired = false
	}
	if voice {
		r.used = true
	}
	r.publish()
	v := r.personalized(c.Actor)
	r.cache[c.ID] = cached{c, v}
	return v, nil
}

func (r *Room) AI() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != "playing" || r.busy || r.state.Phase == "finished" || !r.state.Players[r.state.Current].AI {
		return
	}
	if r.state.Phase == "roll" {
		prop, target := "", ""
		if r.state.Difficulty == "hard" {
			prop, target = r.state.SuggestedPropTarget()
		} else if r.state.Difficulty != "easy" {
			prop = r.state.SupportProp()
		}
		if prop != "" {
			_ = r.state.UsePropOn(prop, target)
			if prop == "luck" {
				_ = r.state.RollRandom()
			}
		} else {
			_ = r.state.RollRandom()
		}
	} else {
		_ = r.state.Move(r.state.AIMove().PieceID)
	}
	r.publish()
}

func (r *Room) BeginSpeech() error { return r.beginSpeech("", "") }
func (r *Room) beginSpeech(actor, lease string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.actorOK(actor, false); err != nil {
		return err
	}
	if lease != "" && r.voiceLease != lease {
		return fail("STALE_ACTION", "语音连接已过期")
	}
	if r.status != "playing" || r.state.Phase == "finished" {
		return fail("GAME_NOT_ACTIVE", "请先开始或继续游戏，再开启语音")
	}
	if r.busy && r.voice == "listening" {
		return fail("VOICE_BUSY", "上一句话还在处理，请稍等")
	}
	r.busy = true
	r.voice = "listening"
	r.grant = game.ID()
	r.grantActor = actor
	r.used = false
	r.grantGame = r.state.ID
	r.grantVersion = r.state.Version
	r.publish()
	return nil
}

func (r *Room) CurrentActionToken() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != "playing" || !r.busy || r.used || r.grantGame != r.state.ID || r.grantVersion != r.state.Version {
		return ""
	}
	return r.grant
}

func (r *Room) VoiceState(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.voiceState(state)
}
func (r *Room) voiceState(state string) {
	if r.status != "playing" {
		state = "disconnected"
	}
	r.voice = state
	if state == "disconnected" || state == "error" {
		r.invalidateCoach()
	}
	if state == "ready" || state == "standby" || state == "disconnected" || state == "error" {
		r.busy = false
		r.grant = ""
	}
	r.publish()
}

func (r *Room) Tool(name string, args map[string]string) (any, error) { return r.tool(name, args, "") }
func (r *Room) tool(name string, args map[string]string, actor string) (any, error) {
	if name == "get_game_state" || name == "get_legal_moves" {
		r.mu.Lock()
		defer r.mu.Unlock()
		token := ""
		if r.status == "playing" && r.busy && !r.used && r.grantActor == actor && r.grantGame == r.state.ID && r.grantVersion == r.state.Version {
			token = r.grant
		}
		suggested, target := r.state.SuggestedPropTarget()
		reason := "经典规则需六点起飞，启用本回合起飞卡后可用任意点数起飞；加速卡对已离开机库的飞机增加两步；使用幸运骰会立即掷两次取较大值，无需再单独掷骰。每次掷骰前最多用一张已有道具，不能通过语音指定点数。炸弹立即送回对方公共航线上的一架指定飞机，护盾抵挡一次炸弹或撞机，两者都需指定piece_id且不能选机库、起飞位或终点航道；一次掷骰前仍只能用一张。超过终点不能移动，请只选 legal_moves 中的棋子。"
		voiceView := r.view()
		voiceView.Network = nil
		voiceView.Me = nil
		return map[string]any{"room": voiceView, "action_token": token, "rules_hint": reason,
			"prop_targets": map[string][]string{"bomb": r.state.PropTargets("bomb"), "shield": r.state.PropTargets("shield")}, "suggested_prop": map[string]string{"prop_id": suggested, "piece_id": target},
			"current_player": r.state.Players[r.state.Current], "legal_moves": r.state.Legal}, nil
	}
	if name != "roll_dice" && name != "move_piece" && name != "use_prop" && name != "coach_move" && name != "confirm_coach" {
		return nil, fail("UNKNOWN_TOOL", "未知游戏工具")
	}
	token := args["action_token"]
	r.mu.Lock()
	// Derive identity/version from the grant; a model cannot select another room or game.
	c := Command{Actor: actor, ID: "voice:" + token, GameID: r.grantGame, Version: r.grantVersion, Token: token, Action: "roll"}
	if name == "coach_move" {
		c.Action, c.Choice = "coach", args["choice"]
	}
	if name == "confirm_coach" {
		c.Action, c.CoachID = "confirm_coach", args["coach_id"]
	}
	if name == "move_piece" {
		c.Action = "move"
		c.PieceID = args["piece_id"]
	}
	if name == "use_prop" {
		c.Action = "prop"
		c.PropID = args["prop_id"]
		c.PieceID = args["piece_id"]
	}
	r.mu.Unlock()
	v, err := r.Do(c, true)
	if err != nil {
		return nil, err
	}
	v.Network = nil
	v.Me = nil
	if name == "coach_move" {
		return map[string]any{"room": v, "message": "仅更新建议或取消建议，没有走棋、掷骰或扣道具。下一句话明确确认后才可移动。"}, nil
	}
	return map[string]any{"room": v, "message": "操作已执行。以返回棋局为准，简短说明结果。本句话不要再执行第二次游戏操作。"}, nil
}

type Store struct {
	mu       sync.Mutex
	rooms    map[string]*Room
	sessions map[string]string
}

func NewStore() *Store { return &Store{rooms: map[string]*Room{}, sessions: map[string]string{}} }
func (s *Store) Create(session string, o game.Options) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.rooms[s.sessions[session]]; old != nil && old.HasMember(session) {
		return old, nil
	}
	if len(s.rooms) >= 256 {
		return nil, fmt.Errorf("房间已满，请稍后再试")
	}
	r := New(o)
	r.initOwner(session, o.Network)
	s.rooms[r.ID] = r
	s.sessions[session] = r.ID
	return r, nil
}
func (s *Store) Get(session, id string) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session == "" {
		return nil
	}
	if id == "" {
		id = s.sessions[session]
	}
	r := s.rooms[id]
	if r == nil || !r.HasMember(session) {
		return nil
	}
	return r
}
func (s *Store) Run(ctxDone <-chan struct{}) {
	t := time.NewTicker(1200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case <-t.C:
			s.mu.Lock()
			all := make([]*Room, 0, len(s.rooms))
			for id, r := range s.rooms {
				r.mu.Lock()
				expired := time.Since(r.updated) > 4*time.Hour && len(r.listeners) == 0 && !r.busy
				r.mu.Unlock()
				if expired {
					delete(s.rooms, id)
					for key, rid := range s.sessions {
						if rid == id {
							delete(s.sessions, key)
						}
					}
					continue
				}
				all = append(all, r)
			}
			s.mu.Unlock()
			for _, r := range all {
				r.AI()
			}
		}
	}
}

// control runs under the room lock and invalidates every prior speech grant.
func (r *Room) control(action string) error {
	next, text := "", ""
	switch {
	case action == "start" && r.status == "waiting":
		next, text = "playing", "所有飞行员已就位，游戏开始！"
	case action == "pause" && r.status == "playing" && r.state.Phase != "finished":
		next, text = "paused", "游戏已暂停，棋盘和本次点数保留。"
	case action == "resume" && r.status == "paused":
		next, text = "playing", "游戏继续，从刚才的位置出发。"
	case action == "end" && r.status != "ended":
		next, text = "ended", "本局已结束，可以重新准备下一局。"
	default:
		return fail("INVALID_TRANSITION", "当前状态不能执行这个房间操作")
	}
	r.invalidateCoach()
	r.status, r.grant, r.busy, r.voice = next, "", false, "disconnected"
	r.voiceLease = ""
	r.voiceActor = ""
	r.used = false
	r.state.Version++
	r.state.History = append(r.state.History, game.Event{ID: game.ID(), Kind: action, Text: text})
	if len(r.state.History) > 24 {
		r.state.History = r.state.History[len(r.state.History)-24:]
	}
	return nil
}

// CreateNew explicitly replaces this browser's same-screen room.
func (s *Store) CreateNew(session string, o game.Options) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := DefaultVoiceSettings()
	old := s.rooms[s.sessions[session]]
	if (old == nil || old.invite != "") && len(s.rooms) >= 256 {
		return nil, fmt.Errorf("房间已满，请稍后再试")
	}
	if old != nil && old.invite == "" {
		old.mu.Lock()
		settings = old.voiceSettings
		if old.status != "ended" {
			_ = old.control("end")
			old.publish()
		}
		old.mu.Unlock()
		delete(s.rooms, old.ID)
	}
	o.Waiting = true
	r := New(o)
	r.initOwner(session, o.Network)
	r.voiceSettings = settings
	s.rooms[r.ID] = r
	s.sessions[session] = r.ID
	return r, nil
}
