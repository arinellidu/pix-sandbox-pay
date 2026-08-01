package api

import "net/http"

type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.log.Error("health check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "error"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
