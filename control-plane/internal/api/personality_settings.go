package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/personality"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type personalitySettingsRequest struct {
	Content string `json:"content"`
}

type personalitySettingsResponse struct {
	Content   string `json:"content"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (s *Server) getPersonalitySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetPersonalitySettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := personalitySettingsResponse{}
	if settings != nil && strings.TrimSpace(settings.Content) != "" {
		response.Content = settings.Content
		response.Source = "stored"
		response.CreatedAt = settings.CreatedAt
		response.UpdatedAt = settings.UpdatedAt
	} else if content, err := personality.ReadFromDisk(); err == nil && strings.TrimSpace(content) != "" {
		response.Content = content
		response.Source = "file"
	} else {
		response.Content = personality.Default
		response.Source = "default"
	}
	if response.Source == "" {
		response.Source = "default"
		response.Content = personality.Default
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) updatePersonalitySettings(w http.ResponseWriter, r *http.Request) {
	var req personalitySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	current, err := s.store.GetPersonalitySettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := now
	if current != nil && current.CreatedAt != "" {
		createdAt = current.CreatedAt
	}
	settings := store.PersonalitySettings{
		Content:   req.Content,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := s.store.UpsertPersonalitySettings(r.Context(), settings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := personalitySettingsResponse{
		Content:   settings.Content,
		Source:    "stored",
		CreatedAt: settings.CreatedAt,
		UpdatedAt: settings.UpdatedAt,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
