// Command hosted serves Vercel's evidence site and live console from one image.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rjrddy/relay/internal/engine"
	"github.com/rjrddy/relay/internal/server"
	"github.com/rjrddy/relay/internal/worker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func isolated(ctx context.Context, dsn string) (*engine.Engine, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, errors.New("configure PostgreSQL DATABASE_URL")
	}
	base, err := engine.Open(ctx, dsn)
	if err != nil {
		return nil, errors.New("database connection failed")
	}
	err = func() error {
		tx, err := base.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('relay-vercel-schema-v1'))"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS relay_vercel_v1"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}()
	base.Pool.Close()
	if err != nil {
		return nil, errors.New("dedicated Relay schema initialization failed")
	}
	q := u.Query()
	q.Set("search_path", "relay_vercel_v1")
	u.RawQuery = q.Encode()
	e, err := engine.Open(ctx, u.String())
	if err != nil {
		return nil, errors.New("isolated database connection failed")
	}
	err = func() error {
		tx, err := e.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('relay-vercel-schema-v1'))"); err != nil {
			return err
		}
		if err = e.Migrate(ctx); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}()
	if err != nil {
		e.Pool.Close()
		return nil, errors.New("isolated workflow migration failed")
	}
	return e, nil
}
func run() error {
	provider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer provider.Shutdown(context.Background())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	boot, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	e, err := isolated(boot, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer e.Pool.Close()
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		return (&worker.Worker{Engine: e, ID: "vercel-" + engine.ID()}).Loop(ctx)
	}
	web := os.Getenv("RELAY_WEB")
	if web == "" {
		web = "/app/web"
	}
	proof := os.Getenv("RELAY_PROOF")
	if proof == "" {
		proof = "/app/proof"
	}
	app, err := server.New(e, os.Getenv("RELAY_PASSWORD"), os.Getenv("RELAY_API_TOKEN"), web, true)
	if err != nil {
		return err
	}
	if err = app.EnableDurableSessions(boot); err != nil {
		return errors.New("session storage initialization failed")
	}
	if os.Getenv("RELAY_SEED") == "1" {
		for _, r := range []engine.Request{{Key: "vercel-seed-normal-v1", Input: engine.Input{Value: 42, WorkMS: 100}, MaxAttempts: 3}, {Key: "vercel-seed-retry-v1", Input: engine.Input{Value: 7, FailUntil: 1, WorkMS: 100}, MaxAttempts: 3}, {Key: "vercel-seed-dead-v1", Input: engine.Input{Value: 11, PermanentFailure: true, WorkMS: 100}, MaxAttempts: 2}} {
			if _, _, err = e.Enqueue(boot, r); err != nil {
				return errors.New("synthetic seeding failed")
			}
		}
	}
	mux := http.NewServeMux()
	live := app.Handler()
	mux.Handle("/api/", live)
	mux.Handle("/health", live)
	mux.Handle("/metrics", live)
	mux.Handle("/assets/", live)
	mux.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/console/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/console/", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/console")
		live.ServeHTTP(w, r)
	})
	mux.Handle("/", http.FileServer(http.Dir(proof)))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("hosted_service_stopped", "reason", err.Error())
		os.Exit(1)
	}
}
