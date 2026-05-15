package event

import "time"

type Type string

const (
	SessionStart Type = "session_start"
	TextDelta    Type = "text_delta"
	ToolUse      Type = "tool_use"
	ToolResult   Type = "tool_result"
	MessageEnd   Type = "message_end"
	TurnComplete Type = "turn_complete"
	Log          Type = "log"
	Raw          Type = "raw"
)

type Event struct {
	Type    Type   `json:"type"`
	Session string `json:"session"`
	Ts      string `json:"ts"`

	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
	Usage     *Usage `json:"usage,omitempty"`
	Status    string `json:"status,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
}

type Usage struct {
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

func New(typ Type, session string) Event {
	return Event{
		Type:    typ,
		Session: session,
		Ts:      time.Now().Format(time.RFC3339Nano),
	}
}
