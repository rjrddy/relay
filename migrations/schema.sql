CREATE TABLE IF NOT EXISTS queue_control (
 id integer PRIMARY KEY CHECK (id=1), paused boolean NOT NULL DEFAULT false,
 rollout integer NOT NULL DEFAULT 100 CHECK(rollout BETWEEN 0 AND 100),
 capacity integer NOT NULL DEFAULT 2000 CHECK(capacity BETWEEN 1 AND 100000),
 version bigint NOT NULL DEFAULT 1
);
INSERT INTO queue_control(id) VALUES(1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS config_versions (
 version bigint PRIMARY KEY, paused boolean NOT NULL, rollout integer NOT NULL,
 capacity integer NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO config_versions VALUES(1,false,100,2000,clock_timestamp()) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS runs (
 id text PRIMARY KEY, idem_key text NOT NULL UNIQUE, fingerprint text NOT NULL,
 definition text NOT NULL, input jsonb NOT NULL, cohort integer NOT NULL,
 state text NOT NULL CHECK(state IN ('running','done','dead')),
 enqueued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 scheduled_at timestamptz NOT NULL, traceparent text NOT NULL,
 completed_at timestamptz
);
CREATE TABLE IF NOT EXISTS steps (
 run_id text NOT NULL REFERENCES runs(id), position integer NOT NULL,
 name text NOT NULL, state text NOT NULL CHECK(state IN ('waiting','queued','running','done','dead')),
 attempts integer NOT NULL DEFAULT 0, max_attempts integer NOT NULL,
 epoch bigint NOT NULL DEFAULT 0, owner text NOT NULL DEFAULT '',
 available_at timestamptz NOT NULL, lease_until timestamptz,
 first_started timestamptz, completed_at timestamptz, result bigint,
 last_error text NOT NULL DEFAULT '', PRIMARY KEY(run_id,position)
);
CREATE INDEX IF NOT EXISTS eligible_steps ON steps(state,available_at,lease_until);
CREATE TABLE IF NOT EXISTS effects (
 run_id text NOT NULL REFERENCES runs(id), position integer NOT NULL,
 result bigint NOT NULL, committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(run_id,position)
);
CREATE TABLE IF NOT EXISTS events (
 seq bigserial PRIMARY KEY, run_id text NOT NULL, position integer NOT NULL DEFAULT -1,
 kind text NOT NULL, owner text NOT NULL DEFAULT '', epoch bigint NOT NULL DEFAULT 0,
 attempt integer NOT NULL DEFAULT 0, at timestamptz NOT NULL DEFAULT clock_timestamp(),
 trace_id text NOT NULL DEFAULT '', span_id text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS run_events ON events(run_id,seq);
