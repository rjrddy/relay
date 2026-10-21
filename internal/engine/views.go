package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	Paused   bool  `json:"paused"`
	Rollout  int   `json:"rollout"`
	Capacity int   `json:"capacity"`
	Version  int64 `json:"version"`
}

func (e *Engine) Config(ctx context.Context) (Config, error) {
	var c Config
	err := e.Pool.QueryRow(ctx, "SELECT paused,rollout,capacity,version FROM queue_control WHERE id=1").Scan(&c.Paused, &c.Rollout, &c.Capacity, &c.Version)
	return c, err
}
func (e *Engine) Publish(ctx context.Context, c Config) (Config, error) {
	if c.Rollout < 0 || c.Rollout > 100 || c.Capacity < 1 || c.Capacity > 100000 {
		return c, errors.New("invalid configuration bounds")
	}
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	old, err := control(ctx, tx, true)
	if err != nil {
		return c, err
	}
	if old.Version != c.Version {
		return c, ErrConfigConflict
	}
	c.Version++
	_, err = tx.Exec(ctx, "UPDATE queue_control SET paused=$1,rollout=$2,capacity=$3,version=$4 WHERE id=1", c.Paused, c.Rollout, c.Capacity, c.Version)
	if err != nil {
		return c, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO config_versions(version,paused,rollout,capacity) VALUES($1,$2,$3,$4)", c.Version, c.Paused, c.Rollout, c.Capacity)
	if err != nil {
		return c, err
	}
	if err = event(ctx, tx, Job{RunID: "_control", Position: -1}, "config_published"); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (e *Engine) Rollback(ctx context.Context, target, expected int64) (Config, error) {
	var c Config
	err := e.Pool.QueryRow(ctx, "SELECT paused,rollout,capacity FROM config_versions WHERE version=$1", target).Scan(&c.Paused, &c.Rollout, &c.Capacity)
	if err != nil {
		return c, err
	}
	c.Version = expected
	return e.Publish(ctx, c)
}
func (e *Engine) Previous(ctx context.Context, j Job) (int64, error) {
	if j.Position == 0 {
		return j.Input.Value, nil
	}
	var value int64
	err := e.Pool.QueryRow(ctx, "SELECT result FROM effects WHERE run_id=$1 AND position=$2", j.RunID, j.Position-1).Scan(&value)
	return value, err
}
func (e *Engine) jsonQuery(ctx context.Context, sql string, args ...any) (json.RawMessage, error) {
	var raw []byte
	err := e.Pool.QueryRow(ctx, sql, args...).Scan(&raw)
	return json.RawMessage(raw), err
}
func (e *Engine) Runs(ctx context.Context) (json.RawMessage, error) {
	return e.jsonQuery(ctx, `SELECT coalesce(jsonb_agg(x ORDER BY enqueued_at DESC),'[]'::jsonb) FROM
 (SELECT id,idem_key,definition,input,state,enqueued_at,scheduled_at,completed_at,cohort,
 (SELECT count(*) FROM steps s WHERE s.run_id=r.id AND s.state='done') AS completed_steps,
 (SELECT sum(attempts) FROM steps s WHERE s.run_id=r.id) AS attempts
 FROM runs r ORDER BY enqueued_at DESC LIMIT 100) x`)
}
func (e *Engine) Detail(ctx context.Context, id string) (json.RawMessage, error) {
	return e.jsonQuery(ctx, `SELECT jsonb_build_object('run',to_jsonb(r),
 'steps',coalesce((SELECT jsonb_agg(s ORDER BY position) FROM steps s WHERE s.run_id=r.id),'[]'::jsonb),
 'effects',coalesce((SELECT jsonb_agg(f ORDER BY position) FROM effects f WHERE f.run_id=r.id),'[]'::jsonb),
 'events',coalesce((SELECT jsonb_agg(e ORDER BY seq) FROM (SELECT * FROM events WHERE run_id=r.id ORDER BY seq DESC LIMIT 200) e),'[]'::jsonb))
 FROM runs r WHERE id=$1`, id)
}
func (e *Engine) Metrics(ctx context.Context) (json.RawMessage, error) {
	return e.jsonQuery(ctx, `SELECT jsonb_build_object(
 'running',(SELECT count(*) FROM runs WHERE state='running'),
 'done',(SELECT count(*) FROM runs WHERE state='done'),
 'dead',(SELECT count(*) FROM runs WHERE state='dead'),
 'effects',(SELECT count(*) FROM effects),
 'retries',(SELECT count(*) FROM events WHERE kind='retry_scheduled'),
 'claims',(SELECT count(*) FROM events WHERE kind='claimed'),
 'reclaims',(SELECT count(*) FROM steps WHERE epoch>1),
 'enqueue_start_p50_ms',(SELECT coalesce(percentile_cont(.5) WITHIN GROUP (ORDER BY extract(epoch FROM (s.first_started-r.enqueued_at))*1000),0) FROM runs r JOIN steps s ON r.id=s.run_id AND s.position=0 WHERE s.first_started IS NOT NULL),
 'enqueue_start_p99_ms',(SELECT coalesce(percentile_cont(.99) WITHIN GROUP (ORDER BY extract(epoch FROM (s.first_started-r.enqueued_at))*1000),0) FROM runs r JOIN steps s ON r.id=s.run_id AND s.position=0 WHERE s.first_started IS NOT NULL),
 'latency_samples',(SELECT coalesce(jsonb_agg(ms),'[]'::jsonb) FROM (SELECT extract(epoch FROM (s.first_started-r.enqueued_at))*1000 AS ms FROM runs r JOIN steps s ON r.id=s.run_id AND s.position=0 WHERE s.first_started IS NOT NULL ORDER BY r.enqueued_at DESC LIMIT 100) q))`)
}
func (e *Engine) Reconcile(ctx context.Context) (json.RawMessage, error) {
	return e.jsonQuery(ctx, `SELECT jsonb_build_object(
 'runs',(SELECT count(*) FROM runs), 'done',(SELECT count(*) FROM runs WHERE state='done'),
 'dead',(SELECT count(*) FROM runs WHERE state='dead'),
 'pending',(SELECT count(*) FROM runs WHERE state='running'),
 'effects',(SELECT count(*) FROM effects),
 'missing_effects',(SELECT count(*) FROM steps s LEFT JOIN effects e USING(run_id,position) WHERE s.state='done' AND e.run_id IS NULL),
 'orphan_effects',(SELECT count(*) FROM effects e JOIN steps s USING(run_id,position) WHERE s.state<>'done'),
 'incorrect_receipts',(SELECT count(*) FROM runs r JOIN effects e ON r.id=e.run_id AND e.position=2 WHERE e.result<>(r.input->>'value')::bigint*2),
 'duplicate_effects',(SELECT coalesce(sum(n-1),0) FROM (SELECT count(*) n FROM effects GROUP BY run_id,position HAVING count(*)>1) x),
 'latencies',(SELECT coalesce(jsonb_agg(jsonb_build_object('run',r.id,'key',r.idem_key,'enqueue_start_ms',extract(epoch FROM(s.first_started-r.enqueued_at))*1000,'schedule_adjusted_ms',extract(epoch FROM(s.first_started-r.scheduled_at))*1000)),'[]'::jsonb) FROM runs r JOIN steps s ON r.id=s.run_id AND s.position=0 WHERE s.first_started IS NOT NULL))`)
}
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
func (e *Engine) Versions(ctx context.Context) (json.RawMessage, error) {
	return e.jsonQuery(ctx, `SELECT coalesce(jsonb_agg(x ORDER BY version DESC),'[]'::jsonb) FROM (SELECT * FROM config_versions ORDER BY version DESC LIMIT 50) x`)
}
