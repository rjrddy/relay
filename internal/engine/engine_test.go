package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"testing/quick"
	"time"
)

func fixture(t *testing.T) *Engine {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required: integration tests never silently skip")
	}
	ctx := context.Background()
	e, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + ID()
	if _, err = e.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	e.Pool.Close()
	// A per-test search_path makes parallel tests independent without deleting user tables.
	sep := "&"
	if !containsQuestion(dsn) {
		sep = "?"
	}
	e, err = Open(ctx, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e.Pool.Close() })
	return e
}
func containsQuestion(s string) bool {
	for _, c := range s {
		if c == '?' {
			return true
		}
	}
	return false
}
func req(key string) Request { return Request{Key: key, Input: Input{Value: 7}, MaxAttempts: 3} }
func enqueue(t *testing.T, e *Engine, key string) string {
	t.Helper()
	id, _, err := e.Enqueue(context.Background(), req(key))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func claim(t *testing.T, e *Engine, owner string) *Job {
	t.Helper()
	j, err := e.Claim(context.Background(), owner)
	if err != nil || j == nil {
		t.Fatalf("claim: %v %v", j, err)
	}
	return j
}
func TestIdempotencyCapacityConflict(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	cfg, _ := e.Config(ctx)
	cfg.Capacity = 1
	e.Publish(ctx, cfg)
	id := enqueue(t, e, "same")
	again, created, err := e.Enqueue(ctx, req("same"))
	if err != nil || created || id != again {
		t.Fatal("dedupe failed", err)
	}
	changed := req("same")
	changed.Input.Value++
	if _, _, err = e.Enqueue(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, _, err = e.Enqueue(ctx, req("full")); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}
func TestLeaseFencingAndAtomicEffects(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	e.Lease = 70 * time.Millisecond
	id := enqueue(t, e, "fenced")
	a := claim(t, e, "a")
	time.Sleep(90 * time.Millisecond)
	b := claim(t, e, "b")
	if b.Epoch <= a.Epoch {
		t.Fatal("epoch did not advance")
	}
	if err := e.Complete(ctx, *a, 999); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if err := e.Heartbeat(ctx, *a); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if err := e.Fail(ctx, *a, "synthetic_retry"); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if err := e.Complete(ctx, *b, 7); err != nil {
		t.Fatal(err)
	}
	if err := e.Complete(ctx, *b, 7); !errors.Is(err, ErrFenced) {
		t.Fatal("duplicate completion admitted", err)
	}
	for n := 1; n < 3; n++ {
		j := claim(t, e, "b")
		v, _ := e.Previous(ctx, *j)
		if n == 1 {
			v *= 2
		}
		if err := e.Complete(ctx, *j, v); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := e.Detail(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Effects []any `json:"effects"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Effects) != 3 {
		t.Fatal(string(raw))
	}
}
func TestConcurrentAdmissionAndClaims(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, err := e.Enqueue(ctx, req("concurrent"))
			if err != nil {
				errs <- err
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatal(seen)
	}
	results := make(chan *Job, 12)
	claimErrs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j, err := e.Claim(ctx, fmt.Sprint(i))
			if err != nil {
				claimErrs <- err
			}
			results <- j
		}(i)
	}
	wg.Wait()
	close(results)
	close(claimErrs)
	for err := range claimErrs {
		t.Fatal(err)
	}
	claimed := 0
	for j := range results {
		if j != nil {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatal("concurrent double claim", claimed)
	}
}
func TestDelayPauseRolloutAndRollback(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	r := req("later")
	r.DelayMS = 90
	e.Enqueue(ctx, r)
	if j, _ := e.Claim(ctx, "a"); j != nil {
		t.Fatal("scheduled too early")
	}
	c, _ := e.Config(ctx)
	old := c.Version
	c.Rollout = 0
	c, _ = e.Publish(ctx, c)
	time.Sleep(100 * time.Millisecond)
	if j, _ := e.Claim(ctx, "a"); j != nil {
		t.Fatal("rollout ignored")
	}
	c, err := e.Rollback(ctx, old, c.Version)
	if err != nil {
		t.Fatal(err)
	}
	j := claim(t, e, "a")
	c.Paused = true
	c, _ = e.Publish(ctx, c)
	if err = e.Complete(ctx, *j, 7); !errors.Is(err, ErrPaused) {
		t.Fatal(err)
	}
	if next, _ := e.Claim(ctx, "b"); next != nil {
		t.Fatal("kill switch ignored")
	}
	c.Paused = false
	e.Publish(ctx, c)
	if err = e.Complete(ctx, *j, 7); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Publish(ctx, c); !errors.Is(err, ErrConfigConflict) {
		t.Fatal("stale config accepted", err)
	}
}
func TestDeadLetterReplayPreservesPreviousEffect(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	id := enqueue(t, e, "replay")
	a := claim(t, e, "a")
	if err := e.Complete(ctx, *a, 7); err != nil {
		t.Fatal(err)
	}
	b := claim(t, e, "a")
	if err := e.Fail(ctx, *b, "synthetic_permanent"); err != nil {
		t.Fatal(err)
	}
	if err := e.Replay(ctx, id); err != nil {
		t.Fatal(err)
	}
	c := claim(t, e, "a")
	if c.Position != 1 || c.Epoch <= b.Epoch {
		t.Fatal("replayed successful step or reset fence")
	}
	if err := e.Complete(ctx, *b, 999); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if err := e.Complete(ctx, *c, 14); err != nil {
		t.Fatal(err)
	}
	d := claim(t, e, "a")
	e.Complete(ctx, *d, 14)
	if err := e.Replay(ctx, id); !errors.Is(err, ErrNotDead) {
		t.Fatal(err)
	}
}
func TestExpiredLeaseCannotHeartbeatOrComplete(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	e.Lease = 30 * time.Millisecond
	enqueue(t, e, "expired")
	j := claim(t, e, "a")
	time.Sleep(50 * time.Millisecond)
	if err := e.Heartbeat(ctx, *j); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if err := e.Complete(ctx, *j, 999); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
}
func TestCrashBudgetEventuallyDeadLetters(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	e.Lease = 20 * time.Millisecond
	r := req("crashbudget")
	r.MaxAttempts = 1
	e.Enqueue(ctx, r)
	claim(t, e, "crashed")
	time.Sleep(35 * time.Millisecond)
	if j, err := e.Claim(ctx, "recover"); err != nil || j != nil {
		t.Fatal(j, err)
	}
	raw, _ := e.Reconcile(ctx)
	var q map[string]any
	json.Unmarshal(raw, &q)
	if q["dead"] != float64(1) {
		t.Fatal(string(raw))
	}
}
func TestBackoffAndCohortProperties(t *testing.T) {
	err := quick.Check(func(attempt uint8, id uint64) bool {
		j := Job{RunID: fmt.Sprint(id), Position: 1, Attempt: int(attempt%10) + 1}
		a := Backoff(j)
		return a == Backoff(j) && a >= 25*time.Millisecond && a <= 6401*time.Millisecond && Cohort(j.RunID) >= 0 && Cohort(j.RunID) < 100
	}, &quick.Config{MaxCount: 1000, Rand: rand.New(rand.NewSource(42))})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitForLockCannotReviveExpiredLease(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	e.Lease = 80 * time.Millisecond
	enqueue(t, e, "locked")
	j := claim(t, e, "a")
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT * FROM steps WHERE run_id=$1 AND position=0 FOR UPDATE", j.RunID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- e.Heartbeat(ctx, *j) }()
	time.Sleep(110 * time.Millisecond)
	tx.Commit(ctx)
	if err = <-result; !errors.Is(err, ErrFenced) {
		t.Fatal("expired lease revived after lock wait", err)
	}
}
