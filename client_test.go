package main

import (
	"context"
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

// TestDoAskSuccess verifies the happy path decodes the message field.
func TestDoAskSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"message":"hello there"}`)
	}))
	defer srv.Close()

	msg, err := newBillyClient(srv.URL).doAsk("hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "hello there" {
		t.Fatalf("got %q, want %q", msg, "hello there")
	}
}

// TestDoAskTurnInProgress verifies the /ask path maps a 409 turn-in-progress
// to the sentinel error (not a raw "ask request failed: 409").
func TestDoAskTurnInProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"session_turn_in_progress"}`)
	}))
	defer srv.Close()

	_, err := newBillyClient(srv.URL).doAsk("hello")
	if !errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected errTurnInProgress, got %v", err)
	}
}

// TestDoAskOtherErrorUnchanged verifies non-409 errors keep the original
// "ask request failed: <code>" message (success/other-error paths untouched).
func TestDoAskOtherErrorUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newBillyClient(srv.URL).doAsk("hello")
	if err == nil || errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected a plain non-409 error, got %v", err)
	}
	if !strings.Contains(err.Error(), "ask request failed: 500") {
		t.Fatalf("expected 'ask request failed: 500', got %v", err)
	}
}

// drainStream consumes a stream channel, returning the assembled text and the
// first error encountered (if any).
func drainStream(ch <-chan streamEvent) (string, error) {
	var full string
	for ev := range ch {
		if ev.Err != nil {
			return full, ev.Err
		}
		if ev.Done {
			if ev.FullText != "" {
				full = ev.FullText
			}
			return full, nil
		}
		full += ev.Chunk
	}
	return full, nil
}

// TestOpenAskStreamHappyPath verifies SSE chunks are parsed and assembled.
func TestOpenAskStreamHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"Hello \",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"world\",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true}\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	full, err := drainStream(ch)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if full != "Hello world" {
		t.Fatalf("got %q, want %q", full, "Hello world")
	}
}

// TestOpenAskStreamMalformedChunk verifies a non-JSON data line surfaces as a
// stream error rather than silently corrupting the transcript.
func TestOpenAskStreamMalformedChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	if _, err := drainStream(ch); err == nil {
		t.Fatalf("expected a parse error from malformed chunk, got nil")
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

	_, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hello")
	if !errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected errTurnInProgress from stream, got %v", err)
	}
}

// TestOpenAskStreamOtherError verifies a non-409 stream failure is a plain error.
func TestOpenAskStreamOtherError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hello")
	if err == nil || errors.Is(err, errTurnInProgress) {
		t.Fatalf("expected a plain non-409 error, got %v", err)
	}
	if !strings.Contains(err.Error(), "stream request failed: 503") {
		t.Fatalf("expected 'stream request failed: 503', got %v", err)
	}
}

// TestGetJSONNon200 verifies getJSON surfaces a non-200 as an error.
func TestGetJSONNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dest struct{ X int }
	err := newBillyClient(srv.URL).getJSON("/runtime/status", &dest)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected a 500 error, got %v", err)
	}
}
