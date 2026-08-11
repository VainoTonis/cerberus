package stream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tonis/cerberus/internal/event"
)

// Stats holds accumulated token usage and metadata from a processed stream.
type Stats struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
	Turns            int
	SessionID        string

	// ToolCalls is the number of tool_execution_start events observed.
	ToolCalls int
	// LimitReason is set when a turn or output-token limit causes cancellation.
	LimitReason string
	// ToolExecTime is the wall-clock time spent inside tool execution
	// (between tool_execution_start and its matching tool_execution_end).
	ToolExecTime time.Duration
}

// Limits configures when to kill the agent.
type Limits struct {
	MaxTurns        int
	MaxOutputTokens int
}

// Processor reads pi JSON events from a reader, emits structured events,
// tracks token usage, and enforces turn/token limits.
type Processor struct {
	session   string
	emitter   event.Emitter
	logW      io.Writer
	limits    Limits
	cancel    func()
	stats     Stats
	canceled  bool
	toolStart map[string]time.Time
}

func NewProcessor(session string, emitter event.Emitter, logW io.Writer, limits Limits, cancel func()) *Processor {
	return &Processor{
		session:   session,
		emitter:   emitter,
		logW:      logW,
		limits:    limits,
		cancel:    cancel,
		toolStart: make(map[string]time.Time),
	}
}

// piEvent is the JSON structure emitted by the pi agent (--mode json).
type piEvent struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Message struct {
		Usage struct {
			Input      int `json:"input"`
			Output     int `json:"output"`
			CacheRead  int `json:"cacheRead"`
			CacheWrite int `json:"cacheWrite"`
			Cost       struct {
				Total float64 `json:"total"`
			} `json:"cost"`
		} `json:"usage"`
	} `json:"message"`
	AssistantMessageEvent struct {
		Type     string `json:"type"`
		Delta    string `json:"delta"`
		ToolCall struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"toolCall"`
	} `json:"assistantMessageEvent"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Result     struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
}

// Process reads from r until EOF, parsing pi events and emitting structured
// events through the configured emitter. Blocks until r is closed or an
// unrecoverable error occurs. Returns accumulated stats.
func (p *Processor) Process(r io.Reader) Stats {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if p.logW != nil {
			fmt.Fprintln(p.logW, line)
		}

		var ev piEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			e := event.New(event.Log, p.session)
			e.Content = line
			p.emitter.Emit(e)
			continue
		}

		switch {
		case ev.Type == "session" && ev.ID != "":
			if p.stats.SessionID == "" {
				p.stats.SessionID = ev.ID
			}
			e := event.New(event.SessionStart, p.session)
			e.SessionID = ev.ID
			p.emitter.Emit(e)

		case ev.Type == "message_update" &&
			ev.AssistantMessageEvent.Type == "text_delta" &&
			ev.AssistantMessageEvent.Delta != "":
			e := event.New(event.TextDelta, p.session)
			e.Content = ev.AssistantMessageEvent.Delta
			p.emitter.Emit(e)

		case ev.Type == "message_update" &&
			ev.AssistantMessageEvent.Type == "toolcall_end":
			toolInput, _ := json.Marshal(ev.AssistantMessageEvent.ToolCall.Arguments)
			e := event.New(event.ToolUse, p.session)
			e.ToolName = ev.AssistantMessageEvent.ToolCall.Name
			e.ToolInput = string(toolInput)
			p.emitter.Emit(e)

		case ev.Type == "tool_execution_start":
			p.stats.ToolCalls++
			if ev.ToolCallID != "" {
				p.toolStart[ev.ToolCallID] = time.Now()
			}

		case ev.Type == "tool_execution_end":
			if ev.ToolCallID != "" {
				if start, ok := p.toolStart[ev.ToolCallID]; ok {
					p.stats.ToolExecTime += time.Since(start)
					delete(p.toolStart, ev.ToolCallID)
				}
			}
			e := event.New(event.ToolResult, p.session)
			e.ToolName = ev.ToolName
			if len(ev.Result.Content) > 0 {
				e.Content = ev.Result.Content[0].Text
			}
			p.emitter.Emit(e)

		case ev.Type == "message_end":
			p.stats.InputTokens += ev.Message.Usage.Input
			p.stats.OutputTokens += ev.Message.Usage.Output
			p.stats.CacheReadTokens += ev.Message.Usage.CacheRead
			p.stats.CacheWriteTokens += ev.Message.Usage.CacheWrite
			p.stats.CostUSD += ev.Message.Usage.Cost.Total
			p.stats.Turns++

			e := event.New(event.MessageEnd, p.session)
			e.Usage = &event.Usage{
				InputTokens:      ev.Message.Usage.Input,
				OutputTokens:     ev.Message.Usage.Output,
				CacheReadTokens:  ev.Message.Usage.CacheRead,
				CacheWriteTokens: ev.Message.Usage.CacheWrite,
				CostUSD:          ev.Message.Usage.Cost.Total,
			}
			p.emitter.Emit(e)

			if p.limits.MaxTurns > 0 && p.stats.Turns >= p.limits.MaxTurns {
				reason := fmt.Sprintf("turn limit reached (%d turns)", p.stats.Turns)
				fmt.Fprintf(os.Stderr, "[%s] %s\n", p.session, reason)
				if p.stats.LimitReason == "" {
					p.stats.LimitReason = reason
				}
				p.doCancel()
			}
			if p.limits.MaxOutputTokens > 0 && p.stats.OutputTokens >= p.limits.MaxOutputTokens {
				reason := fmt.Sprintf("output token limit reached (%d tokens)", p.stats.OutputTokens)
				fmt.Fprintf(os.Stderr, "[%s] %s\n", p.session, reason)
				if p.stats.LimitReason == "" {
					p.stats.LimitReason = reason
				}
				p.doCancel()
			}

		default:
			e := event.New(event.Raw, p.session)
			e.Content = line
			p.emitter.Emit(e)
		}
	}

	return p.stats
}

// Stats returns the current accumulated stats (safe to call after Process returns).
func (p *Processor) Stats() Stats {
	return p.stats
}

// doCancel invokes the cancel function at most once.
func (p *Processor) doCancel() {
	if p.canceled {
		return
	}
	p.canceled = true
	if p.cancel != nil {
		p.cancel()
	}
}
