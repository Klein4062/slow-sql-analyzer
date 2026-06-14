package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestHealthz(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
