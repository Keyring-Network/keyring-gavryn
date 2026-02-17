package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type memorySettingsRequest struct {
	Enabled bool `json:"enabled"`
}

type memorySettingsResponse struct {
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) getMemorySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetMemorySettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := memorySettingsResponse{Enabled: false}
	if settings != nil {
		response.Enabled = settings.Enabled
		response.CreatedAt = settings.CreatedAt
		response.UpdatedAt = settings.UpdatedAt
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) updateMemorySettings(w http.ResponseWriter, r *http.Request) {
	var req memorySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	current, err := s.store.GetMemorySettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := now
	if current != nil && current.CreatedAt != "" {
		createdAt = current.CreatedAt
	}
	settings := store.MemorySettings{
		Enabled:   req.Enabled,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := s.store.UpsertMemorySettings(r.Context(), settings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	get := memorySettingsResponse{
		Enabled:   settings.Enabled,
		CreatedAt: settings.CreatedAt,
		UpdatedAt: settings.UpdatedAt,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(get)
}
