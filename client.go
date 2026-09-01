package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// errTurnInProgress is returned when billy-runtime rejects a request with
// HTTP 409 / code "session_turn_in_progress" — the correct one-turn-at-a-time
// guard. It is a benign, transient condition (Billy is still working on the
// previous turn), not a hard failure, so callers surface a friendly state
// rather than an alarming error.
var errTurnInProgress = errors.New("session_turn_in_progress")

// turnInProgressMessage is the friendly, non-alarming line shown to the operator
// when a 409 turn-in-progress is hit.
const turnInProgressMessage = "⏳ Billy is still thinking — give him a moment…"

// is409TurnInProgress reports whether a non-200 response is the runtime's
// one-turn-at-a-time guard. A 409 alone is treated as turn-in-progress; if a
// JSON body is present it must carry code "session_turn_in_progress" to qualify,
// so unrelated 409s (should any exist) are not misclassified.
func is409TurnInProgress(resp *http.Response) bool {
	if resp.StatusCode != http.StatusConflict {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		// No readable body — a bare 409 from /ask is the turn guard.
		return true
	}
	var payload struct {
		Code   string `json:"code"`
		Detail struct {
			Code string `json:"code"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		// Non-JSON 409 body: still a conflict on a single-turn endpoint.
		return true
	}
	if payload.Code == "session_turn_in_progress" || payload.Detail.Code == "session_turn_in_progress" {
		return true
	}
	// A 409 with a different code is not our turn guard.
	return payload.Code == "" && payload.Detail.Code == ""
}

type billyClient struct {
	baseURL    string
	sessionID  string
	http       *http.Client
	streamHTTP *http.Client
}

type streamEvent struct {
	Chunk    string
	FullText string
	Done     bool
	Err      error
}

// unixBaseURL is the fixed authority used for requests over the Unix socket.
// There is no real host on an AF_UNIX connection, so only the path prefixes
// (/health, /ask, …) matter; net/http still needs a syntactically valid URL to
// build the request line and Host header, and billy-runtime does not gate on Host.
const unixBaseURL = "http://billy"

// unixSocketPath reports whether addr selects the Unix-socket transport and, if
// so, the filesystem path of the socket. It accepts the URL form
// "unix:///abs/path/to.sock" (canonical, matches the curl --unix-socket / Docker
// convention) and the shorthand "unix:/abs/path/to.sock". Anything else —
// http://…, https://…, a bare host:port — is not a socket address.
func unixSocketPath(addr string) (string, bool) {
	if p, ok := strings.CutPrefix(addr, "unix://"); ok {
		return p, true
	}
	if p, ok := strings.CutPrefix(addr, "unix:"); ok {
		return p, true
	}
	return "", false
}

// resolveTransport interprets a billy-runtime address and returns the base URL to
// prefix onto request paths together with the RoundTripper that carries them.
//
//	http://host:port (or https://…) — ordinary TCP: the base URL is the address as
//	    given and the transport is nil, so net/http uses its default pooled
//	    transport. This path is byte-for-byte the pre-existing behaviour.
//	unix:///path/to/socket — AF_UNIX: every request dials the socket instead of a
//	    TCP host and the base URL becomes unixBaseURL. This is the Go analogue of
//	    `curl --unix-socket`.
//
// The Unix path carries the peer's kernel credentials (SO_PEERCRED), so a local or
// SSH-forwarded socket connection lets billy-runtime resolve the caller's principal
// from the transport with no text challenge — the operator (uid 1000) over the
// socket is recognised as the operator, any other uid as that principal. The TUI
// does nothing identity-specific; the transport speaks (Modular Identity P7/P10).
func resolveTransport(addr string) (baseURL string, rt http.RoundTripper) {
	socketPath, ok := unixSocketPath(addr)
	if !ok {
		return addr, nil
	}
	return unixBaseURL, &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
}

// newBillyClient builds a client for the given address. addr is either an
// http(s):// URL (TCP, the default) or a unix:// socket address (see
// resolveTransport). The chosen transport is applied to both HTTP clients so that
// /ask/stream (SSE) rides the same connection family as the plain endpoints.
func newBillyClient(addr string) *billyClient {
	baseURL, rt := resolveTransport(addr)
	return &billyClient{
		baseURL:   baseURL,
		sessionID: fmt.Sprintf("tui-%d", time.Now().UnixNano()),
		http:      &http.Client{Timeout: 30 * time.Second, Transport: rt},
		// Streaming carries no total deadline: a slow local model (e.g. qwen)
		// can take minutes per turn. The per-turn context (see askStream) bounds
		// it with a generous ceiling and provides the cancel handle for `esc`.
		streamHTTP: &http.Client{Transport: rt},
	}
}

func (c *billyClient) Health() error {
	resp, err := c.http.Get(c.baseURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}
	return nil
}

// doAsk performs a blocking (non-streaming) turn against /ask using the pooled
// client. Used as the fallback when /ask/stream is unavailable.
func (c *billyClient) doAsk(prompt string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": c.sessionID,
	})
	resp, err := c.http.Post(c.baseURL+"/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if is409TurnInProgress(resp) {
			return "", errTurnInProgress
		}
		return "", fmt.Errorf("ask request failed: %d", resp.StatusCode)
	}
	var result struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Message, nil
}

func ask(c *billyClient, prompt string, gen int) tea.Cmd {
	return func() tea.Msg {
		message, err := c.doAsk(prompt)
		if err != nil {
			if errors.Is(err, errTurnInProgress) {
				return turnInProgressMsg{Gen: gen}
			}
			return errMsg{text: "⚠️  " + err.Error(), Gen: gen}
		}
		return responseMsg{text: message, Gen: gen}
	}
}

func askStream(ctx context.Context, c *billyClient, prompt string, gen int) tea.Cmd {
	return func() tea.Msg {
		events, err := c.openAskStream(ctx, prompt)
		if err != nil {
			return StreamErrMsg{Prompt: prompt, Err: err, Gen: gen}
		}
		return nextStreamMessage(events, prompt, gen)
	}
}

func (c *billyClient) openAskStream(ctx context.Context, prompt string) (<-chan streamEvent, error) {
	body, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": c.sessionID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ask/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		turnInProgress := is409TurnInProgress(resp)
		resp.Body.Close()
		if turnInProgress {
			return nil, errTurnInProgress
		}
		return nil, fmt.Errorf("stream request failed: %d", resp.StatusCode)
	}

	events := make(chan streamEvent)
	go func() {
		defer close(events)
		defer resp.Body.Close()

		var fullText strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		// The runtime sends each SSE frame as one "data: {…}" line, and a frame can
		// carry a very large chunk (the server generates the whole reply, then
		// re-chunks it). bufio.Scanner's default 64KiB line cap made one long reply
		// kill the stream mid-turn (ErrTooLong), which then cascaded into a blocking
		// /ask retry against the runtime's one-turn guard. 10MiB of headroom covers
		// any real text reply; a line beyond it still errors rather than hanging.
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			payloadRaw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payloadRaw == "" {
				continue
			}

			var payload struct {
				Chunk string `json:"chunk"`
				Done  bool   `json:"done"`
			}
			if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
				events <- streamEvent{Err: err, Done: true}
				return
			}

			if payload.Chunk != "" {
				fullText.WriteString(payload.Chunk)
				events <- streamEvent{Chunk: payload.Chunk}
			}
			if payload.Done {
				events <- streamEvent{Done: true, FullText: fullText.String()}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			events <- streamEvent{Err: err, Done: true}
		}
	}()
	return events, nil
}

func nextStreamMessage(events <-chan streamEvent, prompt string, gen int) tea.Msg {
	event, ok := <-events
	if !ok {
		return StreamDoneMsg{FullText: "", Gen: gen}
	}
	if event.Err != nil {
		return StreamErrMsg{Prompt: prompt, Err: event.Err, Gen: gen}
	}
	if event.Done {
		return StreamDoneMsg{FullText: event.FullText, Gen: gen}
	}
	return StreamChunkMsg{Chunk: event.Chunk, Prompt: prompt, Gen: gen, events: events}
}

// ── sidebar data types (mirror billy-runtime response models) ────────────────

// RuntimeStatus mirrors RuntimeStatusResponse — a nested object, not a flat map.
type RuntimeStatus struct {
	Service          string `json:"service"`
	RuntimePhase     int    `json:"runtime_phase"`
	LifecycleState   string `json:"lifecycle_state"`
	ExecutionEnabled bool   `json:"execution_enabled"`
	RuntimeContract  string `json:"runtime_contract"`
	CurrentStage     string `json:"current_stage"`
	TargetStage      string `json:"target_stage"`
	Telemetry        struct {
		Total    int `json:"total"`
		Allowed  int `json:"allowed"`
		Rejected int `json:"rejected"`
		Skipped  int `json:"skipped"`
	} `json:"telemetry"`
}

// LLMConfig mirrors GET /api/v1/llm/config.
type LLMConfig struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	Configured bool   `json:"configured"`
}

// GuidanceDecision is one entry from GET /reconciliation/recent.
type GuidanceDecision struct {
	ArtifactID   string  `json:"artifact_id"`
	ArtifactType string  `json:"artifact_type"`
	ReasonCode   string  `json:"reason_code"`
	Action       string  `json:"action"`
	Timestamp    float64 `json:"timestamp"`
}

// ActiveJob is one item from GET /api/v1/execution/jobs/active.
type ActiveJob struct {
	ExecutionJobID string `json:"execution_job_id"`
	CurrentState   string `json:"current_state"`
	CapabilityID   string `json:"capability_id"`
	ExecutorID     string `json:"executor_id"`
}

// getJSON performs a GET against the runtime and decodes the body into dest.
func (c *billyClient) getJSON(path string, dest interface{}) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (c *billyClient) RuntimeStatus() (*RuntimeStatus, error) {
	var s RuntimeStatus
	if err := c.getJSON("/runtime/status", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *billyClient) LLMConfig() (*LLMConfig, error) {
	var cfg LLMConfig
	if err := c.getJSON("/api/v1/llm/config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *billyClient) ReconciliationRecent(limit int) ([]GuidanceDecision, error) {
	var out []GuidanceDecision
	if err := c.getJSON(fmt.Sprintf("/reconciliation/recent?limit=%d", limit), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *billyClient) ActiveJobs() ([]ActiveJob, error) {
	var payload struct {
		Items []ActiveJob `json:"items"`
	}
	if err := c.getJSON("/api/v1/execution/jobs/active", &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

// LLMProviderInfo mirrors GET /api/v1/llm/models entries.
type LLMProviderInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	DefaultModel   string   `json:"default_model"`
	RequiresAPIKey bool     `json:"requires_api_key"`
	Models         []string `json:"models"`
}

func (c *billyClient) ListModels() ([]LLMProviderInfo, error) {
	var out []LLMProviderInfo
	if err := c.getJSON("/api/v1/llm/models", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetLLMConfig switches the active provider/model via the sanctioned operator API
// (POST /api/v1/llm/config) — the immediate rebuild+swap path. An empty model
// asks the runtime to use the provider default.
func (c *billyClient) SetLLMConfig(provider, model, baseURL string) (*LLMConfig, error) {
	payload := map[string]string{"provider": provider}
	if model != "" {
		payload["model"] = model
	}
	if baseURL != "" {
		payload["base_url"] = baseURL
	}
	body, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/api/v1/llm/config", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("set llm config: %d", resp.StatusCode)
	}
	var cfg LLMConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
