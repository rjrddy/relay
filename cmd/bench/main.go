package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rjrddy/relay/internal/engine"
	"github.com/rjrddy/relay/internal/faultproxy"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func start(binary, dsn, id, crash, out string) (*process, error) {
	args := []string{"worker", "--id", id}
	if crash != "" {
		args = append(args, "--crash-phase", crash)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn, "RELAY_ALLOW_FAULTS=1", "RELAY_LEASE_MS=1000")
	file, err := os.Create(filepath.Join("docs/results", out+"-"+id+".jsonl"))
	if err != nil {
		return nil, err
	}
	cmd.Stdout = file
	cmd.Stderr = file
	if err = cmd.Start(); err != nil {
		file.Close()
		return nil, err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); file.Close(); close(p.done) }()
	return p, nil
}
func (p *process) stop() {
	select {
	case <-p.done:
		return
	default:
		p.cmd.Process.Kill()
		<-p.done
	}
}
func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	pos := q * float64(len(values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return values[lo] + (values[hi]-values[lo])*(pos-float64(lo))
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	jobs := flag.Int("jobs", 300, "initial workflow count")
	duration := flag.Duration("duration", 0, "additional sustained wall-clock campaign duration")
	chaos := flag.Bool("chaos", false, "kill actual workers and interrupt database transport")
	out := flag.String("out", "load", "result prefix")
	binary := flag.String("binary", "bin/relay", "worker executable")
	flag.Parse()
	if *jobs < 1 || *jobs > 2000 || *duration < 0 || *duration > 2*time.Hour || strings.ContainsAny(*out, "/\\.") {
		return errors.New("invalid benchmark bounds")
	}
	os.MkdirAll("docs/results", 0755)
	workerBinary, err := filepath.Abs(*binary)
	if err != nil {
		return err
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return errors.New("TEST_DATABASE_URL is required; no benchmark uses a default database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *duration+3*time.Minute)
	defer cancel()
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	admin, err := engine.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer admin.Pool.Close()
	schema := "bench_" + engine.ID()
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		return err
	}
	defer admin.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	e, err := engine.Open(ctx, u.String())
	if err != nil {
		return err
	}
	defer e.Pool.Close()
	if err = e.Migrate(ctx); err != nil {
		return err
	}
	target := u.Host
	if !strings.Contains(target, ":") {
		target = net.JoinHostPort(target, "5432")
	}
	proxy, err := faultproxy.Start(target)
	if err != nil {
		return err
	}
	defer proxy.Close()
	workerURL := *u
	workerURL.Host = proxy.Addr()
	started := time.Now()
	enqueued := 0
	enqueue := func() error {
		r := engine.Request{Key: fmt.Sprintf("bench-%d", enqueued), Input: engine.Input{Value: int64(enqueued % 1000)}, MaxAttempts: 10}
		if enqueued%13 == 0 {
			r.Input.FailUntil = 1
		}
		if *chaos {
			r.Input.WorkMS = 5
		}
		_, _, err := e.Enqueue(ctx, r)
		if err == nil {
			enqueued++
		}
		return err
	}
	for i := 0; i < *jobs; i++ {
		if err = enqueue(); err != nil {
			return err
		}
	}
	var children []*process
	defer func() {
		for _, p := range children {
			p.stop()
		}
	}()
	faults := []map[string]any{}
	var mu sync.Mutex
	record := func(kind string) {
		mu.Lock()
		faults = append(faults, map[string]any{"kind": kind, "elapsed_seconds": time.Since(started).Seconds()})
		mu.Unlock()
	}
	if *chaos {
		for _, phase := range []string{"claim", "commit"} {
			p, err := start(workerBinary, workerURL.String(), "crash-"+phase, phase, *out)
			if err != nil {
				return err
			}
			children = append(children, p)
			select {
			case <-p.done:
				if exit, ok := p.err.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
					return fmt.Errorf("crash hook did not execute: %s", phase)
				}
			case <-time.After(10 * time.Second):
				return errors.New("crash-hook timeout")
			}
			record("process_exit_after_" + phase)
		}
	}
	for i := 0; i < 4; i++ {
		p, err := start(workerBinary, workerURL.String(), fmt.Sprintf("worker-%d", i), "", *out)
		if err != nil {
			return err
		}
		children = append(children, p)
	}
	faultsDone := make(chan struct{})
	go func() {
		defer close(faultsDone)
		if !*chaos {
			return
		}
		end := started.Add(*duration)
		for {
			time.Sleep(200 * time.Millisecond)
			proxy.Set(true, 0)
			record("database_transport_partition_begin")
			time.Sleep(350 * time.Millisecond)
			proxy.Set(false, 0)
			record("database_transport_partition_end")
			time.Sleep(100 * time.Millisecond)
			proxy.Set(false, 15*time.Millisecond)
			record("database_transport_delay_begin")
			time.Sleep(800 * time.Millisecond)
			proxy.Set(false, 0)
			record("database_transport_delay_end")
			if *duration < 30*time.Second || time.Until(end) <= 0 {
				return
			}
			delay := 30 * time.Second
			if time.Until(end) < delay {
				delay = time.Until(end)
			}
			time.Sleep(delay)
			if time.Until(end) <= 0 {
				return
			}
		}
	}()
	sustainedUntil := started.Add(*duration)
	for *duration > 0 && time.Now().Before(sustainedUntil) {
		if err = enqueue(); err != nil && !errors.Is(err, engine.ErrCapacity) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	<-faultsDone
	var reconciliation map[string]any
	var raw json.RawMessage
	for {
		raw, err = e.Reconcile(ctx)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &reconciliation); err != nil {
			return err
		}
		if reconciliation["pending"].(float64) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	elapsed := time.Since(started).Seconds()
	for _, key := range []string{"missing_effects", "orphan_effects", "incorrect_receipts", "duplicate_effects", "dead"} {
		if reconciliation[key].(float64) != 0 {
			return fmt.Errorf("reconciliation failed for %s", key)
		}
	}
	if int(reconciliation["done"].(float64)) != enqueued || int(reconciliation["effects"].(float64)) != 3*enqueued {
		return errors.New("lost run or step effect")
	}
	latencies := []float64{}
	for _, row := range reconciliation["latencies"].([]any) {
		latencies = append(latencies, row.(map[string]any)["enqueue_start_ms"].(float64))
	}
	summary := map[string]any{"workflows": enqueued, "step_effects": reconciliation["effects"], "lost_workflows": enqueued - int(reconciliation["done"].(float64)), "duplicate_effects": reconciliation["duplicate_effects"], "incorrect_receipts": reconciliation["incorrect_receipts"], "elapsed_seconds": elapsed, "elapsed_hours": elapsed / 3600, "workflows_per_second": float64(enqueued) / elapsed, "enqueue_start_p50_ms": quantile(append([]float64{}, latencies...), .5), "enqueue_start_p99_ms": quantile(latencies, .99), "faults": faults, "workers": 4, "scope": "Go engine admission through all ordered Postgres effects, including drain; no HTTP API timing; transport delay is not disk delay"}
	encoded, _ := json.MarshalIndent(summary, "", "  ")
	os.WriteFile(filepath.Join("docs/results", *out+".json"), append(encoded, '\n'), 0644)
	os.WriteFile(filepath.Join("docs/results", *out+"-reconciliation.json"), append(raw, '\n'), 0644)
	rows, err := e.Pool.Query(ctx, "SELECT row_to_json(x) FROM events x ORDER BY seq")
	if err != nil {
		return err
	}
	defer rows.Close()
	file, err := os.Create(filepath.Join("docs/results", *out+"-events.jsonl.gz"))
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(file)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if _, err = gz.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
