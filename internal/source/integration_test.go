//go:build integration

// Integration tests for the live sources against a real PostgreSQL instance.
// Excluded from normal `go test ./...` / `make ci` (which stay offline); run with:
//
//	go test -tags=integration -cover ./internal/source/
//
// Configure the admin connection via SSA_TEST_ADMIN_DSN (default: local Unix socket).
// The harness creates and drops a throwaway database `ssa_itest` for isolation.
package source

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const itestDB = "ssa_itest"

func adminDSN() string {
	if v := os.Getenv("SSA_TEST_ADMIN_DSN"); v != "" {
		return v
	}
	return "host=/tmp port=5432 user=klein dbname=postgres sslmode=disable"
}

func dbDSN() string {
	if v := os.Getenv("SSA_TEST_DB_DSN"); v != "" {
		return v
	}
	return "host=/tmp port=5432 user=klein dbname=" + itestDB + " sslmode=disable"
}

// setupITestDB creates a throwaway database with a 50k-row unindexed table.
// Returns the DSN; skips the test if PG is unreachable.
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
	t.Cleanup(func() { dropITestDB(t) })

	db, err := pgx.Connect(ctx, dbDSN())
	if err != nil {
		t.Fatalf("connect itest db: %v", err)
	}
	defer db.Close(context.Background())
	for _, q := range []string{
		"CREATE TABLE t (id bigserial PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO t (v) SELECT g FROM generate_series(1, 50000) g", // > seqscan threshold
		"ANALYZE t",
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
}

func dropITestDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminDSN())
	if err != nil {
		return
	}
	defer admin.Close(context.Background())
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+itestDB)
}

// TestPostgresSourceLive covers the full pgx path: connect, statement_timeout,
// read-only tx, EXPLAIN ANALYZE, queryTableStats, rollback, parse.
func TestPostgresSourceLive(t *testing.T) {
	setupITestDB(t)
	src := PostgresSource{
		DSN:     dbDSN(),
		Query:   "SELECT count(*) FROM t WHERE v > 25000",
		Analyze: true,
		Timeout: 30 * time.Second,
	}
	res, err := src.Fetch()
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.IsAnalyze {
		t.Error("expected IsAnalyze=true for ANALYZE run")
	}
	if res.Root == nil {
		t.Fatal("nil root")
	}
	if res.ExecutionTime <= 0 {
		t.Error("expected positive execution time")
	}
	// queryTableStats should have populated TableStats for relation "t".
	if len(res.TableStats) == 0 {
		t.Error("expected TableStats populated for the scanned relation")
	}
}

// TestPostgresSourceNoAnalyze covers the estimate-only path (does not execute).
func TestPostgresSourceNoAnalyze(t *testing.T) {
	setupITestDB(t)
	res, err := PostgresSource{
		DSN: dbDSN(), Query: "SELECT count(*) FROM t", Analyze: false, Timeout: 30 * time.Second,
	}.Fetch()
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.IsAnalyze {
		t.Error("Analyze=false should yield estimate-only plan (IsAnalyze=false)")
	}
}

// TestPostgresSourceWriteGuard covers the write/DDL refusal in live mode.
func TestPostgresSourceWriteGuard(t *testing.T) {
	setupITestDB(t)
	_, err := PostgresSource{DSN: dbDSN(), Query: "UPDATE t SET v = 0", Analyze: true}.Fetch()
	if err == nil {
		t.Error("UPDATE must be refused without AllowWrites")
	}

	// With AllowWrites it runs inside a rolled-back tx (no rows actually changed).
	res, err := PostgresSource{
		DSN: dbDSN(), Query: "UPDATE t SET v = 0", Analyze: true, AllowWrites: true, Timeout: 30 * time.Second,
	}.Fetch()
	if err != nil {
		t.Fatalf("AllowWrites Fetch: %v", err)
	}
	if res.Root == nil {
		t.Error("AllowWrites should still produce a plan")
	}
}

// TestCommandSourcePsqlLive covers the command connector driving a real psql.
func TestCommandSourcePsqlLive(t *testing.T) {
	setupITestDB(t)
	src := CommandSource{
		Cmd:     `psql "{dsn}" -At -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) {sql}"`,
		DSN:     dbDSN(),
		Query:   "SELECT count(*) FROM t",
		Timeout: 30 * time.Second,
	}
	res, err := src.Fetch()
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.IsAnalyze {
		t.Error("command connector should yield ANALYZE plan")
	}
	if res.Root == nil {
		t.Fatal("nil root")
	}
}
