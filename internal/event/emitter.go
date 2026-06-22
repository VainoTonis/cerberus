package event

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Emitter receives structured events from the stream processor.
type Emitter interface {
	Emit(Event) error
	Close() error
}

// TextEmitter prints human-readable output to stdout.
// Reproduces the original cerberus terminal behavior:
// only text deltas and non-JSON log lines are shown.
type TextEmitter struct {
	session     string
	atLineStart bool
}

func NewTextEmitter(session string) *TextEmitter {
	return &TextEmitter{session: session, atLineStart: true}
}

func (e *TextEmitter) Emit(ev Event) error {
	switch ev.Type {
	case TextDelta:
		if e.atLineStart {
			fmt.Printf("[%s] ", e.session)
		}
		fmt.Print(ev.Content)
		e.atLineStart = len(ev.Content) > 0 && ev.Content[len(ev.Content)-1] == '\n'
	case Log:
		fmt.Printf("[%s] %s\n", e.session, ev.Content)
		e.atLineStart = true
	case TurnComplete:
		if !e.atLineStart {
			fmt.Println()
		}
		e.atLineStart = true
	}
	return nil
}

func (e *TextEmitter) Close() error {
	if !e.atLineStart {
		fmt.Println()
	}
	return nil
}

// JSONLEmitter writes one JSON line per event to a writer.
type JSONLEmitter struct {
	w io.Writer
}

func NewJSONLEmitter(w io.Writer) *JSONLEmitter {
	if w == nil {
		w = os.Stdout
	}
	return &JSONLEmitter{w: w}
}

func (e *JSONLEmitter) Emit(ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.w, "%s\n", data)
	return err
}

func (e *JSONLEmitter) Close() error { return nil }

// CallbackEmitter POSTs each event as JSON to a URL.
// Errors are logged to stderr but do not fail the stream.
type CallbackEmitter struct {
	url    string
	client *http.Client
}

func NewCallbackEmitter(url string) *CallbackEmitter {
	return &CallbackEmitter{
		url:    url,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (e *CallbackEmitter) Emit(ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	resp, err := e.client.Post(e.url, "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "callback POST error: %v\n", err)
		return nil
	}
	resp.Body.Close()
	return nil
}

func (e *CallbackEmitter) Close() error { return nil }

// MultiEmitter fans out events to multiple emitters.
type MultiEmitter struct {
	emitters []Emitter
}

func NewMultiEmitter(emitters ...Emitter) *MultiEmitter {
	return &MultiEmitter{emitters: emitters}
}

func (m *MultiEmitter) Emit(ev Event) error {
	for _, e := range m.emitters {
		if err := e.Emit(ev); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiEmitter) Close() error {
	for _, e := range m.emitters {
		e.Close()
	}
	return nil
}

// SilentEmitter discards all events (no-op emitter for JSON mode).
type SilentEmitter struct{}

func NewSilentEmitter() *SilentEmitter {
	return &SilentEmitter{}
}

func (s *SilentEmitter) Emit(ev Event) error {
	return nil
}

func (s *SilentEmitter) Close() error {
	return nil
}
