package main

import (
	"context"
	"encoding/json"
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

// TestDoAskSuccess verifies the happy path decodes the message field, and that
// a legacy body — no brain, no approval — yields exactly the pre-contract
// result: same text, nil conductor pointers (contract §1 absence semantics).
func TestDoAskSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"message":"hello there"}`)
	}))
	defer srv.Close()

	res, err := newBillyClient(srv.URL).doAsk("hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "hello there" {
		t.Fatalf("got %q, want %q", res.Message, "hello there")
	}
	if res.Brain != nil || res.Approval != nil {
		t.Fatalf("legacy body must decode with nil Brain/Approval, got %+v / %+v", res.Brain, res.Approval)
	}
}

// TestDoAskBrainApproval verifies a conductor-aware /ask reply delivers the
// brain report and the approval request alongside the text (contract §3/§4).
func TestDoAskBrainApproval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"message": "I want to restart nginx. Reply yes to run it.",
			"brain": {
				"placement": "home",
				"provider": "ollama",
				"model_id": "qwen3.5:9b",
				"reason": "routine turn; floor small; resolved at home",
				"escalated": false,
				"pinned_home": false,
				"degraded_for_privacy": false,
				"effective_tier": "small"
			},
			"approval": {
				"pending": true,
				"id": "appr-1",
				"summary": "restart nginx",
				"command": "systemctl restart nginx",
				"target": "barn"
			}
		}`)
	}))
	defer srv.Close()

	res, err := newBillyClient(srv.URL).doAsk("restart nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "I want to restart nginx. Reply yes to run it." {
		t.Fatalf("message decoded wrong: %q", res.Message)
	}
	b := res.Brain
	if b == nil {
		t.Fatalf("expected a brain report, got nil")
	}
	if b.Placement != "home" || b.Provider != "ollama" || b.ModelID != "qwen3.5:9b" ||
		b.Reason != "routine turn; floor small; resolved at home" ||
		b.Escalated || b.PinnedHome || b.DegradedForPrivacy || b.Failsafe ||
		b.EffectiveTier != "small" {
		t.Fatalf("brain decoded wrong: %+v", b)
	}
	a := res.Approval
	if a == nil {
		t.Fatalf("expected an approval request, got nil")
	}
	if !a.Pending || a.ID != "appr-1" || a.Summary != "restart nginx" ||
		a.Command != "systemctl restart nginx" || a.Target != "barn" {
		t.Fatalf("approval decoded wrong: %+v", a)
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

// drainStreamDone consumes a stream channel until the Done event and returns it
// whole — FullText plus any Brain/Approval — or the first error encountered.
func drainStreamDone(ch <-chan streamEvent) (streamEvent, error) {
	for ev := range ch {
		if ev.Err != nil {
			return ev, ev.Err
		}
		if ev.Done {
			return ev, nil
		}
	}
	return streamEvent{}, nil
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

// TestOpenAskStreamBrainOnDoneFrame verifies the contract's preferred shape:
// the final done frame carries brain + approval, and both arrive on the Done
// event alongside the assembled text (contract §3/§4).
func TestOpenAskStreamBrainOnDoneFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"Hello \",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"world\",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true,"+
			"\"brain\":{\"placement\":\"cloud\",\"provider\":\"openrouter\",\"model_id\":\"big-model\","+
			"\"reason\":\"escalated: complex turn\",\"escalated\":true,\"pinned_home\":false,\"degraded_for_privacy\":false},"+
			"\"approval\":{\"pending\":true,\"id\":\"appr-2\",\"summary\":\"reboot barn\"}}\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	done, err := drainStreamDone(ch)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if done.FullText != "Hello world" {
		t.Fatalf("FullText = %q, want %q", done.FullText, "Hello world")
	}
	if done.Brain == nil || done.Brain.Placement != "cloud" || done.Brain.ModelID != "big-model" || !done.Brain.Escalated {
		t.Fatalf("brain not delivered from done frame: %+v", done.Brain)
	}
	if done.Approval == nil || !done.Approval.Pending || done.Approval.ID != "appr-2" || done.Approval.Summary != "reboot barn" {
		t.Fatalf("approval not delivered from done frame: %+v", done.Approval)
	}
}

// TestOpenAskStreamBrainOnEarlyFrame verifies the "any frame; first non-null
// wins" rule (contract §3): a brain on an early chunk frame followed by a bare
// done frame is still delivered on the Done event.
func TestOpenAskStreamBrainOnEarlyFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"early\",\"done\":false,"+
			"\"brain\":{\"placement\":\"home\",\"provider\":\"ollama\",\"model_id\":\"qwen3.5:9b\","+
			"\"reason\":\"first frame\",\"escalated\":false,\"pinned_home\":false,\"degraded_for_privacy\":false}}\n\n")
		// A later frame with a DIFFERENT brain must lose to the first one.
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true,"+
			"\"brain\":{\"placement\":\"cloud\",\"provider\":\"openrouter\",\"model_id\":\"other\","+
			"\"reason\":\"second frame\",\"escalated\":true,\"pinned_home\":false,\"degraded_for_privacy\":false}}\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	done, err := drainStreamDone(ch)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if done.FullText != "early" {
		t.Fatalf("FullText = %q, want %q", done.FullText, "early")
	}
	if done.Brain == nil || done.Brain.Reason != "first frame" || done.Brain.Placement != "home" {
		t.Fatalf("first non-nil brain must win, got %+v", done.Brain)
	}
	if done.Approval != nil {
		t.Fatalf("no approval was sent, got %+v", done.Approval)
	}
}

// TestOpenAskStreamLegacyNilBrain verifies a legacy stream — frames carrying
// only chunk/done — behaves exactly as today: text assembled, nil Brain, nil
// Approval on the Done event (contract §1 / degradation matrix row 1).
func TestOpenAskStreamLegacyNilBrain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"legacy \",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"reply\",\"done\":false}\n\n")
		_, _ = io.WriteString(w, "data: {\"chunk\":\"\",\"done\":true}\n\n")
	}))
	defer srv.Close()

	ch, err := newBillyClient(srv.URL).openAskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	done, err := drainStreamDone(ch)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if done.FullText != "legacy reply" {
		t.Fatalf("FullText = %q, want %q", done.FullText, "legacy reply")
	}
	if done.Brain != nil || done.Approval != nil {
		t.Fatalf("legacy stream must deliver nil Brain/Approval, got %+v / %+v", done.Brain, done.Approval)
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

	res, err := c.doAsk("hi")
	if err != nil {
		t.Fatalf("doAsk over socket: %v", err)
	}
	if res.Message != "socket ok" {
		t.Fatalf("doAsk over socket = %q, want %q", res.Message, "socket ok")
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

// TestLLMConfigRoutingMode verifies routing_mode decodes when present and
// stays "" on a legacy config body — the "" sentinel is what keeps a legacy
// runtime on today's pinned display (contract §2).
func TestLLMConfigRoutingMode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"auto", `{"provider":"ollama","model":"qwen3.5:9b","configured":true,"routing_mode":"auto"}`, "auto"},
		{"pinned", `{"provider":"ollama","model":"qwen3.5:9b","configured":true,"routing_mode":"pinned"}`, "pinned"},
		{"legacy absent", `{"provider":"ollama","model":"qwen3.5:9b","configured":true}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			cfg, err := newBillyClient(srv.URL).LLMConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.RoutingMode != tc.want {
				t.Fatalf("RoutingMode = %q, want %q", cfg.RoutingMode, tc.want)
			}
		})
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

// TestSetRoutingModePostsModeOnly verifies the toggle is a mode-only POST
// (contract v2 §9): the body carries routing_mode and NOTHING else — no
// provider, no model — so the pin cannot be disturbed; and the runtime's
// authoritative config reply decodes back, routing_mode included.
func TestSetRoutingModePostsModeOnly(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/llm/config" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("body decode: %v", err)
		}
		_, _ = io.WriteString(w, `{"provider":"ollama","model":"qwen3.5:9b","configured":true,"routing_mode":"pinned"}`)
	}))
	defer srv.Close()

	cfg, err := newBillyClient(srv.URL).SetRoutingMode("pinned")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotBody) != 1 || gotBody["routing_mode"] != "pinned" {
		t.Fatalf("body = %v, want exactly {routing_mode: pinned}", gotBody)
	}
	if cfg.RoutingMode != "pinned" || cfg.Provider != "ollama" || cfg.Model != "qwen3.5:9b" {
		t.Fatalf("reply decoded wrong: %+v", cfg)
	}
}

// TestSetRoutingModeErrorSurfaces verifies a rejected mode set (e.g. a 400
// invalid_routing_mode) comes back as an error, never a silent success.
func TestSetRoutingModeErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	if _, err := newBillyClient(srv.URL).SetRoutingMode("sideways"); err == nil {
		t.Fatalf("expected an error from a 400, got nil")
	}
}

// floorFixture is the contract v2 §10 GET shape used across the floor tests.
const floorFixture = `{"tiers":["small","medium","large"],"default_floor":"small",` +
	`"roles":{"chat":"small","coder":"large","companion":"small","sysadmin":"medium"}}`

// TestBrainFloorsDecodeAndAbsent verifies the GET decodes the ordered tier
// vocabulary, the effective table, and the default floor — and that a 404 maps
// to the errFloorSurfaceAbsent sentinel (legacy runtime), never a plain error.
func TestBrainFloorsDecodeAndAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/llm/brain-floors" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, floorFixture)
	}))
	defer srv.Close()

	f, err := newBillyClient(srv.URL).BrainFloors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Tiers) != 3 || f.Tiers[0] != "small" || f.Tiers[2] != "large" {
		t.Fatalf("tiers decoded wrong (order matters): %v", f.Tiers)
	}
	if f.DefaultFloor != "small" {
		t.Fatalf("default_floor = %q, want small", f.DefaultFloor)
	}
	if f.Roles["coder"] != "large" || f.Roles["sysadmin"] != "medium" || len(f.Roles) != 4 {
		t.Fatalf("roles decoded wrong: %v", f.Roles)
	}

	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv404.Close()
	if _, err := newBillyClient(srv404.URL).BrainFloors(); !errors.Is(err, errFloorSurfaceAbsent) {
		t.Fatalf("404 must map to errFloorSurfaceAbsent, got %v", err)
	}
}

// TestSetBrainFloorPostsOneRole verifies the write is a one-role POST
// (contract v2 §10): the role rides the path, the body is exactly {"tier": …},
// and the runtime's post-write table decodes back as the took-effect proof.
func TestSetBrainFloorPostsOneRole(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("body decode: %v", err)
		}
		_, _ = io.WriteString(w, `{"tiers":["small","medium","large"],"default_floor":"small",`+
			`"roles":{"chat":"small","coder":"large","companion":"small","sysadmin":"large"}}`)
	}))
	defer srv.Close()

	f, err := newBillyClient(srv.URL).SetBrainFloor("sysadmin", "large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/llm/brain-floors/sysadmin" {
		t.Fatalf("path = %q, want the role in the path", gotPath)
	}
	if len(gotBody) != 1 || gotBody["tier"] != "large" {
		t.Fatalf("body = %v, want exactly {tier: large}", gotBody)
	}
	if f.Roles["sysadmin"] != "large" {
		t.Fatalf("post-write table not adopted: %v", f.Roles)
	}
}

// TestSetBrainFloorErrors verifies a 400 (e.g. unknown_tier) surfaces as an
// error and a 404 maps to the absent-surface sentinel.
func TestSetBrainFloorErrors(t *testing.T) {
	srv400 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv400.Close()
	if _, err := newBillyClient(srv400.URL).SetBrainFloor("coder", "sideways"); err == nil {
		t.Fatalf("expected an error from a 400, got nil")
	}

	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv404.Close()
	if _, err := newBillyClient(srv404.URL).SetBrainFloor("coder", "large"); !errors.Is(err, errFloorSurfaceAbsent) {
		t.Fatalf("404 must map to errFloorSurfaceAbsent, got %v", err)
	}
}
