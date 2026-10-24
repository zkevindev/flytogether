// Package game contains the authoritative, transport-independent family-v1 rules.
package game

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
)

const RingLength = 52
const Finish = 58

type BoardSpec struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Layout     string `json:"layout"`
	RingLength int    `json:"ring_length"`
	HomeLength int    `json:"home_length"`
	Finish     int    `json:"finish"`
	JumpLength int    `json:"jump_length"`
	FlyFrom    int    `json:"fly_from"`
	FlyTo      int    `json:"fly_to"`
}

var boardCatalog = []BoardSpec{
	{ID: "cross", Name: "经典十字", Layout: "cross", RingLength: 52, HomeLength: 6, Finish: 58, JumpLength: 4, FlyFrom: 17, FlyTo: 29},
	{ID: "long", Name: "长航线", Layout: "cross", RingLength: 84, HomeLength: 8, Finish: 92, JumpLength: 4, FlyFrom: 25, FlyTo: 45},
	{ID: "circle", Name: "环形航线", Layout: "circle", RingLength: 52, HomeLength: 6, Finish: 58, JumpLength: 4, FlyFrom: 17, FlyTo: 29},
}

func Boards() []BoardSpec { return append([]BoardSpec(nil), boardCatalog...) }

func FindBoard(id string) (BoardSpec, bool) {
	if id == "" {
		id = "cross"
	}
	for _, board := range boardCatalog {
		if board.ID == id {
			return board, true
		}
	}
	return BoardSpec{}, false
}

var Colors = []string{"coral", "gold", "blue", "jade"}
var Labels = []string{"小熊号", "兔子号", "狐狸号", "小鹿号"}

type Piece struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Number   int    `json:"number"`
	Shield   bool   `json:"shield,omitempty"`
	Progress int    `json:"progress"` // -1 hangar, 0 launch pad, 1..ring ring, ring+1..finish home lane.
}

// MatchStats is accumulated at the authoritative mutation, never from the bounded feed.
type MatchStats struct {
	Rolls     int `json:"rolls"`
	Takeoffs  int `json:"takeoffs"`
	Arrivals  int `json:"arrivals"`
	Captures  int `json:"captures"`
	PropsUsed int `json:"props_used"`
}

type Player struct {
	Avatar string         `json:"avatar,omitempty"`
	Stats  MatchStats     `json:"stats"`
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Color  string         `json:"color"`
	AI     bool           `json:"ai"`
	Pieces []Piece        `json:"pieces"`
	Props  map[string]int `json:"props"`
}

type Move struct {
	PieceID  string   `json:"piece_id"`
	Label    string   `json:"label"`
	From     int      `json:"from"`
	To       int      `json:"to"`
	Captured []string `json:"captured"`
	Blocked  []string `json:"blocked,omitempty"`
	Jump     bool     `json:"jump"`
	Fly      bool     `json:"fly"`
	Launch   bool     `json:"launch"`
	Finish   bool     `json:"finish"`
	Summary  string   `json:"summary"`
}

type PropEffect struct {
	PieceID string `json:"piece_id"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Kind    string `json:"kind"`
}

type Event struct {
	PropID    string       `json:"prop_id,omitempty"`
	Effects   []PropEffect `json:"effects,omitempty"`
	ID        string       `json:"id"`
	Kind      string       `json:"kind"`
	Text      string       `json:"text"`
	Player    string       `json:"player_id,omitempty"`
	PieceID   string       `json:"piece_id,omitempty"`
	DiceRolls []int        `json:"dice_rolls,omitempty"`
}

type State struct {
	ID           string    `json:"id"`
	Preset       string    `json:"preset"`
	RuleInfo     RuleSpec  `json:"rules"`
	Ruleset      string    `json:"ruleset"`
	Difficulty   string    `json:"difficulty"`
	Board        BoardSpec `json:"board"`
	Mode         string    `json:"mode"`
	ThemeID      string    `json:"theme_id,omitempty"`
	Mission      *Mission  `json:"mission,omitempty"`
	Demo         bool      `json:"demo"`
	Version      int       `json:"version"`
	Turn         int       `json:"turn"`
	Current      int       `json:"current"`
	Phase        string    `json:"phase"`
	Dice         int       `json:"dice"`
	Winner       string    `json:"winner,omitempty"`
	Players      []Player  `json:"players"`
	Legal        []Move    `json:"legal_moves"`
	History      []Event   `json:"history"`
	LastMove     *Move     `json:"last_move,omitempty"`
	PropsEnabled bool      `json:"props_enabled"`
	ActiveProp   string    `json:"active_prop,omitempty"` // "launch_used" occupies this roll's prop slot after an immediate launch-card move.
	DiceRolls    []int     `json:"dice_rolls"`
}

type Options struct {
	ProfileIDs []string `json:"profile_ids,omitempty"`
	Avatars    []string `json:"avatars,omitempty"`
	Preset     string   `json:"preset,omitempty"`
	RoomName   string   `json:"room_name"`
	Difficulty string   `json:"difficulty"`
	Waiting    bool     `json:"waiting"`
	Mode       string   `json:"mode"`
	ThemeID    string   `json:"theme_id,omitempty"`
	MissionID  string   `json:"mission_id,omitempty"`
	Board      string   `json:"board"`
	Names      []string `json:"names"`
	AI         bool     `json:"ai"`
	Demo       bool     `json:"demo"`
	Props      bool     `json:"props"`
	Network    bool     `json:"network,omitempty"`
}

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func RandomDie() int { return randomIndex(6) + 1 }

func randomIndex(count int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
	if err != nil {
		panic(err)
	}
	return int(n.Int64())
}

func New(o Options) *State {
	o = ResolveOptions(o)
	if o.Difficulty != "easy" && o.Difficulty != "hard" {
		o.Difficulty = "normal"
	}
	if o.Mode != "classic" && o.Mode != "coop" {
		o.Mode = "quick"
	}
	count := 2
	if o.Mode == "classic" {
		count = 4
	}
	board, ok := FindBoard(o.Board)
	if !ok {
		board, _ = FindBoard("cross")
	}
	themeID := o.ThemeID
	if o.Mode == "coop" && themeID == "" {
		themeID = "star-rescue"
	}
	s := &State{Preset: o.Preset, ID: ID(), Ruleset: "family-v1", Difficulty: o.Difficulty, Board: board, Mode: o.Mode, ThemeID: themeID, Demo: o.Demo, Version: 1,
		Turn: 1, Phase: "roll", Legal: []Move{}, History: []Event{}, Players: []Player{}, PropsEnabled: o.Props, DiceRolls: []int{}}
	if o.Mode == "coop" && o.MissionID != "" {
		s.Mission = newMission(o.MissionID)
	}
	if o.Props {
		s.Ruleset = "family-props-v3"
	}
	if o.Mode == "coop" {
		s.Ruleset = "family-coop-v1"
	}
	if o.Preset == "gentle" {
		s.Ruleset = "family-gentle-v2"
	}
	if o.Preset == "classic" {
		s.Ruleset = "family-classic-v1"
	}
	s.RuleInfo = s.Rules()
	for i, name := range o.Names {
		p := Player{ID: fmt.Sprintf("player-%d", i), Name: name, Color: Colors[i], AI: o.AI && i == len(o.Names)-1, Pieces: []Piece{}, Props: map[string]int{}}
		if i < len(o.Avatars) && ValidAvatar(o.Avatars[i]) && !p.AI {
			p.Avatar = o.Avatars[i]
		}
		if o.Props {
			p.Props = map[string]int{"launch": 1, "boost": 1, "luck": 1, "bomb": 1, "shield": 1, "boomerang": 1, "rescue": 1, "escort": 1}
			if o.Preset == "gentle" {
				p.Props = map[string]int{"launch": 2, "boost": 1, "luck": 1, "bomb": 0, "shield": 2, "rescue": 1, "escort": 1}
			}
		}
		for j := 0; j < count; j++ {
			p.Pieces = append(p.Pieces, Piece{ID: fmt.Sprintf("p%d-%d", i, j), Label: Labels[j], Number: j + 1, Progress: -1})
		}
		s.Players = append(s.Players, p)
	}
	if o.Demo {
		s.Players[0].Pieces[0].Progress = 12
		s.Players[0].Pieces[1].Progress = 35
		s.Players[1].Pieces[0].Progress = 16
		if len(s.Players) > 2 {
			s.Players[2].Pieces[0].Progress = 32
		}
	}
	s.RefreshMission("playing")
	s.add("started", "欢迎登机！轮到"+s.Players[0].Name+"掷骰子。", "")
	return s
}

func Cell(player, progress int) int { return CellOn(RingLength, player, progress) }

func CellOn(ringLength, player, progress int) int {
	return (player*(ringLength/4) + progress - 1) % ringLength
}

func (s *State) add(kind, text, piece string) {
	s.History = append(s.History, Event{ID: ID(), Kind: kind, Text: text, Player: s.Players[s.Current].ID, PieceID: piece})
	if len(s.History) > 24 {
		s.History = s.History[len(s.History)-24:]
	}
}

func (s *State) legalMoves() []Move {
	out := []Move{}
	p := s.Players[s.Current]
	for _, piece := range p.Pieces {
		if piece.Progress == s.Board.Finish {
			continue
		}
		m := Move{PieceID: piece.ID, Label: piece.Label, From: piece.Progress, Captured: []string{}}
		if piece.Progress == -1 {
			if !s.CanLaunch(s.Dice) {
				continue
			}
			m.To, m.Launch = 0, true
			m.Summary = "从机场起飞，进入起飞位"
		} else {
			steps := s.Dice
			if s.ActiveProp == "boost" {
				steps += 2
			}
			m.To = piece.Progress + steps
			if m.To > s.Board.Finish {
				continue
			}
			if m.To <= s.Board.RingLength {
				// The flight square takes priority; a normal jump can also reach it.
				if m.To == s.Board.FlyFrom {
					m.To, m.Fly = s.Board.FlyTo, true
				} else if CellOn(s.Board.RingLength, s.Current, m.To)%4 == s.Current && m.To+s.Board.JumpLength <= s.Board.RingLength {
					m.To += s.Board.JumpLength
					m.Jump = true
					if m.To == s.Board.FlyFrom {
						m.To, m.Fly = s.Board.FlyTo, true
					}
				}
			}
			m.Finish = m.To == s.Board.Finish
			if m.To > 0 && m.To <= s.Board.RingLength {
				for i, other := range s.Players {
					if i == s.Current {
						continue
					}
					for _, op := range other.Pieces {
						if op.Progress > 0 && op.Progress <= s.Board.RingLength && CellOn(s.Board.RingLength, i, op.Progress) == CellOn(s.Board.RingLength, s.Current, m.To) {
							if op.Shield {
								m.Blocked = append(m.Blocked, op.ID)
							} else {
								m.Captured = append(m.Captured, op.ID)
							}
						}
					}
				}
			}
			m.Summary = fmt.Sprintf("前进 %d 格", steps)
			if s.ActiveProp == "boost" {
				m.Summary += "（加速卡 +2）"
			}
			if m.Jump {
				m.Summary += "，同色跳跃"
			}
			if m.Fly {
				m.Summary += "，穿越飞行航线"
			}
			if len(m.Captured) > 0 {
				m.Summary += fmt.Sprintf("，送 %d 架飞机回机场", len(m.Captured))
			}
			if len(m.Blocked) > 0 {
				m.Summary += fmt.Sprintf("，击破 %d 个护盾", len(m.Blocked))
			}
			if m.Finish {
				m.Summary += "，抵达终点"
			}
		}
		out = append(out, m)
	}
	return out
}

func (s *State) Roll(die int) error {
	return s.RollValues(die)
}

// RollRandom is the transport-facing entry point; clients never supply dice.
func (s *State) RollRandom() error {
	values := []int{RandomDie()}
	if s.ActiveProp == "luck" {
		values = append(values, RandomDie())
	}
	return s.RollValues(values...)
}

func (s *State) RollValues(values ...int) error {
	if s.Phase != "roll" {
		return fmt.Errorf("请先选择这次要移动的飞机")
	}
	count := 1
	if s.ActiveProp == "luck" {
		count = 2
	}
	if len(values) != count {
		return fmt.Errorf("骰子数量无效")
	}
	die := 0
	for _, value := range values {
		if value < 1 || value > 6 {
			return fmt.Errorf("骰子点数无效")
		}
		if value > die {
			die = value
		}
	}
	s.Players[s.Current].Stats.Rolls++
	s.DiceRolls = append([]int(nil), values...)
	s.Dice, s.LastMove = die, nil
	if count == 2 {
		s.add("rolled", fmt.Sprintf("%s 的幸运骰掷出 %d 和 %d 点，取 %d 点", s.Players[s.Current].Name, values[0], values[1], die), "")
	} else {
		s.add("rolled", fmt.Sprintf("%s 掷出了 %d 点", s.Players[s.Current].Name, die), "")
	}
	s.History[len(s.History)-1].DiceRolls = append([]int(nil), values...)
	s.Legal = s.legalMoves()
	s.Version++
	s.Phase = "move"
	if len(s.Legal) == 0 {
		s.add("passed", "这次没有可以移动的飞机，等待下一个机会。", "")
		s.next()
	} else if len(s.Legal) == 1 {
		s.apply(s.Legal[0])
	}
	return nil
}

func (s *State) Move(id string) error {
	if s.Phase != "move" {
		return fmt.Errorf("请先掷骰子")
	}
	for _, m := range s.Legal {
		if m.PieceID == id {
			s.Version++
			s.apply(m)
			return nil
		}
	}
	return fmt.Errorf("这架飞机本次不能移动，请选择高亮的飞机")
}

func (s *State) apply(m Move) {
	p := &s.Players[s.Current]
	if m.Launch {
		p.Stats.Takeoffs++
	}
	if m.Finish {
		p.Stats.Arrivals++
	}
	p.Stats.Captures += len(m.Captured)
	for i := range p.Pieces {
		if p.Pieces[i].ID == m.PieceID {
			p.Pieces[i].Progress = m.To
		}
	}
	blocked := map[string]bool{}
	for _, id := range m.Blocked {
		blocked[id] = true
	}
	captured := map[string]bool{}
	for _, id := range m.Captured {
		captured[id] = true
	}
	for i := range s.Players {
		for j := range s.Players[i].Pieces {
			if blocked[s.Players[i].Pieces[j].ID] {
				s.Players[i].Pieces[j].Shield = false
			}
			if captured[s.Players[i].Pieces[j].ID] {
				s.Players[i].Pieces[j].Shield = false
				s.Players[i].Pieces[j].Progress = -1
			}
		}
	}
	s.LastMove = &m
	s.add("moved", p.Name+"的"+m.Label+"："+m.Summary, m.PieceID)
	finished := 0
	for _, piece := range p.Pieces {
		if piece.Progress == s.Board.Finish {
			finished++
		}
	}
	target := len(p.Pieces)
	if s.Mode == "quick" || s.Mode == "coop" {
		target = 1
	}
	if finished >= target {
		if s.Mode == "coop" {
			allReady := true
			for _, player := range s.Players {
				arrived := 0
				for _, piece := range player.Pieces {
					if piece.Progress == s.Board.Finish {
						arrived++
					}
				}
				if arrived == 0 {
					allReady = false
					break
				}
			}
			if !allReady {
				s.next()
				return
			}
			s.ActiveProp = ""
			s.Winner = "team"
			s.Phase = "finished"
			s.Legal = []Move{}
			s.add("won", "全员抵达星河航站，星河救援成功！", m.PieceID)
			return
		}
		s.ActiveProp = ""
		s.Winner = p.ID
		s.Phase = "finished"
		s.Legal = []Move{}
		s.add("won", p.Name+"率先抵达，赢下了这次飞行！", m.PieceID)
		return
	}
	s.next()
}

func (s *State) next() {
	s.ActiveProp = ""
	s.Phase = "roll"
	s.Legal = []Move{}
	s.Turn++
	if s.Dice != 6 {
		s.Current = (s.Current + 1) % len(s.Players)
	} else {
		s.add("bonus", "掷到六点，再获得一次机会！", "")
	}
}

func (s *State) BestMove() Move {
	moves := append([]Move(nil), s.Legal...)
	score := func(m Move) int {
		n := m.To
		if m.Launch {
			n += 20
		}
		if m.Jump {
			n += 30
		}
		if m.Fly {
			n += 50
		}
		n += len(m.Captured) * 100
		if m.Finish {
			n += 1000
		}
		return n
	}
	sort.SliceStable(moves, func(i, j int) bool { return score(moves[i]) > score(moves[j]) })
	if len(moves) == 0 {
		return Move{}
	}
	return moves[0]
}

func (s *State) AIMove() Move {
	if s.Difficulty == "easy" && len(s.Legal) > 0 {
		return s.Legal[randomIndex(len(s.Legal))]
	}
	return s.BestMove()
}
