// Package relay provides a small retry-safe HTTP client for the Relay control plane.
package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

// Enqueue retries only safe admission requests, preserving key and immutable payload.
// Conflicts and validation/auth failures are returned immediately. Control writes are not retried.
func (c *Client) Enqueue(ctx context.Context, r Request) (Receipt, error) {
	var receipt Receipt
	var last error
	hash := sha256.Sum256([]byte(r.Key))
	for attempt := 0; attempt < 5; attempt++ {
		last = c.request(ctx, "POST", "/api/runs", r, &receipt)
		if last == nil {
			return receipt, nil
		}
		if ctx.Err() != nil {
			return receipt, ctx.Err()
		}
		var api *APIError
		var transport *url.Error
		retryable := errors.Is(last, io.EOF) || errors.Is(last, io.ErrUnexpectedEOF)
		if errors.As(last, &api) {
			retryable = api.Status == 429 || api.Status == 502 || api.Status == 503 || api.Status == 504
		}
		if errors.As(last, &transport) && transport.Op != "parse" {
			retryable = true
		}
		if !retryable || attempt == 4 {
			return receipt, last
		}
		timer := time.NewTimer(time.Duration(100*(1<<attempt)+int(hash[0])%41) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return receipt, ctx.Err()
		case <-timer.C:
		}
	}
	return receipt, last
}
func (c *Client) Run(ctx context.Context, id string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.request(ctx, "GET", "/api/runs/"+url.PathEscape(id), nil, &result)
	return result, err
}
