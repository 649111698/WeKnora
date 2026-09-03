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

// TestStreamThinkingToEventBus_TruncatedRoundSupersedesStreamedDraft: a round
// cut off at the completion-token cap (finish_reason=length) with no tool
// calls is NOT terminal — analyzeResponse keeps the loop running and the model
// re-answers next round. The truncated draft streamed into the answer area
// must therefore be superseded, or consecutive drafts concatenate and the user
// sees the same answer repeated two or three times.
func TestStreamThinkingToEventBus_TruncatedRoundSupersedesStreamedDraft(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "Partial draft cut mid-sentence…"},
				{ResponseType: types.ResponseTypeAnswer, Content: "", Done: true, FinishReason: "length"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	captured := captureFinalAnswerEvents(engine)

	resp, err := engine.streamThinkingToEventBus(context.Background(),
		emptyMessages(), emptyTools(), 0, "sess-1")
	require.NoError(t, err)
	require.Empty(t, resp.ToolCalls)
	require.Equal(t, "length", resp.FinishReason)

	assert.Equal(t, 1, countSupersedes(captured),
		"a length-truncated round must emit exactly one round-end supersede marker")
}

// TestStreamFinalAnswerToEventBus_EmptySynthesisFallsBack: an always-thinking
// model can spend its whole completion budget on reasoning and return an
// empty synthesis. The turn must emit a visible fallback answer instead of
// silently persisting nothing after the user waited through every round.
func TestStreamFinalAnswerToEventBus_EmptySynthesisFallsBack(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeThinking, Content: "reasoning consumes the whole budget"},
				{ResponseType: types.ResponseTypeAnswer, Content: "", Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	captured := captureFinalAnswerEvents(engine)

	state := &types.AgentState{RoundSteps: []types.AgentStep{{Iteration: 0}}}
	err := engine.streamFinalAnswerToEventBus(context.Background(), "test query", state, "sess-1")
	require.NoError(t, err)

	require.NotEmpty(t, state.FinalAnswer, "empty synthesis must fall back to a visible answer")

	var composed string
	var lastIsDoneMarker, sawFallbackContent = false, false
	for _, e := range *captured {
		if e.SupersedePrior {
			continue
		}
		if e.Done && e.Content == "" {
			lastIsDoneMarker = true
		} else {
			lastIsDoneMarker = false
			composed += e.Content
			if e.Content != "" {
				sawFallbackContent = true
			}
		}
	}
	assert.True(t, sawFallbackContent, "fallback content must be emitted")
	assert.True(t, lastIsDoneMarker, "emissions must end with the Done marker")
	assert.Equal(t, state.FinalAnswer, composed,
		"streamed fallback must match the persisted final answer")
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
