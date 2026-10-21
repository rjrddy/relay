package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjrddy/relay/internal/engine"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/scrypt"
)

type session struct {
	Expires time.Time
	CSRF    string
}
type Server struct {
	Engine    *engine.Engine
	salt      []byte
	verifier  []byte
	tokenHash [32]byte
	Secure    bool
	Web       string
	mu        sync.Mutex
	sessions  map[string]session
	budgets   map[string][]time.Time
}

func New(e *engine.Engine, password, token, web string, secure bool) (*Server, error) {
	if len(password) < 12 || len(token) < 24 {
		return nil, errors.New("operator password and SDK token must be provisioned")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	verifier, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	return &Server{Engine: e, salt: salt, verifier: verifier, tokenHash: sha256.Sum256([]byte(token)), Web: web, Secure: secure, sessions: map[string]session{}, budgets: map[string][]time.Time{}}, nil
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func reject(w http.ResponseWriter, status int, message string) {
	send(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		reject(w, 400, "invalid JSON or field bounds")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		reject(w, 400, "only one JSON value is allowed")
		return false
	}
	return true
}
func (s *Server) limit(key string, max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, times := range s.budgets {
		if len(times) == 0 || now.Sub(times[len(times)-1]) > time.Minute {
			delete(s.budgets, k)
		}
	}
	if _, ok := s.budgets[key]; !ok && len(s.budgets) >= 2048 {
		return false
	}
	q := s.budgets[key]
	for len(q) > 0 && now.Sub(q[0]) > time.Minute {
		q = q[1:]
	}
	if len(q) >= max {
		return false
	}
	s.budgets[key] = append(q, now)
	return true
}
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "local"
	}
	return host
}
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); auth != "" {
		candidate := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare(candidate[:], s.tokenHash[:]) != 1 {
			reject(w, 401, "invalid token")
			return false
		}
	} else {
		cookie, err := r.Cookie("relay_session")
		if err != nil {
			reject(w, 401, "sign in required")
			return false
		}
		s.mu.Lock()
		v, ok := s.sessions[cookie.Value]
		if ok && time.Now().After(v.Expires) {
			delete(s.sessions, cookie.Value)
			ok = false
		}
		s.mu.Unlock()
		if !ok {
			reject(w, 401, "session expired")
			return false
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(v.CSRF)) != 1 {
			reject(w, 403, "CSRF proof required")
			return false
		}
	}
	max := 600
	kind := "read:"
	if r.Method != "GET" && r.Method != "HEAD" {
		max = 60
		kind = "write:"
	}
	if !s.limit(kind+clientKey(r), max) {
		reject(w, 429, "rate limited")
		return false
	}
	return true
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.limit("login:"+clientKey(r), 10) {
		reject(w, 429, "rate limited")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Password) > 256 {
		reject(w, 400, "password too long")
		return
	}
	got, err := scrypt.Key([]byte(body.Password), s.salt, 32768, 8, 1, 32)
	if err != nil || subtle.ConstantTimeCompare(got, s.verifier) != 1 {
		reject(w, 401, "invalid credentials")
		return
	}
	token, csrf := engine.ID()+engine.ID(), engine.ID()
	s.mu.Lock()
	for k, v := range s.sessions {
		if time.Now().After(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 100 {
		s.mu.Unlock()
		reject(w, 429, "session capacity")
		return
	}
	s.sessions[token] = session{time.Now().Add(time.Hour), csrf}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "relay_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.Secure, MaxAge: 3600})
	send(w, 200, map[string]string{"csrf": csrf})
}
func (s *Server) auth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authenticate(w, r) {
			handler(w, r)
		}
	}
}
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		send(w, 200, value)
		return
	}
	status, message := 503, "database unavailable; retry with the same idempotency key"
	switch {
	case errors.Is(err, engine.ErrConflict), errors.Is(err, engine.ErrConfigConflict), errors.Is(err, engine.ErrNotDead):
		status, message = 409, err.Error()
	case errors.Is(err, engine.ErrCapacity):
		status, message = 429, err.Error()
	case engine.IsNotFound(err):
		status, message = 404, "not found"
	}
	category := "request_rejected"
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		category = "postgres_" + postgres.Code
	}
	slog.Warn("operation_rejected", "status", status, "category", category, "trace_id", w.Header().Get("X-Trace-Id"))
	reject(w, status, message)
}

type responseLog struct {
	http.ResponseWriter
	status int
}

func (w *responseLog) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseLog) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		err := s.Engine.Pool.Ping(r.Context())
		if err != nil {
			reject(w, 503, "database unavailable")
			return
		}
		send(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/session", s.auth(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("relay_session")
		if err != nil {
			reject(w, 400, "cookie session required")
			return
		}
		s.mu.Lock()
		v := s.sessions[cookie.Value]
		s.mu.Unlock()
		send(w, 200, map[string]string{"csrf": v.CSRF})
	}))
	mux.HandleFunc("POST /api/logout", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("relay_session"); err == nil {
			s.mu.Lock()
			delete(s.sessions, c.Value)
			s.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "relay_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.Secure})
		send(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/runs", s.auth(func(w http.ResponseWriter, r *http.Request) { v, err := s.Engine.Runs(r.Context()); respond(w, v, err) }))
	mux.HandleFunc("POST /api/runs", s.auth(func(w http.ResponseWriter, r *http.Request) {
		var body engine.Request
		if !decode(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			reject(w, 422, err.Error())
			return
		}
		id, created, err := s.Engine.Enqueue(r.Context(), body)
		respond(w, map[string]any{"id": id, "created": created}, err)
	}))
	mux.HandleFunc("GET /api/runs/{id}", s.auth(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Detail(r.Context(), r.PathValue("id"))
		respond(w, v, err)
	}))
	mux.HandleFunc("POST /api/runs/{id}/replay", s.auth(func(w http.ResponseWriter, r *http.Request) {
		err := s.Engine.Replay(r.Context(), r.PathValue("id"))
		respond(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/control", s.auth(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Config(r.Context())
		respond(w, v, err)
	}))
	mux.HandleFunc("PUT /api/control", s.auth(func(w http.ResponseWriter, r *http.Request) {
		var body engine.Config
		if !decode(w, r, &body) {
			return
		}
		if body.Rollout < 0 || body.Rollout > 100 || body.Capacity < 1 || body.Capacity > 100000 {
			reject(w, 422, "invalid configuration bounds")
			return
		}
		v, err := s.Engine.Publish(r.Context(), body)
		respond(w, v, err)
	}))
	mux.HandleFunc("POST /api/control/rollback", s.auth(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Target   int64 `json:"target"`
			Expected int64 `json:"expected"`
		}
		if !decode(w, r, &b) {
			return
		}
		v, err := s.Engine.Rollback(r.Context(), b.Target, b.Expected)
		respond(w, v, err)
	}))
	mux.HandleFunc("GET /api/versions", s.auth(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Versions(r.Context())
		respond(w, v, err)
	}))
	mux.HandleFunc("GET /api/metrics", s.auth(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Metrics(r.Context())
		respond(w, v, err)
	}))
	mux.HandleFunc("GET /api/reconciliation", s.auth(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Engine.Reconcile(r.Context())
		respond(w, v, err)
	}))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		path := filepath.Join(s.Web, filepath.Clean("/"+name))
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		ctx := propagation.TraceContext{}.Extract(requestCtx, propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("relay").Start(ctx, "http.request")
		defer span.End()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		w.Header().Set("X-Trace-Id", span.SpanContext().TraceID().String())
		scoped := r.WithContext(ctx)
		logged := &responseLog{ResponseWriter: w}
		mux.ServeHTTP(logged, scoped)
		status := logged.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("http_completed", "method", r.Method, "status", status, "route", scoped.Pattern, "trace_id", trace.SpanContextFromContext(ctx).TraceID().String())
	})
}
