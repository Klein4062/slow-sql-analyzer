package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanEndpointOffline(t *testing.T) {
	planBytes, err := os.ReadFile(filepath.Join("..", "..", "testdata", "seqscan_large.json"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"plan":  json.RawMessage(planBytes),
		"query": "SELECT * FROM orders WHERE status='pending'",
	})

	srv := New(Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v\n%s", err, rec.Body.String())
	}
	if got := resp["is_analyze"]; got != true {
		t.Errorf("is_analyze = %v, want true", got)
	}
	summary, _ := resp["summary"].(map[string]any)
	if summary == nil || summary["critical"] == nil {
		t.Errorf("expected non-empty summary.critical, got %v", resp["summary"])
	}
}

func TestPlanEndpointBadInput(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAnalyzeQueryErrorPaths(t *testing.T) {
	srv := New(Config{})
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad json", `{not json`, http.StatusBadRequest},
		{"missing query", `{}`, http.StatusBadRequest},
		{"no dsn configured", `{"query":"SELECT 1"}`, http.StatusBadRequest}, // no DefaultDSN, connector pgx
		{"command without exec", `{"query":"SELECT 1","connector":"command"}`, http.StatusBadRequest},
		{"unknown connector", `{"query":"SELECT 1","connector":"weird"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader([]byte(c.body)))
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestPlanEndpointBadJSON(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", bytes.NewReader([]byte(`not json`)))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestRulesCatalog verifies the rules reference covers all three categories
// (common / live / offline) and every implemented rule is documented.
func TestRulesCatalog(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/rules", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var infos []struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &infos); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}

	cats := map[string]int{}
	names := map[string]bool{}
	for _, i := range infos {
		cats[i.Category]++
		names[i.Name] = true
	}
	for _, want := range []string{"common", "live", "offline"} {
		if cats[want] == 0 {
			t.Errorf("category %q has no rules; got %+v", want, cats)
		}
	}
	// Every implemented rule must appear in the catalog.
	for _, want := range []string{
		"SeqScanLargeTable", "CardinalityMisestimate", "DiskSort", "HashSpill",
		"NestedLoopExpensiveInner", "InefficientFilter", "LowBufferHitRatio",
		"Hotspot", "StaleStatistics",
	} {
		if !names[want] {
			t.Errorf("catalog missing rule %q", want)
		}
	}
	// StaleStatistics must be documented under both live and offline (its two
	// mode-specific behaviors).
	var staleCats []string
	for _, i := range infos {
		if i.Name == "StaleStatistics" {
			staleCats = append(staleCats, i.Category)
		}
	}
	for _, want := range []string{"live", "offline"} {
		var found bool
		for _, c := range staleCats {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("StaleStatistics should appear under %q; got %v", want, staleCats)
		}
	}
}

// TestRulesPageServed verifies the rules reference HTML page is reachable.
func TestRulesPageServed(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/rules", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "分析规则说明") {
		t.Error("rules page missing title")
	}
}
