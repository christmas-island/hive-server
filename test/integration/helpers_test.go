//go:build integration

// Package integration provides integration tests that exercise the full
// hive-server stack (HTTP handler + CockroachDB) via the exported
// pkg/testharness package. Each test gets an isolated server + database.
//
// Run with: go test -tags integration -count=1 -v -timeout 300s ./test/integration/...
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// client is a lightweight HTTP helper for integration tests.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(base, token string) *client {
	return &client{
		base:  base,
		token: token,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// do sends an HTTP request and returns status, body bytes, and error.
func (c *client) do(method, path string, body any, headers ...header) (int, []byte, error) {
	var rb io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal: %w", err)
		}
		rb = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rb)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for _, h := range headers {
		req.Header.Set(h.key, h.value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, nil
}

type header struct{ key, value string }

func agentID(id string) header { return header{"X-Agent-ID", id} }

// decode unmarshals JSON response bytes into T.
func decode[T any](data []byte) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}
