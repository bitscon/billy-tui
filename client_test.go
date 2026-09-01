package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestOpenAskStreamLongReply verifies a single SSE frame far larger than
// bufio.Scanner's default 64KiB line cap arrives intact. Before the cap was
// raised this errored mid-stream (bufio.ErrTooLong) and cascaded into a
// blocking /ask retry that collided with the runtime's one-turn guard — a
// long answer presented as a failure (Modernization 8).
func TestOpenAskStreamLongReply(t *testing.T) {
	long := strings.Repeat("a", 200*1024) // one frame, well past the 64KiB default cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\""+long+"\",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true}\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	full, err := drainStream(ch)
	if err != nil {
		t.Fatalf("long reply broke the stream: %v", err)
	}
	if full != long {
		t.Fatalf("long reply arrived corrupted: got %d bytes, want %d", len(full), len(long))
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

// TestUnixSocketPath verifies the scheme parser: which addresses select the
// Unix-socket transport and the socket path each yields.
func TestUnixSocketPath(t *testing.T) {
	cases := []struct {
		addr     string
		wantPath string
		wantOK   bool
	}{
		{"unix:///home/billyb/.billy/sock/billy.sock", "/home/billyb/.billy/sock/billy.sock", true},
		{"unix:/tmp/b.sock", "/tmp/b.sock", true},
		{"http://localhost:5001", "", false},
		{"https://example:443", "", false},
		{"localhost:5001", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			gotPath, gotOK := unixSocketPath(tc.addr)
			if gotOK != tc.wantOK || gotPath != tc.wantPath {
				t.Fatalf("unixSocketPath(%q) = (%q, %v), want (%q, %v)",
					tc.addr, gotPath, gotOK, tc.wantPath, tc.wantOK)
			}
		})
	}
}

// TestResolveTransport verifies the TCP path is left untouched (address passes
// through, no custom transport) while a unix:// address selects the socket
// transport and the fixed dummy base URL.
func TestResolveTransport(t *testing.T) {
	// TCP: address passes through verbatim, transport stays nil (net/http's
	// default) — this is the pre-existing behaviour, unchanged.
	for _, addr := range []string{"http://localhost:5001", "https://example:443"} {
		base, rt := resolveTransport(addr)
		if base != addr {
			t.Fatalf("resolveTransport(%q) base = %q, want %q", addr, base, addr)
		}
		if rt != nil {
			t.Fatalf("resolveTransport(%q) rt = %v, want nil (default transport)", addr, rt)
		}
	}
	// Unix: dummy base URL and a non-nil socket-dialing transport.
	base, rt := resolveTransport("unix:///tmp/b.sock")
	if base != unixBaseURL {
		t.Fatalf("resolveTransport(unix) base = %q, want %q", base, unixBaseURL)
	}
	if rt == nil {
		t.Fatalf("resolveTransport(unix) rt = nil, want a socket transport")
	}
}

// TestUnixSocketTransportEndToEnd proves the client genuinely speaks HTTP over an
// AF_UNIX socket: it stands up a real Unix-socket listener with an http.Server and
// drives all three request kinds the TUI uses — a GET (/health), a POST (/ask),
// and an SSE stream (/ask/stream) — through the built client. This is the
// reproduction that separates "the code looks right" from "the socket carries the
// session" (AGENT_OS §21.4 evidence standard).
func TestUnixSocketTransportEndToEnd(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "b.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sock, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ask", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"message":"socket ok"}`)
	})
	mux.HandleFunc("/ask/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"sock \",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"stream\",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true}\n\n")
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	c := newBillyClient("unix://" + sock)
	if c.baseURL != unixBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, unixBaseURL)
	}

	if err := c.Health(); err != nil {
		t.Fatalf("Health over socket: %v", err)
	}

	msg, err := c.doAsk("hi")
	if err != nil {
		t.Fatalf("doAsk over socket: %v", err)
	}
	if msg != "socket ok" {
		t.Fatalf("doAsk over socket = %q, want %q", msg, "socket ok")
	}

	ch, err := c.openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("openAskStream over socket: %v", err)
	}
	full, err := drainStream(ch)
	if err != nil {
		t.Fatalf("stream over socket: %v", err)
	}
	if full != "sock stream" {
		t.Fatalf("stream over socket = %q, want %q", full, "sock stream")
	}
}

// TestLiveSocketSession is an opt-in integration check against a running
// billy-runtime over its real Unix domain socket. It stays skipped during the
// ordinary hermetic suite (`go test ./...`) so CI needs no live service, and gives
// the operator a one-command proof on the barn:
//
//	BILLY_LIVE_SOCKET=/home/billyb/.billy/sock/billy.sock \
//	    go test -run TestLiveSocketSession -v
//
// It exercises the client's own socket path (Health + GET /api/v1/llm/config) and
// deliberately avoids a live LLM turn, so it is cheap, deterministic, and never
// trips the runtime's one-turn-at-a-time guard.
func TestLiveSocketSession(t *testing.T) {
	sock := os.Getenv("BILLY_LIVE_SOCKET")
	if sock == "" {
		t.Skip("set BILLY_LIVE_SOCKET=/path/to/billy.sock to exercise a running billy-runtime over its socket")
	}
	c := newBillyClient("unix://" + sock)
	if err := c.Health(); err != nil {
		t.Fatalf("Health over live socket %s: %v", sock, err)
	}
	cfg, err := c.LLMConfig()
	if err != nil {
		t.Fatalf("LLMConfig over live socket: %v", err)
	}
	if !cfg.Configured {
		t.Fatalf("LLMConfig over live socket reports unconfigured: %+v", cfg)
	}
	t.Logf("live socket OK — provider=%s model=%s", cfg.Provider, cfg.Model)
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
