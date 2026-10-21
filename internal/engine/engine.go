package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjrddy/relay/migrations"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var ErrConflict = errors.New("idempotency conflict")
var ErrCapacity = errors.New("queue capacity exceeded")
var ErrFenced = errors.New("lease lost or expired")
var ErrPaused = errors.New("queue paused")
var ErrNotDead = errors.New("run is not a dead letter")
var ErrConfigConflict = errors.New("configuration version changed")

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

func (r Request) Validate() error {
	if len(r.Key) < 1 || len(r.Key) > 128 || r.Input.Value < -1000000 || r.Input.Value > 1000000 ||
		r.Input.FailUntil < 0 || r.Input.FailUntil > 10 || r.Input.WorkMS < 0 || r.Input.WorkMS > 5000 ||
		r.DelayMS < 0 || r.DelayMS > 86400000 || r.MaxAttempts < 1 || r.MaxAttempts > 10 {
		return errors.New("invalid workflow input or bounds")
	}
	return nil
}

type Job struct {
	RunID       string `json:"run_id"`
	Position    int    `json:"position"`
	Name        string `json:"name"`
	Epoch       int64  `json:"epoch"`
	Owner       string `json:"owner"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	Input       Input  `json:"input"`
	Traceparent string `json:"-"`
}
type Engine struct {
	Pool  *pgxpool.Pool
	Lease time.Duration
}

func Open(ctx context.Context, dsn string) (*Engine, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 16
	// Bound lock waits, statement waits and abandoned transactions independently of worker timeouts.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "5000"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Engine{pool, 2 * time.Second}, nil
}
func (e *Engine) Migrate(ctx context.Context) error {
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73712951)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, migrations.Schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Cohort(id string) int {
	h := sha256.Sum256([]byte("cohort-v1:" + id))
	return int(binary.BigEndian.Uint64(h[:8]) % 100)
}
func event(ctx context.Context, tx pgx.Tx, j Job, kind string) error {
	sc := trace.SpanContextFromContext(ctx)
	_, err := tx.Exec(ctx, `INSERT INTO events(run_id,position,kind,owner,epoch,attempt,trace_id,span_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, j.RunID, j.Position, kind, j.Owner, j.Epoch, j.Attempt, sc.TraceID().String(), sc.SpanID().String())
	return err
}
func control(ctx context.Context, tx pgx.Tx, exclusive bool) (Config, error) {
	lock := "FOR SHARE"
	if exclusive {
		lock = "FOR UPDATE"
	}
	var c Config
	err := tx.QueryRow(ctx, "SELECT paused,rollout,capacity,version FROM queue_control WHERE id=1 "+lock).Scan(&c.Paused, &c.Rollout, &c.Capacity, &c.Version)
	return c, err
}
func (e *Engine) Enqueue(ctx context.Context, r Request) (string, bool, error) {
	if err := r.Validate(); err != nil {
		return "", false, err
	}
	ctx, span := otel.Tracer("relay").Start(ctx, "workflow.enqueue")
	defer span.End()
	// Idempotency includes delay and retry policy, not only the numeric payload.
	blob, _ := json.Marshal(struct {
		Input       Input
		DelayMS     int
		MaxAttempts int
	}{r.Input, r.DelayMS, r.MaxAttempts})
	hash := sha256.Sum256(blob)
	finger := hex.EncodeToString(hash[:])
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	cfg, err := control(ctx, tx, true)
	if err != nil {
		return "", false, err
	}
	var id, existing string
	err = tx.QueryRow(ctx, "SELECT id,fingerprint FROM runs WHERE idem_key=$1", r.Key).Scan(&id, &existing)
	if err == nil {
		if existing != finger {
			return "", false, ErrConflict
		}
		return id, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	var pending int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM runs WHERE state='running'").Scan(&pending); err != nil {
		return "", false, err
	}
	if pending >= cfg.Capacity {
		return "", false, ErrCapacity
	}
	id = ID()
	input, _ := json.Marshal(r.Input)
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	_, err = tx.Exec(ctx, `INSERT INTO runs(id,idem_key,fingerprint,definition,input,cohort,state,scheduled_at,traceparent)
 VALUES($1,$2,$3,'synthetic-pipeline-v1',$4,$5,'running',clock_timestamp()+$6::bigint*interval '1 millisecond',$7)`, id, r.Key, finger, input, Cohort(id), r.DelayMS, carrier.Get("traceparent"))
	if err != nil {
		return "", false, err
	}
	for i, name := range []string{"validate", "calculate", "receipt"} {
		state := "waiting"
		if i == 0 {
			state = "queued"
		}
		_, err = tx.Exec(ctx, `INSERT INTO steps(run_id,position,name,state,max_attempts,available_at)
 SELECT id,$2,$3,$4,$5,scheduled_at FROM runs WHERE id=$1`, id, i, name, state, r.MaxAttempts)
		if err != nil {
			return "", false, err
		}
	}
	if err = event(ctx, tx, Job{RunID: id, Position: -1}, "enqueued"); err != nil {
		return "", false, err
	}
	return id, true, tx.Commit(ctx)
}
func (e *Engine) Claim(ctx context.Context, owner string) (*Job, error) {
	if owner == "" || len(owner) > 128 {
		return nil, errors.New("invalid owner")
	}
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	cfg, err := control(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if cfg.Paused {
		return nil, tx.Commit(ctx)
	}
	var j Job
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT s.run_id,s.position,s.name,s.epoch,s.attempts,s.max_attempts,r.input,r.traceparent
 FROM steps s JOIN runs r ON s.run_id=r.id WHERE r.state='running' AND r.cohort<$1
 AND ((s.state='queued' AND s.available_at<=clock_timestamp()) OR (s.state='running' AND s.lease_until<=clock_timestamp()))
 ORDER BY s.available_at,s.run_id,s.position FOR UPDATE OF s SKIP LOCKED LIMIT 1`, cfg.Rollout).Scan(&j.RunID, &j.Position, &j.Name, &j.Epoch, &j.Attempt, &j.MaxAttempts, &raw, &j.Traceparent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &j.Input); err != nil {
		return nil, err
	}
	ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": j.Traceparent})
	ctx, claimSpan := otel.Tracer("relay").Start(ctx, "workflow.claim")
	defer claimSpan.End()
	if j.Attempt >= j.MaxAttempts {
		if err = dead(ctx, tx, j, "attempts_exhausted"); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	j.Epoch++
	j.Attempt++
	j.Owner = owner
	_, err = tx.Exec(ctx, `UPDATE steps SET state='running',epoch=$3,owner=$4,attempts=$5,
 lease_until=clock_timestamp()+$6::bigint*interval '1 millisecond',first_started=coalesce(first_started,clock_timestamp()) WHERE run_id=$1 AND position=$2`, j.RunID, j.Position, j.Epoch, owner, j.Attempt, e.Lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	if err = event(ctx, tx, j, "claimed"); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &j, nil
}
func verify(ctx context.Context, tx pgx.Tx, j Job) error {
	var locked int
	if err := tx.QueryRow(ctx, "SELECT position FROM steps WHERE run_id=$1 AND position=$2 FOR UPDATE", j.RunID, j.Position).Scan(&locked); err != nil {
		return err
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT state='running' AND epoch=$3 AND owner=$4 AND lease_until>clock_timestamp()
 FROM steps WHERE run_id=$1 AND position=$2`, j.RunID, j.Position, j.Epoch, j.Owner).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrFenced
	}
	return nil
}
func (e *Engine) Heartbeat(ctx context.Context, j Job) error {
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = verify(ctx, tx, j); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE steps SET lease_until=clock_timestamp()+$3::bigint*interval '1 millisecond' WHERE run_id=$1 AND position=$2`, j.RunID, j.Position, e.Lease.Milliseconds())
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (e *Engine) Complete(ctx context.Context, j Job, result int64) error {
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	cfg, err := control(ctx, tx, false)
	if err != nil {
		return err
	}
	if cfg.Paused {
		return ErrPaused
	}
	if err = verify(ctx, tx, j); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO effects(run_id,position,result) VALUES($1,$2,$3)", j.RunID, j.Position, result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE steps SET state='done',result=$3,completed_at=clock_timestamp(),lease_until=NULL WHERE run_id=$1 AND position=$2", j.RunID, j.Position, result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE steps SET state='queued',available_at=clock_timestamp() WHERE run_id=$1 AND position=$2 AND state='waiting'", j.RunID, j.Position+1)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE runs SET state='done',completed_at=clock_timestamp() WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM steps WHERE run_id=$1 AND state<>'done')`, j.RunID)
	if err != nil {
		return err
	}
	if err = event(ctx, tx, j, "committed"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func dead(ctx context.Context, tx pgx.Tx, j Job, reason string) error {
	if _, err := tx.Exec(ctx, "UPDATE steps SET state='dead',last_error=$3,lease_until=NULL WHERE run_id=$1 AND position=$2", j.RunID, j.Position, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE runs SET state='dead',completed_at=clock_timestamp() WHERE id=$1", j.RunID); err != nil {
		return err
	}
	return event(ctx, tx, j, "dead")
}
func Backoff(j Job) time.Duration {
	shift := j.Attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 7 {
		shift = 7
	}
	base := time.Duration(25*(1<<shift)) * time.Millisecond
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", j.RunID, j.Position, j.Attempt)))
	return base + time.Duration(binary.BigEndian.Uint32(sum[:4])%uint32(base.Milliseconds()+1))*time.Millisecond
}
func (e *Engine) Fail(ctx context.Context, j Job, reason string) error {
	// Reasons are enumerated; never persist arbitrary downstream error/payload strings.
	if reason != "synthetic_retry" && reason != "synthetic_permanent" {
		return errors.New("invalid failure reason")
	}
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = verify(ctx, tx, j); err != nil {
		return err
	}
	if j.Attempt >= j.MaxAttempts || reason == "synthetic_permanent" {
		if err = dead(ctx, tx, j, reason); err != nil {
			return err
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE steps SET state='queued',available_at=clock_timestamp()+$3::bigint*interval '1 millisecond',last_error=$4,lease_until=NULL WHERE run_id=$1 AND position=$2`, j.RunID, j.Position, Backoff(j).Milliseconds(), reason)
		if err != nil {
			return err
		}
		if err = event(ctx, tx, j, "retry_scheduled"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (e *Engine) Replay(ctx context.Context, id string) error {
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	cfg, err := control(ctx, tx, true)
	if err != nil {
		return err
	}
	var pending int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM runs WHERE state='running'").Scan(&pending); err != nil {
		return err
	}
	if pending >= cfg.Capacity {
		return ErrCapacity
	}
	var state string
	if err = tx.QueryRow(ctx, "SELECT state FROM runs WHERE id=$1 FOR UPDATE", id).Scan(&state); err != nil {
		return err
	}
	if state != "dead" {
		return ErrNotDead
	}
	_, err = tx.Exec(ctx, `UPDATE steps SET state='queued',attempts=0,owner='',lease_until=NULL,available_at=clock_timestamp(),last_error='' WHERE run_id=$1 AND state='dead'`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE runs SET state='running',completed_at=NULL WHERE id=$1", id)
	if err != nil {
		return err
	}
	if err = event(ctx, tx, Job{RunID: id, Position: -1}, "manually_replayed"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
