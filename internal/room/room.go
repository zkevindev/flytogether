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
	ID            string          `json:"id"`
	Seq           int             `json:"seq"`
	State         json.RawMessage `json:"state"`
	Voice         string          `json:"voice"`
	VoiceSettings VoiceSettings   `json:"voice_settings"`
}

type VoiceSettings struct {
	WakeEnabled bool   `json:"wake_enabled"`
	WakeWord    string `json:"wake_word"`
}

func DefaultVoiceSettings() VoiceSettings {
	return VoiceSettings{WakeEnabled: true, WakeWord: "小灵同学"}
}

type Command struct {
	ID      string `json:"command_id"`
	GameID  string `json:"game_id"`
	Version int    `json:"expected_version"`
	Action  string `json:"action"`
	PieceID string `json:"piece_id,omitempty"`
	PropID  string `json:"prop_id,omitempty"`
	Token   string `json:"action_token,omitempty"`
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
	mu            sync.Mutex
	ID            string
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
	listeners     map[chan []byte]bool
	updated       time.Time
}

func New(o game.Options) *Room {
	return &Room{ID: game.ID(), state: game.New(o), voice: "disconnected", voiceSettings: DefaultVoiceSettings(), cache: map[string]cached{},
		listeners: map[chan []byte]bool{}, updated: time.Now()}
}

func (r *Room) view() View {
	b, _ := json.Marshal(r.state)
	return View{ID: r.ID, Seq: r.seq, State: b, Voice: r.voice, VoiceSettings: r.voiceSettings}
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
	r.voiceSettings = settings
	r.grant, r.busy = "", false
	r.publish()
	return r.view(), nil
}
func (r *Room) Snapshot() View { r.mu.Lock(); defer r.mu.Unlock(); return r.view() }
func (r *Room) publish() {
	r.seq++
	r.updated = time.Now()
	b, _ := json.Marshal(map[string]any{"type": "state", "room": r.view()})
	for ch := range r.listeners {
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
func (r *Room) Subscribe() (chan []byte, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan []byte, 1)
	r.listeners[ch] = true
	b, _ := json.Marshal(map[string]any{"type": "state", "room": r.view()})
	ch <- b
	return ch, func() { r.mu.Lock(); delete(r.listeners, ch); r.mu.Unlock() }
}

func (r *Room) Restart(o game.Options) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy {
		return View{}, fail("VOICE_BUSY", "请等这次语音操作结束，再开始新游戏")
	}
	r.state = game.New(o)
	r.cache = map[string]cached{}
	r.grant = ""
	r.used = false
	r.publish()
	return r.view(), nil
}

func (r *Room) Do(c Command, voice bool) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.cache[c.ID]; ok {
		if old.Input != c {
			return View{}, fail("COMMAND_CONFLICT", "同一个操作编号不能重复用于不同动作")
		}
		return old.Output, nil
	}
	if c.ID == "" || len(c.ID) > 160 {
		return View{}, fail("INVALID_COMMAND", "操作编号无效")
	}
	if c.GameID != r.state.ID || c.Version != r.state.Version {
		return View{}, fail("STALE_STATE", "棋局已经更新，请重新选择")
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
	if voice && (c.Token == "" || c.Token != r.grant || r.used || r.grantGame != r.state.ID || r.grantVersion != r.state.Version) {
		return View{}, fail("STALE_ACTION", "请重新查看棋局，再说一次操作")
	}
	if len(r.cache) >= 4096 {
		return View{}, fail("ROOM_LIMIT", "这局操作次数较多，请开始新的一局")
	}
	var err error
	switch c.Action {
	case "roll":
		err = r.state.RollRandom()
	case "prop":
		err = r.state.UsePropOn(c.PropID, c.PieceID)
	case "move":
		err = r.state.Move(c.PieceID)
	default:
		return View{}, fail("INVALID_ACTION", "不支持这个操作")
	}
	if err != nil {
		return View{}, fail("ILLEGAL_MOVE", err.Error())
	}
	if voice {
		r.used = true
	}
	r.publish()
	v := r.view()
	r.cache[c.ID] = cached{c, v}
	return v, nil
}

func (r *Room) AI() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy || r.state.Phase == "finished" || !r.state.Players[r.state.Current].AI {
		return
	}
	if r.state.Phase == "roll" {
		if prop, target := r.state.SuggestedPropTarget(); prop != "" {
			_ = r.state.UsePropOn(prop, target)
		} else {
			_ = r.state.RollRandom()
		}
	} else {
		_ = r.state.Move(r.state.BestMove().PieceID)
	}
	r.publish()
}

func (r *Room) BeginSpeech() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy && r.voice == "listening" {
		return fail("VOICE_BUSY", "上一句话还在处理，请稍等")
	}
	r.busy = true
	r.voice = "listening"
	r.grant = game.ID()
	r.used = false
	r.grantGame = r.state.ID
	r.grantVersion = r.state.Version
	r.publish()
	return nil
}

func (r *Room) CurrentActionToken() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.busy || r.used || r.grantGame != r.state.ID || r.grantVersion != r.state.Version {
		return ""
	}
	return r.grant
}

func (r *Room) VoiceState(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.voice = state
	if state == "ready" || state == "standby" || state == "disconnected" || state == "error" {
		r.busy = false
		r.grant = ""
	}
	r.publish()
}

func (r *Room) Tool(name string, args map[string]string) (any, error) {
	if name == "get_game_state" || name == "get_legal_moves" {
		r.mu.Lock()
		defer r.mu.Unlock()
		token := ""
		if r.busy && !r.used && r.grantGame == r.state.ID && r.grantVersion == r.state.Version {
			token = r.grant
		}
		suggested, target := r.state.SuggestedPropTarget()
		reason := "经典规则需六点起飞，启用本回合起飞卡后可用任意点数起飞；加速卡对已离开机库的飞机增加两步；幸运骰掷两次取较大值。每次掷骰前最多用一张已有道具，不能通过语音指定点数。炸弹立即送回对方公共航线上的一架指定飞机，护盾抵挡一次炸弹或撞机，两者都需指定piece_id且不能选机库、起飞位或终点航道；一次掷骰前仍只能用一张。超过终点不能移动，请只选 legal_moves 中的棋子。"
		return map[string]any{"room": r.view(), "action_token": token, "rules_hint": reason,
			"prop_targets": map[string][]string{"bomb": r.state.PropTargets("bomb"), "shield": r.state.PropTargets("shield")}, "suggested_prop": map[string]string{"prop_id": suggested, "piece_id": target},
			"current_player": r.state.Players[r.state.Current], "legal_moves": r.state.Legal}, nil
	}
	if name != "roll_dice" && name != "move_piece" && name != "use_prop" {
		return nil, fail("UNKNOWN_TOOL", "未知游戏工具")
	}
	token := args["action_token"]
	r.mu.Lock()
	// Derive identity/version from the grant; a model cannot select another room or game.
	c := Command{ID: "voice:" + token, GameID: r.grantGame, Version: r.grantVersion, Token: token, Action: "roll"}
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
	if old := s.sessions[session]; old != "" {
		return s.rooms[old], nil
	}
	if len(s.rooms) >= 256 {
		return nil, fmt.Errorf("房间已满，请稍后再试")
	}
	r := New(o)
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
	owned := s.sessions[session]
	if id != "" && id != owned {
		return nil
	}
	return s.rooms[owned]
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
