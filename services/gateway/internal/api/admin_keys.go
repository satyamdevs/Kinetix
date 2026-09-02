package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"orca/gateway/internal/keys"
)

// handleListKeys returns every key record, newest first. The raw secrets are
// never stored or returned; only the prefix is shown.
func handleListKeys(k keys.Keeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := k.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				"listing keys failed: "+err.Error(), "api_error", "query_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": all})
	}
}

type createKeyRequest struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Requests int64  `json:"requests_per_minute"`
	Tokens   int64  `json:"tokens_per_month"`
	TTLHours int64  `json:"ttl_hours"`
}

// handleCreateKey provisions a new key and returns the raw secret exactly
// once. The caller is responsible for persisting it.
func handleCreateKey(k keys.Keeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest,
				"invalid request body: "+err.Error(), "invalid_request_error", "invalid_body")
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeError(w, http.StatusBadRequest,
				"name is required", "invalid_request_error", "missing_name")
			return
		}

		raw, key, err := keys.Generate(req.Name, req.Owner,
			keys.Limit{
				RequestsPerMinute: req.Requests,
				TokensPerMonth:    req.Tokens,
			},
			time.Duration(req.TTLHours)*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				"generating key failed: "+err.Error(), "api_error", "generate_failed")
			return
		}
		if err := k.Create(r.Context(), key); err != nil {
			writeError(w, http.StatusInternalServerError,
				"storing key failed: "+err.Error(), "api_error", "store_failed")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"key": key, "secret": raw})
	}
}

// handleSetKeyEnabled enables or disables a key by id.
func handleSetKeyEnabled(k keys.Keeper, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		if err := k.SetEnabled(r.Context(), id, enabled); err != nil {
			writeKeyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
	}
}

// handleDeleteKey revokes a key permanently.
func handleDeleteKey(k keys.Keeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		if err := k.Delete(r.Context(), id); err != nil {
			writeKeyError(w, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	}
}

// handleGetKey returns a single key record by id.
func handleGetKey(k keys.Keeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		all, err := k.List(r.Context())
		if err != nil {
			writeKeyError(w, err)
			return
		}
		for _, key := range all {
			if key.ID == id {
				writeJSON(w, http.StatusOK, key)
				return
			}
		}
		writeKeyError(w, keys.ErrNotFound)
	}
}

func writeKeyError(w http.ResponseWriter, err error) {
	switch err {
	case keys.ErrNotFound:
		writeError(w, http.StatusNotFound, "api key not found", "invalid_request_error", "key_not_found")
	default:
		writeError(w, http.StatusInternalServerError, err.Error(), "api_error", "operation_failed")
	}
}

// pathID extracts a single path segment id from the request scoped by chi.
func pathID(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "id"))
}
