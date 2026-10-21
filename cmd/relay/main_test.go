package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rjrddy/relay/internal/engine"
	sdk "github.com/rjrddy/relay/sdk/go"
)

func TestEmbeddedServiceCompletesAndShutsDown(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	e, err := engine.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Pool.Close()
	schema := "test_cli_" + engine.ID()
	if _, err = e.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer e.Pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	binary := filepath.Join(t.TempDir(), "relay")
	build := exec.Command("go", "build", "-o", binary, ".")
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(raw))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	endpoint := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	child := exec.Command(binary, "serve")
	child.Env = append(os.Environ(), "DATABASE_URL="+u.String(), "PORT="+fmt.Sprint(port), "RELAY_PASSWORD=synthetic-cli-password", "RELAY_API_TOKEN=synthetic-cli-api-token-123456", "RELAY_EMBEDDED_WORKERS=2", "RELAY_SEED=1", "OTEL_EXPORTER_OTLP_ENDPOINT=")
	output, err := os.Create(filepath.Join(t.TempDir(), "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	child.Stdout = output
	child.Stderr = output
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			child.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		response, err := client.Get("http://" + endpoint + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("startup deadline")
		}
		time.Sleep(50 * time.Millisecond)
	}
	api := sdk.New("http://"+endpoint, "synthetic-cli-api-token-123456")
	receipt, err := api.Enqueue(ctx, sdk.Request{Key: "embedded-smoke", Input: sdk.Input{Value: 21, FailUntil: 1}, MaxAttempts: 4})
	if err != nil {
		t.Fatal(err)
	}
	for {
		raw, err := api.Run(ctx, receipt.ID)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Run struct {
				State string `json:"state"`
			} `json:"run"`
			Effects []struct {
				Result int64 `json:"result"`
			} `json:"effects"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Run.State == "done" {
			if len(result.Effects) != 3 || result.Effects[2].Result != 42 {
				t.Fatal(string(raw))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("embedded completion deadline", string(raw))
		}
		time.Sleep(50 * time.Millisecond)
	}
	child.Process.Signal(os.Interrupt)
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("graceful shutdown deadline")
	}
}
