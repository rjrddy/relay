package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rjrddy/relay/internal/engine"
	sdk "github.com/rjrddy/relay/sdk/go"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*httptest.Server, *http.Client, *engine.Engine) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("real TEST_DATABASE_URL required")
	}
	e, err := engine.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_http_" + engine.ID()
	e.Pool.Exec(context.Background(), "CREATE SCHEMA "+schema)
	e.Pool.Close()
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	e, err = engine.Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	app, err := New(e, "synthetic-operator-password", "synthetic-sdk-token-for-tests", "../../../apps/web/dist", false)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(app.Handler())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	t.Cleanup(func() { s.Close(); e.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e.Pool.Close() })
	return s, client, e
}
func call(t *testing.T, c *http.Client, method, base, path, body, csrf string) (int, map[string]any) {
	t.Helper()
	r, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}
func TestCookieAuthCSRFValidationAndLogout(t *testing.T) {
	s, c, _ := testServer(t)
	if code, _ := call(t, c, "GET", s.URL, "/api/runs", "", ""); code != 401 {
		t.Fatal(code)
	}
	code, auth := call(t, c, "POST", s.URL, "/api/login", `{"password":"synthetic-operator-password"}`, "")
	if code != 200 {
		t.Fatal(code)
	}
	csrf := auth["csrf"].(string)
	body := `{"key":"test","input":{"value":7},"max_attempts":3}`
	if code, _ = call(t, c, "POST", s.URL, "/api/runs", body, ""); code != 403 {
		t.Fatal(code)
	}
	if code, _ = call(t, c, "POST", s.URL, "/api/runs", body, csrf); code != 200 {
		t.Fatal(code)
	}
	if code, _ = call(t, c, "POST", s.URL, "/api/runs", `{"unknown":true}`, csrf); code != 400 {
		t.Fatal(code)
	}
	if code, _ = call(t, c, "POST", s.URL, "/api/runs", strings.Repeat("x", 10000), csrf); code != 400 {
		t.Fatal(code)
	}
	if code, _ = call(t, c, "POST", s.URL, "/api/runs", `{"key":"other","max_attempts":3,"input":{"value":999999999}}`, csrf); code != 422 {
		t.Fatal(code)
	}
	call(t, c, "POST", s.URL, "/api/logout", `{}`, csrf)
	if code, _ = call(t, c, "GET", s.URL, "/api/runs", "", ""); code != 401 {
		t.Fatal(code)
	}
}
func TestSDKHTTPAndDatabaseErrors(t *testing.T) {
	s, _, e := testServer(t)
	client := sdk.New(s.URL, "synthetic-sdk-token-for-tests")
	input := sdk.Request{Key: "sdk", Input: sdk.Input{Value: 8}, MaxAttempts: 3}
	first, err := client.Enqueue(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := client.Enqueue(context.Background(), input)
	if err != nil || again.Created || first.ID != again.ID {
		t.Fatal(again, err)
	}
	input.Input.Value++
	if _, err = client.Enqueue(context.Background(), input); err == nil {
		t.Fatal("conflict swallowed")
	}
	raw, err := client.Run(context.Background(), first.ID)
	if err != nil || !strings.Contains(string(raw), first.ID) {
		t.Fatal(err, string(raw))
	}
	e.Pool.Close()
	r, _ := http.NewRequest("GET", s.URL+"/api/metrics", nil)
	r.Header.Set("Authorization", "Bearer synthetic-sdk-token-for-tests")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal(response.StatusCode)
	}
}
func TestLoginRateLimit(t *testing.T) {
	s, c, _ := testServer(t)
	for i := 0; i < 10; i++ {
		code, _ := call(t, c, "POST", s.URL, "/api/login", `{"password":"wrong"}`, "")
		if code != 401 {
			t.Fatal(code)
		}
	}
	code, _ := call(t, c, "POST", s.URL, "/api/login", `{"password":"wrong"}`, "")
	if code != 429 {
		t.Fatal(code)
	}
}
func TestUnknownRouteDoesNotLeakFiles(t *testing.T) {
	s, c, _ := testServer(t)
	r, err := c.Get(s.URL + "/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 404 || strings.Contains(string(body), "root:") {
		t.Fatal(fmt.Sprint(r.StatusCode))
	}
}
