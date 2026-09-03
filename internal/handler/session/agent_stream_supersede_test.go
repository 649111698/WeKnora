package session

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

// TestAgentStreamHandlerSupersedePersistedContent replays the exact event
// sequence of a multi-round agent turn through the real AgentStreamHandler:
// tool-calling rounds narrate into the answer area, each tool-calling round
// ends with a supersede marker, the finalize synthesis retracts everything
// before streaming, and the completion event carries the synthesis. The
// persisted assistant message must contain the synthesis only — no preamble
// duplication, no preamble replacing the synthesis.
func TestAgentStreamHandlerSupersedePersistedContent(t *testing.T) {
	bus := event.NewEventBus()
	sm := stream.NewMemoryStreamManager()
	msg := &types.Message{ID: "assistant-1", SessionID: "sess-1", Role: "assistant"}

	handler := NewAgentStreamHandler(
		context.Background(), "sess-1", "assistant-1", "req-1", 10000,
		time.Now(), msg, sm, bus, nil,
	)
	handler.Subscribe()

	emitAnswer := func(id, content string, done bool) {
		bus.Emit(context.Background(), event.Event{
			ID: id, Type: event.EventAgentFinalAnswer, SessionID: "sess-1",
			Data: event.AgentFinalAnswerData{Content: content, Done: done},
		})
	}
	emitSupersede := func(id string) {
		bus.Emit(context.Background(), event.Event{
			ID: id, Type: event.EventAgentFinalAnswer, SessionID: "sess-1",
			Data: event.AgentFinalAnswerData{SupersedePrior: true},
		})
	}
	emitToolCall := func(id string) {
		bus.Emit(context.Background(), event.Event{
			ID: id, Type: event.EventAgentToolCall, SessionID: "sess-1",
			Data: event.AgentToolCallData{ToolCallID: id, ToolName: "some_tool"},
		})
	}

	// Round 1: narrate then call a tool.
	emitAnswer("r1-answer", "我先查一下售后数据。", false)
	emitToolCall("tc-1")
	emitSupersede("r1-answer-supersede")
	// Round 2: more narration after the tool-call event (the leak case), then a tool.
	emitToolCall("tc-2")
	emitAnswer("r2-answer", "数据取到了，再按类型过滤。", false)
	emitSupersede("r2-answer-supersede")
	// Finalize: retract everything, stream the synthesis.
	emitSupersede("final-answer-supersede")
	emitAnswer("final-answer", "统计结论：问题查询 24 条。", false)
	emitAnswer("final-answer", "", true)
	// Completion carries the engine's synthesized answer.
	bus.Emit(context.Background(), event.Event{
		ID: "complete-1", Type: event.EventAgentComplete, SessionID: "sess-1",
		Data: event.AgentCompleteData{
			FinalAnswer: "统计结论：问题查询 24 条。",
			MessageID:   "assistant-1",
			AgentSteps:  []types.AgentStep{},
		},
	})

	assert.Equal(t, "统计结论：问题查询 24 条。", msg.Content,
		"persisted content must be the synthesis only")
}
