// Package relay provides a best-effort client for forwarding agent activity
// and token usage to the only-claws API.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/christmas-island/hive-server/internal/log"
)

// UsageReport holds token usage data to relay to only-claws.
type UsageReport struct {
	Model            string  `json:"model"`
	InputTokens      int     `json:"inputTokens"`
	OutputTokens     int     `json:"outputTokens"`
	CacheReadTokens  int     `json:"cacheReadTokens"`
	CacheWriteTokens int     `json:"cacheWriteTokens"`
	TotalTokens      int     `json:"totalTokens"`
	EstimatedCostUsd float64 `json:"estimatedCostUsd"`
	SessionID        string  `json:"sessionId"`
	Timestamp        string  `json:"timestamp"`
}

// Client relays agent data to the only-claws API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a relay Client. If baseURL is empty, all methods are no-ops.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// UpdateStatus sends an agent status update to only-claws.
// Errors are logged but never returned (best-effort).
func (c *Client) UpdateStatus(ctx context.Context, agentID, status, activity string) error {
	if c.baseURL == "" {
		return nil
	}

	body := map[string]string{
		"status":   status,
		"activity": activity,
	}
	url := fmt.Sprintf("%s/agents/%s/status", c.baseURL, agentID)
	if err := c.do(ctx, http.MethodPatch, url, body); err != nil {
		log.Error("relay: update status: ", err)
	}
	return nil
}

// RecordUsage sends token usage data to only-claws.
// Errors are logged but never returned (best-effort).
func (c *Client) RecordUsage(ctx context.Context, agentID string, usage UsageReport) error {
	if c.baseURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/agents/%s/usage", c.baseURL, agentID)
	if err := c.do(ctx, http.MethodPost, url, usage); err != nil {
		log.Error("relay: record usage: ", err)
	}
	return nil
}

// do performs an HTTP request with JSON body and checks for a 2xx response.
func (c *Client) do(ctx context.Context, method, url string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d", method, url, resp.StatusCode)
	}
	return nil
}
