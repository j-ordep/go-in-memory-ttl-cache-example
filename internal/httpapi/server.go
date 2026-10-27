package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"turnover-api/internal/analytics"
)

type Server struct {
	repo *analytics.Repo
	cat  *analytics.Catalog
}

func NewServer(repo *analytics.Repo, cat *analytics.Catalog) *Server {
	return &Server{repo: repo, cat: cat}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/attributes/{key}/options", s.attributeOptions)

	mux.HandleFunc("POST /api/employees/search", s.searchEmployees)
	mux.HandleFunc("GET /api/employees/{id}", s.employeeDetail)

	mux.HandleFunc("POST /api/dashboard/overview", s.overview)
	mux.HandleFunc("POST /api/dashboard/breakdown", s.breakdown)

	mux.HandleFunc("POST /api/factors/summary", s.factorsSummary)
	mux.HandleFunc("POST /api/factors/{key}/scatter", s.factorScatter)

	mux.HandleFunc("GET /api/turnover/monthly", s.turnoverMonthly)
	mux.HandleFunc("GET /api/turnover/annual", s.turnoverAnnual)

	mux.HandleFunc("POST /api/filters/validate", s.validateFilter)

	// chamado quando a análise em Python termina (ou pelo próprio fluxo
	// que acompanha o status da competência)
	mux.HandleFunc("POST /internal/catalog/reload", s.reloadCatalog)

	return withViewer(mux)
}

// ---------------------------------------------------------------- autenticação

type viewerKey struct{}

// withViewer é um placeholder: troque pela sua autenticação real
// (JWT/sessão). O que importa é colocar no contexto o login = parte
// do e-mail antes do @, o mesmo valor de visible_to.
func withViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := r.Header.Get("X-User-Login")
		ctx := context.WithValue(r.Context(), viewerKey{}, analytics.Viewer{Login: login})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func viewer(r *http.Request) analytics.Viewer {
	v, _ := r.Context().Value(viewerKey{}).(analytics.Viewer)
	return v
}

// ---------------------------------------------------------------- corpo comum

// scopeRequest é o corpo dos endpoints que aceitam filtro.
// O filtro vai no corpo (POST) porque é uma árvore JSON.
type scopeRequest struct {
	Month  string                `json:"month"` // "2026-03"; vazio = último mês processado
	Filter *analytics.FilterNode `json:"filter"`
}

func (s *Server) decodeScope(w http.ResponseWriter, r *http.Request, dst any, sr *scopeRequest) (time.Time, bool) {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return time.Time{}, false
	}
	month, err := s.repo.ResolveMonth(r.Context(), sr.Month)
	if err != nil {
		fail(w, r, err)
		return time.Time{}, false
	}
	return month, true
}

// ---------------------------------------------------------------- handlers

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	cat, err := s.repo.Catalog(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, cat)
}

func (s *Server) attributeOptions(w http.ResponseWriter, r *http.Request) {
	month, err := s.repo.ResolveMonth(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		fail(w, r, err)
		return
	}
	opts, err := s.repo.AttributeOptions(r.Context(), viewer(r), month, r.PathValue("key"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, opts)
}

func (s *Server) searchEmployees(w http.ResponseWriter, r *http.Request) {
	var req struct {
		scopeRequest
		SortBy   string `json:"sortBy"`
		SortDesc bool   `json:"sortDesc"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
	}
	month, ok := s.decodeScope(w, r, &req, &req.scopeRequest)
	if !ok {
		return
	}
	page, err := s.repo.SearchEmployees(r.Context(), viewer(r), analytics.SearchParams{
		Month: month, Filter: req.Filter, SortBy: req.SortBy, SortDesc: req.SortDesc,
		Limit: req.Limit, Offset: req.Offset,
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, page)
}

func (s *Server) employeeDetail(w http.ResponseWriter, r *http.Request) {
	month, err := s.repo.ResolveMonth(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		fail(w, r, err)
		return
	}
	d, err := s.repo.EmployeeDetail(r.Context(), viewer(r), r.PathValue("id"), month)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, d)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	var req scopeRequest
	month, ok := s.decodeScope(w, r, &req, &req)
	if !ok {
		return
	}
	o, err := s.repo.Overview(r.Context(), viewer(r), month, req.Filter)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, o)
}

func (s *Server) breakdown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		scopeRequest
		By      string    `json:"by"`
		Buckets []float64 `json:"buckets"`
	}
	month, ok := s.decodeScope(w, r, &req, &req.scopeRequest)
	if !ok {
		return
	}
	rows, err := s.repo.Breakdown(r.Context(), viewer(r), analytics.BreakdownParams{
		Month: month, Filter: req.Filter, By: req.By, Buckets: req.Buckets,
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, rows)
}

func (s *Server) factorsSummary(w http.ResponseWriter, r *http.Request) {
	var req scopeRequest
	month, ok := s.decodeScope(w, r, &req, &req)
	if !ok {
		return
	}
	out, err := s.repo.FactorsSummary(r.Context(), viewer(r), month, req.Filter)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) factorScatter(w http.ResponseWriter, r *http.Request) {
	var req scopeRequest
	month, ok := s.decodeScope(w, r, &req, &req)
	if !ok {
		return
	}
	out, err := s.repo.FactorScatter(r.Context(), viewer(r), month, r.PathValue("key"), req.Filter)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) turnoverMonthly(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to, err := s.repo.ResolveMonth(r.Context(), q.Get("to"))
	if err != nil {
		fail(w, r, err)
		return
	}
	from := to.AddDate(0, -11, 0)
	if q.Get("from") != "" {
		if from, err = s.repo.ResolveMonth(r.Context(), q.Get("from")); err != nil {
			fail(w, r, err)
			return
		}
	}
	out, err := s.repo.MonthlyTurnover(r.Context(), from, to)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) turnoverAnnual(w http.ResponseWriter, r *http.Request) {
	year, err := strconv.Atoi(r.URL.Query().Get("year"))
	if err != nil || year < 2000 || year > 2100 {
		writeError(w, r, http.StatusBadRequest, "informe year=AAAA")
		return
	}
	out, err := s.repo.AnnualTurnover(r.Context(), year)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

// validateFilter é usado pelo frontend antes de salvar um filtro em
// saved_filters: garante que a árvore compila contra o catálogo atual.
func (s *Server) validateFilter(w http.ResponseWriter, r *http.Request) {
	var f analytics.FilterNode
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&f); err != nil {
		writeError(w, r, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	cat, err := s.repo.Catalog(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := analytics.ValidateFilter(cat, &f); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reloadCatalog(w http.ResponseWriter, r *http.Request) {
	s.cat.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- respostas

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	if status, ok := analytics.IsClientError(err); ok {
		writeError(w, r, status, err.Error())
		return
	}
	slog.Error("erro interno", "path", r.URL.Path, "err", err)
	writeError(w, r, http.StatusInternalServerError, "erro interno")
}
