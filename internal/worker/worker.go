package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/rjrddy/relay/internal/engine"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Worker struct {
	Engine *engine.Engine
	ID     string
	Hook   func(string, engine.Job)
}

func (w *Worker) Loop(ctx context.Context) error {
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, 3*time.Second)
		j, err := w.Engine.Claim(op, w.ID)
		cancel()
		if err != nil {
			slog.Warn("claim_failed", "owner", w.ID)
			if !wait(ctx, 50*time.Millisecond) {
				break
			}
			continue
		}
		if j == nil {
			if !wait(ctx, 10*time.Millisecond) {
				break
			}
			continue
		}
		if w.Hook != nil {
			w.Hook("claim", *j)
		}
		w.handle(ctx, *j)
	}
	return ctx.Err()
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (w *Worker) handle(parent context.Context, j engine.Job) {
	ctx := propagation.TraceContext{}.Extract(parent, propagation.MapCarrier{"traceparent": j.Traceparent})
	ctx, span := otel.Tracer("relay").Start(ctx, "workflow.step", trace.WithAttributes(attribute.String("run.id", j.RunID), attribute.Int("step.position", j.Position), attribute.Int64("step.epoch", j.Epoch)))
	defer span.End()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	hbDone := make(chan struct{})
	defer func() { cancel(); <-hbDone }()
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(w.Engine.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				op, c := context.WithTimeout(ctx, w.Engine.Lease/3)
				err := w.Engine.Heartbeat(op, j)
				c()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	if !wait(ctx, time.Duration(j.Input.WorkMS)*time.Millisecond) {
		span.SetStatus(codes.Error, "lease or worker cancelled")
		return
	}
	op, c := context.WithTimeout(ctx, 3*time.Second)
	defer c()
	var err error
	if j.Position == 1 && (j.Input.PermanentFailure || j.Epoch <= int64(j.Input.FailUntil)) {
		reason := "synthetic_retry"
		if j.Input.PermanentFailure {
			reason = "synthetic_permanent"
		}
		err = w.Engine.Fail(op, j, reason)
	} else {
		var value int64
		value, err = w.Engine.Previous(op, j)
		if err == nil {
			if j.Position == 1 {
				value *= 2
			}
			err = w.Engine.Complete(op, j, value)
		}
		if err == nil && w.Hook != nil {
			w.Hook("commit", j)
		}
	}
	outcome := "transition_committed"
	if err != nil {
		outcome = "not_committed"
		span.SetStatus(codes.Error, "database operation did not acknowledge completion")
	}
	if errors.Is(err, engine.ErrFenced) {
		outcome = "fenced"
	}
	slog.Info("step_finished", "run", j.RunID, "position", j.Position, "epoch", j.Epoch, "attempt", j.Attempt, "outcome", outcome, "trace_id", span.SpanContext().TraceID().String())
}
