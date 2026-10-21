package relay

import (
	"context"
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
