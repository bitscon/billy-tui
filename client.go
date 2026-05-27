package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type billyClient struct {
	baseURL   string
	sessionID string
	http      *http.Client
}

type streamEvent struct {
	Chunk    string
	FullText string
	Done     bool
	Err      error
}

func newBillyClient(baseURL string) *billyClient {
	return &billyClient{
		baseURL:   baseURL,
		sessionID: fmt.Sprintf("tui-%d", time.Now().UnixNano()),
		http:      &http.Client{Timeout: 30 * time.Second},
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

func (c *billyClient) Ask(prompt string) (string, error) {
	return requestAsk(prompt, c.sessionID, c.baseURL)
}

func requestAsk(prompt string, sessionID string, baseURL string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": sessionID,
	})
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Post(baseURL+"/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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

func ask(prompt string, sessionID string, baseURL string) tea.Cmd {
	return func() tea.Msg {
		message, err := requestAsk(prompt, sessionID, baseURL)
		if err != nil {
			return errMsg{text: "⚠️  " + err.Error()}
		}
		return responseMsg{text: message}
	}
}

func askStream(prompt string, sessionID string, baseURL string) tea.Cmd {
	return func() tea.Msg {
		events, err := openAskStream(prompt, sessionID, baseURL)
		if err != nil {
			return StreamErrMsg{Prompt: prompt, Err: err}
		}
		return nextStreamMessage(events, prompt)
	}
}

func openAskStream(prompt string, sessionID string, baseURL string) (<-chan streamEvent, error) {
	body, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": sessionID,
	})
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Post(baseURL+"/ask/stream", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("stream request failed: %d", resp.StatusCode)
	}

	events := make(chan streamEvent)
	go func() {
		defer close(events)
		defer resp.Body.Close()

		var fullText strings.Builder
		scanner := bufio.NewScanner(resp.Body)
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

func nextStreamMessage(events <-chan streamEvent, prompt string) tea.Msg {
	event, ok := <-events
	if !ok {
		return StreamDoneMsg{FullText: ""}
	}
	if event.Err != nil {
		return StreamErrMsg{Prompt: prompt, Err: event.Err}
	}
	if event.Done {
		return StreamDoneMsg{FullText: event.FullText}
	}
	return StreamChunkMsg{Chunk: event.Chunk, Prompt: prompt, events: events}
}

func (c *billyClient) RuntimeStatus() (map[string]string, error) {
	resp, err := c.http.Get(c.baseURL + "/runtime/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *billyClient) TelemetryEvents() ([]map[string]interface{}, error) {
	resp, err := c.http.Get(c.baseURL + "/telemetry/events")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
