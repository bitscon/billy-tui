package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeResp builds an *http.Response with the given status and body for testing
// the 409 classifier without a live server.
func makeResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestIs409TurnInProgress(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"bare 409 no body", http.StatusConflict, "", true},
		{"409 with code", http.StatusConflict, `{"code":"session_turn_in_progress"}`, true},
		{"409 with nested detail code", http.StatusConflict, `{"detail":{"code":"session_turn_in_progress"}}`, true},
		{"409 non-json body", http.StatusConflict, "Conflict", true},
		{"409 with different code", http.StatusConflict, `{"code":"something_else"}`, false},
		{"200 ok", http.StatusOK, `{"message":"hi"}`, false},
		{"500 error", http.StatusInternalServerError, "boom", false},
		{"404 not found", http.StatusNotFound, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := is409TurnInProgress(makeResp(tc.status, tc.body))
			if got != tc.want {
				t.Fatalf("is409TurnInProgress(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestRequestAskTurnInProgress verifies the /ask path maps a 409 turn-in-progress
// to the sentinel error (not a raw "ask request failed: 409").
func TestRequestAskTurnInProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"session_turn_in_progress"}`)
	}))
	defer srv.Close()

	_, err := requestAsk("hello", "sess-1", srv.URL)
	if !errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected errTurnInProgress, got %v", err)
	}
}

// TestRequestAskOtherErrorUnchanged verifies non-409 errors keep the original
// "ask request failed: <code>" message (success/other-error paths untouched).
func TestRequestAskOtherErrorUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := requestAsk("hello", "sess-1", srv.URL)
	if err == nil || errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected a plain non-409 error, got %v", err)
	}
	if !strings.Contains(err.Error(), "ask request failed: 500") {
		t.Fatalf("expected 'ask request failed: 500', got %v", err)
	}
}

// TestOpenAskStreamTurnInProgress verifies the /ask/stream path maps a 409
// turn-in-progress to the sentinel error.
func TestOpenAskStreamTurnInProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"detail":{"code":"session_turn_in_progress"}}`)
	}))
	defer srv.Close()

	_, err := openAskStream("hello", "sess-1", srv.URL)
	if !errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected errTurnInProgress from stream, got %v", err)
	}
}
