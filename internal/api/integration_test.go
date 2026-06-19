//go:build integration

// Integration tests that drive the full HTTP server (api.Handler) against a
// live PostgreSQL, covering the /v1/analyze success path that unit tests
// (which use no DB) cannot. Run with:
//
//	make test-integration
//
// Creates and drops a throwaway database `ssa_itest_api` (distinct from the
// source package's `ssa_itest` so the two packages can run in parallel).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const itestDB = "ssa_itest_api"

func adminDSN() string {
	if v := os.Getenv("SSA_TEST_ADMIN_DSN"); v != "" {
		return v
	}
	return "host=/tmp port=5432 user=klein dbname=postgres sslmode=disable"
}

func dbDSN() string {
	return "host=/tmp port=5432 user=klein dbname=" + itestDB + " sslmode=disable"
}

func setupITestDB(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminDSN())
	if err != nil {
		t.Skipf("PostgreSQL unreachable (set SSA_TEST_ADMIN_DSN): %v", err)
	}
	defer admin.Close(context.Background())
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+itestDB); err != nil {
		t.Skipf("drop db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+itestDB); err != nil {
		t.Skipf("create db (need CREATEDB priv): %v", err)
	}
	t.Cleanup(func() {
		c, _ := pgx.Connect(ctx, adminDSN())
		if c != nil {
			defer c.Close(context.Background())
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+itestDB)
		}
	})
	db, err := pgx.Connect(ctx, dbDSN())
	if err != nil {
		t.Fatalf("connect itest db: %v", err)
	}
	defer db.Close(context.Background())
	for _, q := range []string{
		"CREATE TABLE t (id bigserial PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO t (v) SELECT g FROM generate_series(1, 50000) g",
		"ANALYZE t",
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
}

func newLiveServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := New(Config{DefaultDSN: dbDSN()})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func postJSON(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// TestServeAnalyzeLive covers the full live path: HTTP -> analyzeQuery ->
// PostgresSource -> EXPLAIN -> report JSON.
func TestServeAnalyzeLive(t *testing.T) {
	setupITestDB(t)
	ts := newLiveServer(t)

	code, out := postJSON(t, ts.URL+"/v1/analyze", map[string]any{
		"query": "SELECT count(*) FROM t WHERE v > 25000",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, out)
	}
	if out["is_analyze"] != true {
		t.Errorf("is_analyze = %v, want true", out["is_analyze"])
	}
	if out["plan_tree"] == nil {
		t.Error("expected plan_tree")
	}
	summary, _ := out["summary"].(map[string]any)
	if summary == nil {
		t.Fatal("missing summary")
	}
	total := 0
	for _, v := range summary {
		if n, ok := v.(float64); ok {
			total += int(n)
		}
	}
	if total == 0 {
		t.Errorf("expected some findings on the unindexed query, summary=%v", summary)
	}
}

// TestServePlanOfflineViaServer hits the offline endpoint through the real
// server (the unit test already covers logic; this covers routing+render in situ).
func TestServePlanOfflineViaServer(t *testing.T) {
	setupITestDB(t) // not strictly needed, keeps parity / exercises server only
	ts := newLiveServer(t)
	code, out := postJSON(t, ts.URL+"/v1/plan", map[string]any{
		"plan": json.RawMessage(`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"t","Plan Rows":1000000,"Actual Rows":1000000,"Actual Loops":1,"Filter":"(v > 0)","Rows Removed by Filter":9000000},"Execution Time":10.0}]`),
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, out)
	}
	if out["plan_tree"] == nil {
		t.Error("expected plan_tree")
	}
}

// TestServeStaticEndpoints checks the non-DB endpoints through the live server.
func TestServeStaticEndpoints(t *testing.T) {
	setupITestDB(t)
	ts := newLiveServer(t)

	for _, path := range []string{"/healthz", "/v1/rules", "/rules", "/"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}
