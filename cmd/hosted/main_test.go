package main

import (
	"context"
	"github.com/rjrddy/relay/internal/engine"
	"os"
	"testing"
)

func TestHostedSchemaIsolatedAndReentrant(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("real synthetic test database required")
	}
	ctx := context.Background()
	e, err := isolated(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		e.Pool.Close()
		base, err := engine.Open(ctx, dsn)
		if err != nil {
			t.Error(err)
			return
		}
		defer base.Pool.Close()
		if _, err = base.Pool.Exec(ctx, "DROP SCHEMA relay_vercel_v1 CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	var name string
	if err = e.Pool.QueryRow(ctx, "SELECT current_schema()").Scan(&name); err != nil || name != "relay_vercel_v1" {
		t.Fatal(err, name)
	}
	second, err := isolated(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	second.Pool.Close()
	if _, _, err = e.Enqueue(ctx, engine.Request{Key: "hosted-schema-test", Input: engine.Input{Value: 1}, MaxAttempts: 2}); err != nil {
		t.Fatal(err)
	}
}
