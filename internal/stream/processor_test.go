package stream

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tonis/cerberus/internal/event"
)

// noopEmitter discards all events; used to keep processor tests focused on Stats.
type noopEmitter struct{}

func (noopEmitter) Emit(event.Event) error { return nil }
func (noopEmitter) Close() error           { return nil }

// buildTurnEvents returns a synthetic pi JSON event stream with n turns, each
// containing one tool call (tool_execution_start + tool_execution_end).
func buildTurnEvents(n int) string {
	var b strings.Builder
	fmt.Fprintln(&b, `{"type":"session","id":"sess-1"}`)
	for i := 0; i < n; i++ {
		toolID := fmt.Sprintf("tool-%d", i)
		fmt.Fprintf(&b, `{"type":"tool_execution_start","toolCallId":%q,"toolName":"read"}`+"\n", toolID)
		fmt.Fprintf(&b, `{"type":"tool_execution_end","toolCallId":%q,"toolName":"read","result":{"content":[{"text":"ok"}]}}`+"\n", toolID)
		fmt.Fprintf(&b, `{"type":"message_end","message":{"usage":{"input":10,"output":5}}}`+"\n")
	}
	return b.String()
}

func TestProcess_NoLimits_NoCancel(t *testing.T) {
	cancelCalls := 0
	cancel := func() { cancelCalls++ }

	p := NewProcessor("s", noopEmitter{}, nil, Limits{}, cancel)
	stats := p.Process(strings.NewReader(buildTurnEvents(5)))

	if stats.Turns != 5 {
		t.Fatalf("expected 5 turns, got %d", stats.Turns)
	}
	if stats.ToolCalls != 5 {
		t.Fatalf("expected 5 tool calls, got %d", stats.ToolCalls)
	}
	if stats.LimitReason != "" {
		t.Fatalf("expected no limit reason, got %q", stats.LimitReason)
	}
	if cancelCalls != 0 {
		t.Fatalf("expected cancel not called, got %d calls", cancelCalls)
	}
}

func TestProcess_MaxTurns_TriggersLimitAndCancelOnce(t *testing.T) {
	cancelCalls := 0
	cancel := func() { cancelCalls++ }

	p := NewProcessor("s", noopEmitter{}, nil, Limits{MaxTurns: 3}, cancel)
	stats := p.Process(strings.NewReader(buildTurnEvents(10)))

	if stats.Turns != 10 {
		t.Fatalf("expected all 10 turns to be processed (cancel doesn't stop the stream), got %d", stats.Turns)
	}
	if stats.ToolCalls != 10 {
		t.Fatalf("expected 10 tool calls (limit only affects future cancellation), got %d", stats.ToolCalls)
	}
	if stats.LimitReason == "" || !strings.Contains(stats.LimitReason, "turn limit") {
		t.Fatalf("expected turn limit reason, got %q", stats.LimitReason)
	}
	if cancelCalls != 1 {
		t.Fatalf("expected cancel called exactly once, got %d", cancelCalls)
	}
	if stats.ToolExecTime < 0 {
		t.Fatalf("expected non-negative tool exec time, got %v", stats.ToolExecTime)
	}
}
