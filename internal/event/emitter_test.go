package event

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCallbackEmitter_Emit_UnreachableURL ensures that a callback URL that
// cannot be reached (nothing listening) does not cause Emit to return an
// error or panic. Callback delivery failures must be non-fatal so a run
// against a dead callback URL still completes.
func TestCallbackEmitter_Emit_UnreachableURL(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Emit panicked on unreachable URL: %v", r)
		}
	}()

	// Port 0 on localhost with no listener should reliably fail to connect.
	e := NewCallbackEmitter("http://127.0.0.1:0/callback")

	err := e.Emit(New(Log, "test-session"))
	if err != nil {
		t.Fatalf("Emit returned an error for unreachable URL, want nil: %v", err)
	}
}

// TestCallbackEmitter_Emit_ServerError ensures that a callback URL that
// responds with a 500 status still results in a non-fatal Emit call.
func TestCallbackEmitter_Emit_ServerError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Emit panicked on server 500 response: %v", r)
		}
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	e := NewCallbackEmitter(srv.URL)

	err := e.Emit(New(Log, "test-session"))
	if err != nil {
		t.Fatalf("Emit returned an error for 500 response, want nil: %v", err)
	}
}
