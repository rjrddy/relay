package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientPreservesIdempotencyAndSurfacesConflict(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-synthetic-sdk-token" {
			t.Error("token absent")
		}
		n++
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.Write([]byte(`{"id":"same","created":true}`))
		} else {
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"idempotency conflict"}`))
		}
	}))
	defer s.Close()
	c := New(s.URL, "test-synthetic-sdk-token")
	receipt, err := c.Enqueue(context.Background(), Request{Key: "same-key", MaxAttempts: 3})
	if err != nil || receipt.ID != "same" {
		t.Fatal(receipt, err)
	}
	if _, err = c.Enqueue(context.Background(), Request{Key: "same-key", MaxAttempts: 3}); err == nil {
		t.Fatal("conflict swallowed")
	}
}

func TestAmbiguousEnqueueRetriesSameRequest(t *testing.T) {
	bodies := []string{}
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		count++
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"ambiguous response"}`))
			return
		}
		w.Write([]byte(`{"id":"committed-before-lost-ack","created":false}`))
	}))
	defer s.Close()
	c := New(s.URL, "synthetic-token")
	receipt, err := c.Enqueue(context.Background(), Request{Key: "stable", Input: Input{Value: 7}, MaxAttempts: 3})
	if err != nil || receipt.Created || count != 2 || bodies[0] != bodies[1] {
		t.Fatal(receipt, count, err)
	}
}
