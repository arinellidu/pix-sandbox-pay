package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// tokenTTL is how long an issued sandbox token claims to be valid.
const tokenTTL = 3600

// defaultScope mirrors the scopes a PSP hands out for the API Pix surface.
const defaultScope = "cob.read cob.write pix.read pix.write webhook.read webhook.write"

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// oauthError is the RFC 6749 §5.2 error body.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type tokenRequest struct {
	GrantType string `json:"grant_type"`
	ClientID  string `json:"client_id"`
	Scope     string `json:"scope"`
}

// handleToken is a mock client-credentials endpoint. It validates the grant
// type and hands back a fake bearer token; no credential is ever rejected —
// this is a sandbox, and auth is not what it is here to emulate.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, oauthError{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	if req.GrantType != "client_credentials" {
		writeJSON(w, http.StatusBadRequest, oauthError{
			Error:            "unsupported_grant_type",
			ErrorDescription: "only client_credentials is supported",
		})
		return
	}

	scope := req.Scope
	if scope == "" {
		scope = defaultScope
	}
	// Deterministic: the token comes from the seeded source, so a given seed
	// always yields the same token sequence.
	token := "sandbox_" + s.rng.Hex(24)

	clientID := req.ClientID
	if clientID == "" {
		if id, _, ok := r.BasicAuth(); ok {
			clientID = id
		}
	}
	if _, err := s.store.AppendEvent(r.Context(), "oauth", "oauth.token.issued", map[string]any{
		"client_id":  clientID,
		"scope":      scope,
		"expires_in": tokenTTL,
	}); err != nil {
		s.log.Error("append oauth event", "err", err)
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   tokenTTL,
		Scope:       scope,
	})
}

// parseTokenRequest accepts the RFC-mandated form encoding and, as a
// convenience for curl-driven demos, a JSON body.
func parseTokenRequest(r *http.Request) (tokenRequest, error) {
	var req tokenRequest

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, err
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return req, err
	}
	req.GrantType = r.PostFormValue("grant_type")
	req.ClientID = r.PostFormValue("client_id")
	req.Scope = r.PostFormValue("scope")

	// An empty body from a bare `curl -X POST` still reads as
	// client_credentials: the sandbox has exactly one grant.
	if req.GrantType == "" && len(r.PostForm) == 0 {
		req.GrantType = "client_credentials"
	}
	return req, nil
}
