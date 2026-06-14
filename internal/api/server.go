// Package api exposes the analyzer over HTTP. /v1/plan analyzes a supplied
// EXPLAIN document (no database needed); /v1/analyze runs EXPLAIN against a
// configured PostgreSQL instance.
package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Klein4062/slow-sql-analyzer/internal/advise"
	"github.com/Klein4062/slow-sql-analyzer/internal/analyzer"
	"github.com/Klein4062/slow-sql-analyzer/internal/config"
	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
	"github.com/Klein4062/slow-sql-analyzer/internal/report"
	"github.com/Klein4062/slow-sql-analyzer/internal/rules"
	"github.com/Klein4062/slow-sql-analyzer/internal/source"
)

//go:embed ui/index.html
var indexHTML []byte

// Config configures the server.
type Config struct {
	// DefaultDSN is used for /v1/analyze when the request omits a DSN.
	DefaultDSN string
	// WriteTimeout bounds how long a single EXPLAIN may run server-side.
	WriteTimeout time.Duration
}

// Server is the HTTP API server.
type Server struct {
	cfg      Config
	analyzer *analyzer.Analyzer
}

// New builds a Server.
func New(cfg Config) *Server {
	return &Server{cfg: cfg, analyzer: analyzer.New(rules.Default())}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Timeout(2 * time.Minute))

	r.Get("/", s.ui)
	r.Get("/ui", s.ui)
	r.Get("/healthz", s.health)
	r.Post("/v1/plan", s.analyzePlan)
	r.Post("/v1/analyze", s.analyzeQuery)
	return r
}

// ui serves the single-page web UI.
func (s *Server) ui(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(indexHTML)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type requestOptions struct {
	DisableRules []string `json:"disable_rules"`
}

type planRequest struct {
	Plan    json.RawMessage `json:"plan"`
	Query   string          `json:"query"`
	Options requestOptions   `json:"options"`
}

type queryRequest struct {
	Query       string         `json:"query"`
	DSN         string         `json:"dsn"`
	Analyze     *bool          `json:"analyze"`
	AllowWrites bool           `json:"allow_writes"`
	Timeout     string         `json:"timeout"`
	Options     requestOptions `json:"options"`
}

// analyzePlan analyzes an EXPLAIN document supplied in the request body.
func (s *Server) analyzePlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON body: %v", err)
		return
	}
	if len(req.Plan) == 0 {
		httpError(w, http.StatusBadRequest, "missing 'plan' field (EXPLAIN FORMAT JSON)")
		return
	}
	result, err := plan.Parse(req.Plan)
	if err != nil {
		httpError(w, http.StatusBadRequest, "could not parse plan: %v", err)
		return
	}
	result.SourceQuery = req.Query
	s.respondReport(w, result, req.Query, req.Options)
}

// analyzeQuery runs EXPLAIN against PostgreSQL and analyzes the result.
func (s *Server) analyzeQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON body: %v", err)
		return
	}
	if req.Query == "" {
		httpError(w, http.StatusBadRequest, "missing 'query' field")
		return
	}
	dsn := req.DSN
	if dsn == "" {
		dsn = s.cfg.DefaultDSN
	}
	if dsn == "" {
		httpError(w, http.StatusBadRequest, "no DSN configured (set server --dsn or pass 'dsn' in the request)")
		return
	}

	analyze := true
	if req.Analyze != nil {
		analyze = *req.Analyze
	}
	timeout := s.cfg.WriteTimeout
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}

	src := source.PostgresSource{
		DSN:         dsn,
		Query:       req.Query,
		Analyze:     analyze,
		AllowWrites: req.AllowWrites,
		Timeout:     timeout,
	}
	result, err := src.Fetch()
	if err != nil {
		httpError(w, http.StatusBadGateway, "EXPLAIN failed: %v", err)
		return
	}
	s.respondReport(w, result, req.Query, req.Options)
}

// respondReport runs the analyzer over result and writes the JSON report.
func (s *Server) respondReport(w http.ResponseWriter, result *plan.PlanResult, query string, opts requestOptions) {
	cfg := config.Default()
	for _, name := range opts.DisableRules {
		cfg.Options.DisabledRules[name] = true
	}
	rep := s.analyzer.Run(result, cfg)
	model := report.Model{
		Result:   result,
		Findings: rep.Findings,
		Actions:  advise.Actions(rep.Findings),
		Query:    query,
	}
	data, err := report.RenderJSON(model)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "render report: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}
