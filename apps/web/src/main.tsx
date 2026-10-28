import "./motion";
import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./style.css";
type Input = { value: number; fail_until: number; work_ms: number };
type Submission = {
  key: string;
  input: Input & { permanent_failure: boolean };
  delay_ms: number;
  max_attempts: number;
};
type Run = {
  id: string;
  idem_key: string;
  state: string;
  enqueued_at: string;
  scheduled_at: string;
  completed_steps: number;
  attempts: number;
  input: Input;
};
type Config = {
  paused: boolean;
  rollout: number;
  capacity: number;
  version: number;
};
type Step = {
  position: number;
  name: string;
  state: string;
  attempts: number;
  epoch: number;
  owner: string;
  last_error: string;
  result: number | null;
  lease_until: string | null;
};
type Event = {
  seq: number;
  kind: string;
  at: string;
  position: number;
  epoch: number;
  trace_id: string;
  span_id: string;
  owner: string;
};
type Detail = {
  run: Run;
  steps: Step[];
  events: Event[];
  effects: { position: number; result: number }[];
};
type Metrics = {
  running: number;
  done: number;
  dead: number;
  effects: number;
  retries: number;
  claims: number;
  enqueue_start_p50_ms: number;
  enqueue_start_p99_ms: number;
  latency_samples: number[];
};
function App() {
  const [csrf, setCsrf] = useState(""),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const [runs, setRuns] = useState<Run[]>([]),
    [metrics, setMetrics] = useState<Metrics | null>(null),
    [config, setConfig] = useState<Config | null>(null),
    [versions, setVersions] = useState<Config[]>([]),
    [selected, setSelected] = useState(""),
    [detail, setDetail] = useState<Detail | null>(null),
    [filter, setFilter] = useState("all"),
    [busy, setBusy] = useState(false);
  const [value, setValue] = useState(42),
    [delay, setDelay] = useState(0),
    [fault, setFault] = useState(0),
    [rollout, setRollout] = useState(100);
  const pending = useRef<Submission | null>(null);
  const [unresolved, setUnresolved] = useState("");
  const call = async (path: string, method = "GET", body?: object) => {
    const r = await fetch("/api/" + path, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 401 && path !== "login") setCsrf("");
      throw Error(data.error || "Request failed");
    }
    return data;
  };
  const refresh = async () => {
    const [r, m, c, v] = await Promise.all([
      call("runs"),
      call("metrics"),
      call("control"),
      call("versions"),
    ]);
    setRuns(r);
    setMetrics(m);
    setConfig(c);
    setVersions(v);
  };
  const action = async (f: () => Promise<void>) => {
    setError("");
    setNotice("");
    setBusy(true);
    try {
      await f();
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    try {
      const saved = JSON.parse(
        sessionStorage.getItem("relay-pending") || "null",
      );
      if (
        saved &&
        typeof saved.key === "string" &&
        saved.key.length <= 128 &&
        Number.isSafeInteger(saved.input?.value)
      ) {
        pending.current = saved;
        setUnresolved(saved.key);
        setValue(saved.input.value);
        setFault(saved.input.fail_until);
        setDelay(saved.delay_ms);
      }
    } catch {
      sessionStorage.removeItem("relay-pending");
    }
    fetch("/api/session")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d) setCsrf(d.csrf);
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (!csrf) return;
    refresh().catch((e) => setError(String(e)));
    const timer = setInterval(
      () => refresh().catch((e) => setError(String(e))),
      1500,
    );
    return () => clearInterval(timer);
  }, [csrf]);
  useEffect(() => {
    setDetail(null);
    if (!csrf || !selected) return;
    let active = true;
    const load = async () => {
      try {
        const data = await call("runs/" + selected);
        if (active) setDetail(data);
      } catch (e) {
        if (active) setError(String(e));
      }
    };
    load();
    const timer = setInterval(load, 1500);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [csrf, selected]);
  useEffect(() => {
    if (config) setRollout(config.rollout);
  }, [config?.version]);
  const enqueue = async (burst = false) => {
    let last = "",
      created = true;
    for (let i = 0; i < (burst ? 10 : 1); i++) {
      const f = burst ? (i % 3 === 0 ? 1 : 0) : fault;
      if (!pending.current)
        pending.current = {
          key: "console-" + crypto.randomUUID(),
          input: {
            value: burst ? 100 + i : value,
            fail_until: f,
            permanent_failure: false,
            work_ms: 250,
          },
          delay_ms: burst ? (i % 3) * 500 : delay,
          max_attempts: f === 2 ? 2 : 4,
        };
      sessionStorage.setItem("relay-pending", JSON.stringify(pending.current));
      setUnresolved(pending.current.key);
      const r = await call("runs", "POST", pending.current);
      pending.current = null;
      sessionStorage.removeItem("relay-pending");
      setUnresolved("");
      last = r.id;
      created = r.created;
      setSelected(last);
    }
    setNotice(
      burst
        ? "Synthetic burst acknowledged. Workers will claim eligible steps."
        : created
          ? "Workflow admitted with an immutable input and idempotency key."
          : "Existing workflow acknowledged; no duplicate admitted.",
    );
  };
  if (!csrf)
    return (
      <div className="login">
        <div className="brand">
          <span className="logo">↗</span> RELAY
        </div>
        <p className="eyebrow">RELAY / THE DISPATCH OFFICE</p>
        <h1>
          Work survives.
          <br />
          Even when workers don’t.
        </h1>
        <p>
          Leased steps. Fenced commits. A durable record
          <br />
          of what happened, and what happens next.
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            action(async () => {
              const d = await call("login", "POST", { password });
              setCsrf(d.csrf);
              setPassword("");
            });
          }}
        >
          <label htmlFor="password">Operator password</label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
          <button disabled={busy}>Enter workspace ↗</button>
        </form>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <small>Original engineering · Synthetic workflows only</small>
      </div>
    );
  const filtered = runs.filter((r) => filter === "all" || r.state === filter);
  const latencies = metrics?.latency_samples ?? [];
  return (
    <div className="shell">
      <aside aria-label="Dispatch navigation">
        <div className="brand">
          <span className="logo">↗</span> RELAY
        </div>
        <p className="eyebrow">DISPATCH OFFICE</p>
        <a href="#overview" className="selected">
          ◈ &nbsp; Workflow overview
        </a>
        <a href="#runs">↗ &nbsp; Runs & dead letters</a>
        <a href="#controls">⌘ &nbsp; Rollout controls</a>
        <a href="#detail">≋ &nbsp; Execution timeline</a>
        <div className="aside-bottom">
          <span className="dot" /> Postgres is authoritative
          <br />
          <small>Leases · epochs · atomic effects</small>
          <button
            className="subtle"
            onClick={() =>
              action(async () => {
                await call("logout", "POST", {});
                setCsrf("");
              })
            }
          >
            Sign out
          </button>
        </div>
      </aside>
      <main id="overview">
        <header>
          <span>
            WORKSPACE <b>/</b> RELAY OPERATIONS
          </span>
          <span className="badge">SYNTHETIC PIPELINE · V1</span>
        </header>
        <section className="title">
          <div>
            <p className="eyebrow">DEPARTURES / WORKFLOW DISPATCH</p>
            <h1>
              Every step accounted for<span>.</span>
            </h1>
            <p>Follow the work from admission to its final receipt.</p>
          </div>
          <span className={"status " + (config?.paused ? "paused" : "")}>
            <span className="dot" />
            {config?.paused ? "Dispatch paused" : "Dispatch enabled"}
          </span>
        </section>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="notice" role="status">
            {notice}
          </p>
        )}
        <section className="stats">
          <article>
            <label>UNFINISHED RUNS</label>
            <strong>
              {metrics?.running ?? "—"}
              <small> / {config?.capacity ?? "—"}</small>
            </strong>
            <p>Delayed & in-flight included</p>
          </article>
          <article>
            <label>COMPLETED / DEAD LETTERS</label>
            <strong>
              {metrics?.done ?? "—"}
              <small> / {metrics?.dead ?? "—"}</small>
            </strong>
            <p>Persisted terminal states</p>
          </article>
          <article>
            <label>ENQUEUE → FIRST CLAIM / P99</label>
            <strong>
              {metrics?.enqueue_start_p99_ms.toFixed(1) ?? "—"}
              <small> ms</small>
            </strong>
            <p>Includes scheduled waiting</p>
          </article>
          <article>
            <label>COMMITTED STEP EFFECTS</label>
            <strong>{metrics?.effects ?? "—"}</strong>
            <p>{metrics?.retries ?? "—"} retry transitions recorded</p>
          </article>
        </section>
        <div className="two-col">
          <section className="panel">
            <div className="panel-title">
              <h2>Start a workflow</h2>
              <span className="eyebrow">SYNTHETIC DATA</span>
            </div>
            <form
              className="enqueue"
              onSubmit={(e) => {
                e.preventDefault();
                action(() => enqueue());
              }}
            >
              {unresolved && (
                <p className="notice" role="status">
                  Admission is awaiting acknowledgement. Retry preserves the
                  original input and key, including after reload.
                  <br />
                  <code>{unresolved}</code>
                </p>
              )}
              <fieldset disabled={busy || Boolean(unresolved)}>
                <div className="fields">
                  <label htmlFor="value">
                    Input value
                    <input
                      id="value"
                      type="number"
                      min={-1000000}
                      max={1000000}
                      value={value}
                      onChange={(e) => setValue(Number(e.target.value))}
                      required
                    />
                  </label>
                  <label htmlFor="delay">
                    Schedule delay
                    <select
                      id="delay"
                      value={delay}
                      onChange={(e) => setDelay(Number(e.target.value))}
                    >
                      <option value={0}>Immediately</option>
                      <option value={2000}>After 2 seconds</option>
                      <option value={10000}>After 10 seconds</option>
                    </select>
                  </label>
                  <label htmlFor="fault">
                    Failure injection
                    <select
                      id="fault"
                      value={fault}
                      onChange={(e) => setFault(Number(e.target.value))}
                    >
                      <option value={0}>None</option>
                      <option value={1}>Retry first calculate claim</option>
                      <option value={2}>Exhaust budget → dead letter</option>
                    </select>
                  </label>
                </div>
              </fieldset>
              <div className="pipeline">
                <span>Validate</span>
                <i>→</i>
                <span>Calculate × 2</span>
                <i>→</i>
                <span>Receipt</span>
              </div>
              <div className="form-actions">
                <button disabled={busy}>
                  {unresolved
                    ? "↗ Retry pending workflow"
                    : "↗ Enqueue workflow"}
                </button>
                <button
                  type="button"
                  className="secondary"
                  disabled={busy || Boolean(unresolved)}
                  onClick={() => action(() => enqueue(true))}
                >
                  Admit demo burst
                </button>
              </div>
              <p className="footnote">
                Retries keep the same run. A successful step’s effect is never
                replayed.
              </p>
            </form>
          </section>
          <section className="panel" id="controls">
            <div className="panel-title">
              <h2>Dispatch controls</h2>
              <span className="pill">revision {config?.version ?? "—"}</span>
            </div>
            <div className="control-content">
              <div className="switch-row">
                <div>
                  <strong>Kill switch</strong>
                  <p>Pause new claims and effect commits.</p>
                </div>
                <button
                  className={config?.paused ? "" : "danger"}
                  disabled={busy}
                  onClick={() =>
                    action(async () => {
                      await call("control", "PUT", {
                        ...config,
                        paused: !config?.paused,
                      });
                      setNotice(
                        config?.paused
                          ? "Dispatch resumed."
                          : "Dispatch paused. Previously committed work is preserved.",
                      );
                    })
                  }
                >
                  {config?.paused ? "Resume dispatch" : "Pause dispatch"}
                </button>
              </div>
              <label htmlFor="rollout">
                Stable cohort rollout <b>{rollout}%</b>
              </label>
              <input
                id="rollout"
                type="range"
                min={0}
                max={100}
                value={rollout}
                onChange={(e) => setRollout(Number(e.target.value))}
              />
              <div className="form-actions">
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() =>
                    action(async () => {
                      await call("control", "PUT", { ...config, rollout });
                      setNotice("New configuration revision published.");
                    })
                  }
                >
                  Publish rollout
                </button>
                <button
                  className="link"
                  disabled={busy || versions.length < 2}
                  onClick={() =>
                    action(async () => {
                      await call("control/rollback", "POST", {
                        target: versions[1].version,
                        expected: config?.version,
                      });
                      setNotice(
                        "Previous settings restored as a new audited revision.",
                      );
                    })
                  }
                >
                  Rollback previous ↶
                </button>
              </div>
              <p className="footnote">
                Configuration writes use version checks. A stale operator cannot
                overwrite a newer revision.
              </p>
            </div>
          </section>
        </div>
        <section className="panel" id="runs">
          <div className="panel-title">
            <h2>
              Runs & dead letters <small>Most recent admission history</small>
            </h2>
            <select
              aria-label="Run state filter"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            >
              <option value="all">All states</option>
              <option value="running">Running / delayed</option>
              <option value="dead">Dead letters</option>
              <option value="done">Completed</option>
            </select>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Workflow</th>
                  <th>Input</th>
                  <th>State</th>
                  <th>Steps</th>
                  <th>Attempts</th>
                  <th>Admitted</th>
                </tr>
              </thead>
              <tbody>
                {filtered.slice(0, 30).map((r) => (
                  <tr
                    key={r.id}
                    className={selected === r.id ? "active-row" : ""}
                  >
                    <td>
                      <button
                        className="link mono"
                        onClick={() => setSelected(r.id)}
                      >
                        {r.id.slice(0, 10)} ↗
                      </button>
                      <small className="key">{r.idem_key.slice(0, 26)}</small>
                    </td>
                    <td>{r.input.value}</td>
                    <td>
                      <span className={"pill " + r.state}>{r.state}</span>
                    </td>
                    <td>{r.completed_steps} / 3</td>
                    <td>{r.attempts}</td>
                    <td>{new Date(r.enqueued_at).toLocaleTimeString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!filtered.length && (
            <p className="empty">
              No workflows in this view. Admit one to begin.
            </p>
          )}
        </section>
        <div className="two-col detail-grid">
          <section className="panel" id="detail">
            <div className="panel-title">
              <h2>
                Execution timeline
                {detail && (
                  <small className="mono">
                    Run {detail.run.id.slice(0, 10)}
                  </small>
                )}
              </h2>
              {detail && (
                <button
                  className="secondary"
                  disabled={busy || detail.run.state !== "dead"}
                  onClick={() =>
                    action(async () => {
                      await call(
                        "runs/" + detail.run.id + "/replay",
                        "POST",
                        {},
                      );
                      setNotice(
                        "Failed step requeued. Completed prefix and effect keys preserved.",
                      );
                    })
                  }
                >
                  Replay failed step
                </button>
              )}
            </div>
            {detail ? (
              <>
                <div className="step-list">
                  {detail.steps.map((s) => (
                    <div className="step" key={s.position}>
                      <span className={"step-icon " + s.state}>
                        {s.state === "done" ? "✓" : s.position + 1}
                      </span>
                      <div>
                        <strong>{s.name}</strong>
                        <p>
                          epoch {s.epoch} · attempt {s.attempts}
                          {s.result !== null ? ` · result ${s.result}` : ""}
                        </p>
                      </div>
                      <span className={"pill " + s.state}>{s.state}</span>
                    </div>
                  ))}
                </div>
                <div className="events">
                  {detail.events
                    .slice(-12)
                    .reverse()
                    .map((e) => (
                      <div className="event" key={e.seq}>
                        <span className="event-dot" />
                        <div>
                          <strong>{e.kind.replaceAll("_", " ")}</strong>
                          <small>
                            {new Date(e.at).toLocaleTimeString()} · step{" "}
                            {e.position < 0 ? "—" : e.position + 1} · epoch{" "}
                            {e.epoch}
                          </small>
                          <code title={e.trace_id}>
                            trace {e.trace_id.slice(0, 20)}…
                          </code>
                        </div>
                      </div>
                    ))}
                </div>
              </>
            ) : (
              <p className="empty">
                {selected
                  ? "Loading persisted workflow history…"
                  : "Select a workflow to inspect its steps, lease epochs and trace-linked events."}
              </p>
            )}
          </section>
          <section className="panel">
            <div className="panel-title">
              <h2>Queue latency distribution</h2>
              <span className="eyebrow">RECENT CLAIMS</span>
            </div>
            <div className="histogram">
              {[10, 50, 100, 500, 1000, 5000, Infinity].map((v, i, all) => {
                const n = latencies.filter(
                  (x) => x <= v && x > (all[i - 1] ?? -1),
                ).length;
                return (
                  <div key={i}>
                    <i
                      style={{
                        height:
                          Math.max(
                            3,
                            (n / Math.max(1, latencies.length)) * 130,
                          ) + "px",
                      }}
                    />
                    <small>
                      {v === Infinity
                        ? ">5s"
                        : v >= 1000
                          ? `≤${v / 1000}s`
                          : `≤${v}`}
                    </small>
                  </div>
                );
              })}
            </div>
            <p className="footnote">
              Milliseconds, enqueue to first step claim. Scheduled delay is
              included; benchmark artifacts also report schedule-adjusted
              latency.
            </p>
            <div className="guarantee">
              <span>THE COMMIT BOUNDARY</span>
              <h3>
                Repeated execution.
                <br />
                One internal effect.
              </h3>
              <p>
                Each effect and its step transition commit in the same Postgres
                transaction. Stale lease holders are fenced by owner, epoch and
                expiry.
              </p>
              <small>
                External HTTP/email tools still require downstream idempotency.
              </small>
            </div>
          </section>
        </div>
        <footer>
          RELAY / DURABILITY YOU CAN INSPECT
          <span>Polling persisted state · Synthetic workflows only</span>
        </footer>
      </main>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
