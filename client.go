package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type billyClient struct {
	baseURL   string
	sessionID string
	http      *http.Client
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
	body, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": c.sessionID,
	})
	resp, err := c.http.Post(c.baseURL+"/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Message, nil
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
