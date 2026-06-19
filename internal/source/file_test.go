package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileSourceFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`[{"Plan":{"Node Type":"Seq Scan","Plan Rows":5},"Execution Time":0.1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := FileSource{Path: path, Query: "SELECT 1"}.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.NodeType != "Seq Scan" {
		t.Errorf("root = %q", res.Root.NodeType)
	}
	if res.SourceQuery != "SELECT 1" {
		t.Errorf("SourceQuery = %q", res.SourceQuery)
	}
}

func TestFileSourceMissingFile(t *testing.T) {
	if _, err := (FileSource{Path: "/no/such/file.json"}).Fetch(); err == nil {
		t.Error("missing file -> error")
	}
}

func TestFileSourceBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte(`not json`), 0o644)
	if _, err := (FileSource{Path: path}).Fetch(); err == nil {
		t.Error("bad json -> error")
	}
}

func TestFileSourceStdin(t *testing.T) {
	// Redirect stdin to a pipe with JSON content.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	go func() { w.Write([]byte(`[{"Plan":{"Node Type":"Result","Plan Rows":1}}]`)); w.Close() }()

	res, err := (FileSource{Path: ""}).Fetch() // empty Path -> stdin
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.NodeType != "Result" {
		t.Errorf("root = %q", res.Root.NodeType)
	}
}

func TestFileSourceEmptyStdin(t *testing.T) {
	r, w, _ := os.Pipe()
	w.Close() // empty
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	if _, err := (FileSource{Path: "-"}).Fetch(); err == nil {
		t.Error("empty stdin -> error")
	}
}

// postgres.go helpers
func TestBareRelationNames(t *testing.T) {
	got := bareRelationNames([]string{"public.orders", "orders", "public.users", ""})
	want := []string{"orders", "users"}
	if len(got) != 2 || got[0] != "orders" || got[1] != "users" {
		t.Errorf("bareRelationNames = %v, want %v", got, want)
	}
}

func TestLastDot(t *testing.T) {
	if lastDot("a.b.c") != 3 {
		t.Error("lastDot a.b.c")
	}
	if lastDot("abc") != -1 {
		t.Error("lastDot abc -> -1")
	}
}

func TestPostgresSourceGuardWrite(t *testing.T) {
	// write/DDL guard refuses unless AllowWrites.
	if err := (PostgresSource{Query: "UPDATE t SET x=1"}).guardWrite(); err == nil {
		t.Error("UPDATE should be refused without --allow-writes")
	}
	if err := (PostgresSource{Query: "UPDATE t SET x=1", AllowWrites: true}).guardWrite(); err != nil {
		t.Error("with AllowWrites should pass guard")
	}
	if err := (PostgresSource{Query: "SELECT 1"}).guardWrite(); err != nil {
		t.Error("SELECT should pass guard")
	}
	if err := (PostgresSource{}).guardWrite(); err != nil {
		t.Error("empty query passes guard (Fetch checks separately)")
	}
}

func TestPostgresTimeoutOrDefault(t *testing.T) {
	if got := (PostgresSource{}).timeoutOr(); got.String() != "30s" {
		t.Errorf("default timeout 30s, got %v", got)
	}
	if got := (PostgresSource{Timeout: 0}).timeoutOr(); got.String() != "30s" {
		t.Errorf("0 -> default 30s")
	}
}

func TestFirstWord(t *testing.T) {
	cases := map[string]string{
		"SELECT *":   "SELECT",
		"  UPDATE t": "UPDATE",
		"WITH x":     "WITH",
		"(subquery)": "",
		"abc":        "abc",
	}
	for in, want := range cases {
		if got := firstWord(in); got != want {
			t.Errorf("firstWord(%q) = %q want %q", in, got, want)
		}
	}
}
