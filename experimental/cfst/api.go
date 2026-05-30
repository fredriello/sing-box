package cfst

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sagernet/sing/common/json"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// APIRouter returns the chi router for CFST API endpoints.
func (s *CFSTService) APIRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/status", s.getStatus)
	r.Post("/run", s.postRun)
	r.Get("/results", s.getResults)
	r.Post("/cancel", s.postCancel)
	return r
}

func (s *CFSTService) getStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := map[string]any{
		"running":      s.running,
		"last_run":     s.lastRun,
		"result_count": len(s.results),
		"last_error":   s.lastError,
	}
	render.JSON(w, r, status)
}

type runRequest struct {
	DN    int  `json:"dn"`
	P     int  `json:"p"`
	Force bool `json:"force"`
}

func (s *CFSTService) postRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	s.mu.Lock()
	if s.running && !req.Force {
		s.mu.Unlock()
		render.Status(r, http.StatusConflict)
		render.JSON(w, r, map[string]any{"error": "speed test already running"})
		return
	}
	if s.running && req.Force {
		// Cancel existing run
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.running = true
	s.runID++
	currentRunID := s.runID
	s.mu.Unlock()

	dn := req.DN
	if dn <= 0 {
		dn = s.options.DownloadCount
	}
	if dn <= 0 {
		dn = 10
	}
	p := req.P
	if p <= 0 {
		p = s.options.DisplayCount
	}
	if p <= 0 {
		p = 10
	}

	go s.runSpeedTest(dn, p, currentRunID)

	render.JSON(w, r, map[string]any{"started": true})
}

func (s *CFSTService) getResults(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	results := s.results
	s.mu.Unlock()

	if results == nil {
		results = []Result{}
	}

	format := r.URL.Query().Get("format")
	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	if format == "table" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%-18s %-8s %-8s %-10s %-12s %-18s %-8s\n",
			"IP \xe5\x9c\xb0\xe5\x9d\x80", "\xe5\xb7\xb2\xe5\x8f\x91\xe9\x80\x81", "\xe5\xb7\xb2\xe6\x8e\xa5\xe6\x94\xb6", "\xe4\xb8\xa2\xe5\x8c\x85\xe7\x8e\x87", "\xe5\xb9\xb3\xe5\x9d\x87\xe5\xbb\xb6\xe8\xbf\x9f", "\xe4\xb8\x8b\xe8\xbd\xbd\xe9\x80\x9f\xe5\xba\xa6(MB/s)", "\xe5\x9c\xb0\xe5\x8c\xba\xe7\xa0\x81"))
		for _, res := range results {
			sb.WriteString(fmt.Sprintf("%-18s %-8d %-8d %-10.2f %-12.2f %-18.2f %-8s\n",
				res.IP, res.Sent, res.Received, res.LossRate, res.AvgLatencyMS, res.DownloadSpeedMB, res.Colo))
		}
		_, _ = w.Write([]byte(sb.String()))
		return
	}

	render.JSON(w, r, results)
}

func (s *CFSTService) postCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		render.JSON(w, r, map[string]any{"cancelled": false, "reason": "not running"})
		return
	}

	if s.cancel != nil {
		s.cancel()
	}

	render.JSON(w, r, map[string]any{"cancelled": true})
}
