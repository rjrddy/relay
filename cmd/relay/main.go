package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/rjrddy/relay/internal/engine"
	"github.com/rjrddy/relay/internal/server"
	"github.com/rjrddy/relay/internal/worker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: relay migrate|seed|serve|worker|reconcile")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	provider := sdktrace.NewTracerProvider(sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "relay-"+os.Args[1]))))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			return err
		}
		provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "relay-"+os.Args[1]))))
	}
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		provider.Shutdown(c)
	}()
	e, err := engine.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database connection failed")
	}
	defer e.Pool.Close()
	if lease := os.Getenv("RELAY_LEASE_MS"); lease != "" {
		n, err := strconv.Atoi(lease)
		if err != nil || n < 150 || n > 60000 {
			return errors.New("invalid lease duration")
		}
		e.Lease = time.Duration(n) * time.Millisecond
	}
	if err = e.Migrate(ctx); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	switch os.Args[1] {
	case "migrate":
		return nil
	case "seed":
		return seed(ctx, e)
	case "worker":
		flags := flag.NewFlagSet("worker", flag.ContinueOnError)
		crash := flags.String("crash-phase", "", "test-only claim or commit crash")
		owner := flags.String("id", engine.ID(), "worker identity")
		if err = flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *crash != "" && (*crash != "claim" && *crash != "commit" || os.Getenv("RELAY_ALLOW_FAULTS") != "1") {
			return errors.New("fault hook requires explicit test opt-in")
		}
		w := worker.Worker{Engine: e, ID: *owner, Hook: func(phase string, j engine.Job) {
			if *crash == phase {
				slog.Info("injected_process_exit", "phase", phase, "run", j.RunID)
				os.Exit(17)
			}
		}}
		err = w.Loop(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case "reconcile":
		raw, err := e.Reconcile(ctx)
		if err == nil {
			fmt.Println(string(raw))
		}
		return err
	case "serve":
		if os.Getenv("RELAY_SEED") == "1" {
			if err := seed(ctx, e); err != nil {
				return err
			}
		}
		serviceCtx, serviceCancel := context.WithCancel(ctx)
		var workers sync.WaitGroup
		defer func() { serviceCancel(); workers.Wait() }()
		if value := os.Getenv("RELAY_EMBEDDED_WORKERS"); value != "" {
			count, err := strconv.Atoi(value)
			if err != nil || count < 0 || count > 4 {
				return errors.New("invalid embedded worker count")
			}
			for i := 0; i < count; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					w := worker.Worker{Engine: e, ID: "embedded-" + engine.ID()}
					w.Loop(serviceCtx)
				}()
			}
		}

		web := os.Getenv("RELAY_WEB")
		if web == "" {
			web = "apps/web/dist"
		}
		app, err := server.New(e, os.Getenv("RELAY_PASSWORD"), os.Getenv("RELAY_API_TOKEN"), web, os.Getenv("COOKIE_SECURE") == "1")
		if err != nil {
			return err
		}
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		httpServer := &http.Server{Addr: ":" + port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				httpServer.Shutdown(c)
			case <-done:
			}
		}()
		err = httpServer.ListenAndServe()
		close(done)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	default:
		return errors.New("unknown command")
	}
}
func seed(ctx context.Context, e *engine.Engine) error {
	for i := 0; i < 18; i++ {
		input := engine.Input{Value: int64(100 + i), WorkMS: 80}
		attempts := 4
		if i%5 == 0 {
			input.FailUntil = 1
		}
		if i == 17 {
			input.FailUntil = 2
			attempts = 2
		}
		_, _, err := e.Enqueue(ctx, engine.Request{Key: fmt.Sprintf("seed-v1-%d", i), Input: input, DelayMS: (i % 3) * 200, MaxAttempts: attempts})
		if err != nil && !errors.Is(err, engine.ErrCapacity) {
			return err
		}
	}

	slog.Info("synthetic_seed_complete")
	return nil
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("relay_failed", "reason", err.Error())
		os.Exit(1)
	}
}
