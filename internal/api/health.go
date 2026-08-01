package api

import "net/http"

// healthResponse carries the version because a CI run needs to know which
// build answered its tests. A failing pipeline that cannot name the binary it
// ran against is a pipeline nobody can debug.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.log.Error("health check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "error", Version: s.cfg.Version})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: s.cfg.Version})
}
