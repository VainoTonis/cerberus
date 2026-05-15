package stream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
}

// Limits configures when to kill the agent.
type Limits struct {
	MaxTurns        int
	MaxOutputTokens int
}

// Processor reads pi JSON events from a reader, emits structured events,
// tracks token usage, and enforces turn/token limits.
type Processor struct {
	session string
	emitter event.Emitter
	logW    io.Writer
	limits  Limits
	cancel  func()
	stats   Stats
}

func NewProcessor(session string, emitter event.Emitter, logW io.Writer, limits Limits, cancel func()) *Processor {
	return &Processor{
		session: session,
		emitter: emitter,
		logW:    logW,
		limits:  limits,
		cancel:  cancel,
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
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
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
				fmt.Fprintf(os.Stderr, "[%s] turn limit reached (%d turns)\n", p.session, p.stats.Turns)
				if p.cancel != nil {
					p.cancel()
				}
			}
			if p.limits.MaxOutputTokens > 0 && p.stats.OutputTokens >= p.limits.MaxOutputTokens {
				fmt.Fprintf(os.Stderr, "[%s] output token limit reached (%d tokens)\n", p.session, p.stats.OutputTokens)
				if p.cancel != nil {
					p.cancel()
				}
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
