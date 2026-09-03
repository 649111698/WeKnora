package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureFinalAnswerEvents records every EventAgentFinalAnswer emission so
// tests can assert both the streamed content and the supersede markers.
func captureFinalAnswerEvents(engine *AgentEngine) *[]event.AgentFinalAnswerData {
	var captured []event.AgentFinalAnswerData
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentFinalAnswerData); ok {
			captured = append(captured, d)
		}
		return nil
	})
	return &captured
}

func countSupersedes(events *[]event.AgentFinalAnswerData) int {
	n := 0
	for _, e := range *events {
		if e.SupersedePrior {
			n++
		}
	}
	return n
}

// TestStreamThinkingToEventBus_ToolCallRoundSupersedesStreamedPreamble:
// a round that narrates into the answer area AND requests tool calls is not
// terminal — the narration is a preamble. The round must end with exactly one
// supersede marker so the preamble cannot survive into the final answer.
func TestStreamThinkingToEventBus_ToolCallRoundSupersedesStreamedPreamble(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "Preamble narration."},
				{
					ResponseType: types.ResponseTypeToolCall,
					ToolCalls: []types.LLMToolCall{{
						ID:       "call-1",
						Function: types.FunctionCall{Name: "some_tool", Arguments: `{}`},
					}},
					FinishReason: "tool_calls",
				},
				// Content deltas can arrive after the tool-call deltas; they must
				// still be covered by the round-end supersede.
				{ResponseType: types.ResponseTypeAnswer, Content: "Trailing narration."},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	captured := captureFinalAnswerEvents(engine)

	resp, err := engine.streamThinkingToEventBus(context.Background(),
		emptyMessages(), emptyTools(), 0, "sess-1")
	require.NoError(t, err)
	require.NotEmpty(t, resp.ToolCalls, "round must carry the requested tool call")

	assert.Equal(t, 1, countSupersedes(captured),
		"a tool-calling round must emit exactly one round-end supersede marker")
	var streamed string
	for _, e := range *captured {
		if !e.SupersedePrior {
			streamed += e.Content
		}
	}
	assert.Equal(t, "Preamble narration.Trailing narration.", streamed)
}

// TestStreamThinkingToEventBus_NaturalStopDoesNotSupersede: the terminal round
// (plain answer, no tool calls) is the real answer — no supersede may be
// emitted, or the answer itself would be retracted.
func TestStreamThinkingToEventBus_NaturalStopDoesNotSupersede(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "The answer."},
				{ResponseType: types.ResponseTypeAnswer, Content: "", Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	captured := captureFinalAnswerEvents(engine)

	_, err := engine.streamThinkingToEventBus(context.Background(),
		emptyMessages(), emptyTools(), 0, "sess-1")
	require.NoError(t, err)

	assert.Zero(t, countSupersedes(captured),
		"a natural-stop round must not supersede the streamed answer")
}

// TestStreamFinalAnswerToEventBus_SupersedesBeforeSynthesis: the finalize
// synthesis is a complete replacement answer; it must retract all previously
// streamed answer segments first so leftover preambles do not render the
// answer twice.
func TestStreamFinalAnswerToEventBus_SupersedesBeforeSynthesis(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "Synthesized answer."},
				{ResponseType: types.ResponseTypeAnswer, Content: "", Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	captured := captureFinalAnswerEvents(engine)

	state := &types.AgentState{RoundSteps: []types.AgentStep{{Iteration: 0}}}
	err := engine.streamFinalAnswerToEventBus(context.Background(), "test query", state, "sess-1")
	require.NoError(t, err)

	require.NotEmpty(t, *captured, "finalize must emit answer events")
	assert.True(t, (*captured)[0].SupersedePrior,
		"the first finalize emission must be the supersede marker")
	assert.Equal(t, 1, countSupersedes(captured),
		"finalize must emit exactly one supersede marker")
}
