package linx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/zkevindev/flytogether/internal/game"
)

var ErrSpeechBusy = errors.New("播报正在准备，请稍后重试")

// Speaker is an output-only Lingxi session. It has no microphone input and no
// game tools. A small bounded cache avoids regenerating repeated turn names.
type Speaker struct {
	Config Config
	mu     sync.Mutex
	busy   bool
	cache  map[string][]byte
	order  []string
}

func TurnAnnouncement(state game.State) (string, string) {
	if len(state.Players) == 0 {
		return "", ""
	}
	if state.Phase == "finished" {
		for _, player := range state.Players {
			if player.ID == state.Winner {
				return state.ID + ":winner", "恭喜" + player.Name + "获得胜利，大家一起为他撒花！"
			}
		}
	}
	if state.Phase != "roll" || state.Current < 0 || state.Current >= len(state.Players) {
		return "", ""
	}
	return state.ID + ":" + strconv.Itoa(state.Turn), "轮到" + state.Players[state.Current].Name + "掷骰子啦！"
}

func (s *Speaker) Say(ctx context.Context, text string) ([]byte, error) {
	if !s.Config.Enabled() || strings.TrimSpace(text) == "" {
		return nil, errors.New("灵矽播报尚未配置")
	}
	s.mu.Lock()
	if audio := s.cache[text]; len(audio) > 0 {
		s.mu.Unlock()
		return audio, nil
	}
	if s.busy {
		s.mu.Unlock()
		return nil, ErrSpeechBusy
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	endpoint, err := realtimeURL(s.Config.URL, s.Config.Model)
	if err != nil {
		return nil, errors.New("灵矽播报配置无效")
	}
	up, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{Subprotocols: []string{"realtime"}, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + s.Config.APIKey}}})
	if err != nil {
		return nil, errors.New("灵矽播报连接暂时不可用")
	}
	defer up.CloseNow()
	up.SetReadLimit(1 << 20)
	send := func(value any) error {
		data, e := json.Marshal(value)
		if e != nil {
			return e
		}
		return up.Write(ctx, websocket.MessageText, data)
	}
	var audio []byte
	configured, requested := false, false
	for {
		_, data, err := up.Read(ctx)
		if err != nil {
			return nil, errors.New("灵矽播报暂时没有完成")
		}
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Status string `json:"status"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		switch event.Type {
		case "session.created":
			update := realtimeSessionUpdate(s.Config.Model)
			session := update["session"].(map[string]any)
			session["tools"] = []any{}
			session["tool_choice"] = "none"
			session["instructions"] = "你是家庭飞行棋的中文播报员，只朗读给定内容，不增加事实。"
			if err := send(update); err != nil {
				return nil, errors.New("灵矽播报初始化失败")
			}
			configured = true
		case "session.updated":
			if !configured || requested {
				continue
			}
			requested = true
			if err := send(map[string]any{"type": "response.create", "response": map[string]any{"tool_choice": "none", "instructions": "请只用亲切简短的中文朗读下面这句话，不要补充内容：" + text}}); err != nil {
				return nil, errors.New("灵矽播报请求失败")
			}
		case "response.audio.delta", "response.output_audio.delta":
			if !requested {
				continue
			}
			chunk, err := base64.StdEncoding.DecodeString(event.Delta)
			if err != nil || len(audio)+len(chunk) > 2<<20 {
				return nil, errors.New("灵矽播报音频无效")
			}
			audio = append(audio, chunk...)
		case "error":
			return nil, errors.New("灵矽播报暂时不可用")
		case "response.done":
			if !requested || event.Response.Status != "completed" || len(audio) == 0 || len(audio)%2 != 0 {
				return nil, errors.New("灵矽播报没有返回完整音频")
			}
			s.mu.Lock()
			if s.cache == nil {
				s.cache = make(map[string][]byte)
			}
			if len(s.order) >= 32 {
				delete(s.cache, s.order[0])
				s.order = s.order[1:]
			}
			s.cache[text] = audio
			s.order = append(s.order, text)
			s.mu.Unlock()
			return audio, nil
		}
	}
}
