package linx

import (
	"strings"
	"testing"

	"github.com/zkevindev/flytogether/internal/game"
	"github.com/zkevindev/flytogether/internal/room"
)

func TestBridgeProtocolCoachProposalComparisonAndConfirmation(t *testing.T) {
	r := room.New(game.Options{Names: []string{"甲", "乙"}, Demo: true})
	state := protocolState(t, r)
	if _, err := r.Do(room.Command{ID: "prepare-boost", GameID: state.ID, Version: state.Version, Action: "prop", PropID: "boost"}, false); err == nil {
		t.Fatal("disabled props accepted boost")
	}
	// Demo has two airborne pieces, so every random die produces a real choice.
	if _, err := r.Do(room.Command{ID: "prepare-roll", GameID: state.ID, Version: state.Version, Action: "roll"}, false); err != nil {
		t.Fatal(err)
	}
	if state = protocolState(t, r); state.Phase != "move" || len(state.Legal) < 2 {
		t.Fatalf("expected two legal planes: %+v", state)
	}
	h := newProtocolHarness(t, r, false, "quiet")
	input := func(id, text string) map[string]any {
		h.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id, "transcript": text})
		readUntil(t, h.ctx, h.browser, "stt")
		receipt := readUntil(t, h.ctx, h.browser, "game_action")
		spectatorRequest(t, h.ctx, h.requests, "response.create")
		h.send(t, map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
		readUntil(t, h.ctx, h.browser, "response.created")
		h.send(t, map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		readUntil(t, h.ctx, h.browser, "response.done")
		return receipt
	}
	receipt := input("proposal", "帮我选")
	if receipt["action"] != "coach_move" || r.Snapshot().Coach == nil {
		t.Fatal("voice proposal absent", receipt)
	}
	first := r.Snapshot().Coach.Move.PieceID
	if protocolState(t, r).Version != state.Version {
		t.Fatal("proposal moved a plane")
	}
	input("comparison", "另一架")
	if got := r.Snapshot().Coach; got == nil || got.Move.PieceID == first {
		t.Fatal("comparison did not change proposal")
	}
	input("cancel", "取消建议")
	if r.Snapshot().Coach != nil || protocolState(t, r).Version != state.Version {
		t.Fatal("cancel moved a plane or retained proposal")
	}
	input("reproposal", "帮我选")
	proposed := r.Snapshot().Coach.Move.PieceID
	receipt = input("confirm", "就它")
	if receipt["action"] != "confirm_coach" || protocolState(t, r).Version <= state.Version {
		t.Fatal("confirmation did not commit")
	}
	if got := protocolState(t, r); got.LastMove == nil || got.LastMove.PieceID != proposed {
		t.Fatalf("wrong proposal committed: %+v", got.LastMove)
	}
	if !strings.Contains(receipt["text"].(string), "甲") {
		t.Fatal("confirmation omitted acting player")
	}
}
