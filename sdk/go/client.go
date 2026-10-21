// Package relay provides a small retry-safe HTTP client for the Relay control plane.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/propagation"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Input struct {
	Value            int64 `json:"value"`
	FailUntil        int   `json:"fail_until"`
	PermanentFailure bool  `json:"permanent_failure"`
	WorkMS           int   `json:"work_ms"`
}
type Request struct {
	Key         string `json:"key"`
	Input       Input  `json:"input"`
	DelayMS     int    `json:"delay_ms"`
	MaxAttempts int    `json:"max_attempts"`
}
type Receipt struct {
	ID      string `json:"id"`
	Created bool   `json:"created"`
}
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("relay HTTP %d: %s", e.Status, e.Message) }

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(base, token string) *Client {
	return &Client{strings.TrimRight(base, "/"), token, &http.Client{Timeout: 10 * time.Second}}
}
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	response, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		var result struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&result)
		return &APIError{response.StatusCode, result.Error}
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(output)
}

// Enqueue keeps the key unchanged. On an ambiguous response callers retry the same Request.
// The client deliberately does not blindly retry non-idempotent control operations.
func (c *Client) Enqueue(ctx context.Context, r Request) (Receipt, error) {
	var receipt Receipt
	err := c.request(ctx, "POST", "/api/runs", r, &receipt)
	return receipt, err
}
func (c *Client) Run(ctx context.Context, id string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.request(ctx, "GET", "/api/runs/"+url.PathEscape(id), nil, &result)
	return result, err
}
