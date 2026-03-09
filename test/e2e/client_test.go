//go:build e2e

// Package e2e provides end-to-end smoke tests for hive-server.
// Run with: go test -tags e2e -count=1 -v ./test/e2e/...
//
// Required environment variables:
//   E2E_TARGET_URL — hive-server base URL (e.g. http://localhost:8080)
//   E2E_TOKEN      — Bearer token (optional; leave empty when HIVE_TOKEN is unset)
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// hiveClient is a minimal HTTP client for hive-server E2E tests.
type hiveClient struct {
	base  string
	token string
	http  *http.Client
}

// reqOpt modifies an outgoing HTTP request before it is sent.
type reqOpt func(*http.Request)

// withNoAuth removes the Authorization header so the request is unauthenticated.
func withNoAuth() reqOpt {
	return func(r *http.Request) { r.Header.Del("Authorization") }
}

// withBadToken sets a deliberately invalid Bearer token.
func withBadToken() reqOpt {
	return func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer __e2e__-invalid-token-xyz")
	}
}

// withAgentID sets the X-Agent-ID header.
func withAgentID(id string) reqOpt {
	return func(r *http.Request) { r.Header.Set("X-Agent-ID", id) }
}

// newHiveClient creates a new E2E HTTP client targeting base with the given token.
func newHiveClient(base, token string) *hiveClient {
	return &hiveClient{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// do executes an HTTP request and returns (statusCode, responseBody, error).
// The client's Bearer token is set by default; use reqOpt to override.
func (c *hiveClient) do(method, path string, body any, opts ...reqOpt) (int, []byte, error) {
	var rb io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal body: %w", err)
		}
		rb = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.base+path, rb)
	if err != nil {
		return 0, nil, fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for _, o := range opts {
		o(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read body: %w", err)
	}
	return resp.StatusCode, b, nil
}

// decodeJSON unmarshals JSON bytes into a value of type T.
func decodeJSON[T any](b []byte) (T, error) {
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return v, fmt.Errorf("decode JSON: %w (body: %.300s)", err, b)
	}
	return v, nil
}
