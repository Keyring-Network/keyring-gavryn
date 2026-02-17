package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/skills"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

var skillNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type skillRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Files       []skillFilePayload `json:"files"`
}

type skillFilePayload struct {
	Path        string `json:"path"`
	ContentBase string `json:"content_base64"`
	ContentType string `json:"content_type"`
}

type skillFilesUpdate struct {
	Files       []skillFilePayload `json:"files"`
	DeletePaths []string           `json:"delete_paths"`
}

type skillResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type skillFileResponse struct {
	Path        string `json:"path"`
	ContentBase string `json:"content_base64"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	skillsList, err := s.store.ListSkills(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := make([]skillResponse, 0, len(skillsList))
	for _, skill := range skillsList {
		response = append(response, skillResponse{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			CreatedAt:   skill.CreatedAt,
			UpdatedAt:   skill.UpdatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skills": response})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var req skillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateSkillName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	files := ensureSkillMarkdown(req.Files)
	fileEntries, err := decodeSkillFiles(files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	skill := store.Skill{
		ID:          uuid.New().String(),
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateSkill(r.Context(), skill); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := upsertSkillFiles(r.Context(), s.store, skill.ID, fileEntries, now); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := materializeSkillFiles(r.Context(), s.store, skill.ID, skill.Name, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(skillResponse{
		ID:          skill.ID,
		Name:        skill.Name,
		Description: skill.Description,
		CreatedAt:   skill.CreatedAt,
		UpdatedAt:   skill.UpdatedAt,
	})
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	existing, err := s.store.GetSkill(r.Context(), skillID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	var req skillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	if err := validateSkillName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated := store.Skill{
		ID:          existing.ID,
		Name:        name,
		Description: firstNonEmpty(strings.TrimSpace(req.Description), existing.Description),
		CreatedAt:   existing.CreatedAt,
		UpdatedAt:   now,
	}
	if err := s.store.UpdateSkill(r.Context(), updated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := materializeSkillFiles(r.Context(), s.store, updated.ID, updated.Name, existing.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(skillResponse{
		ID:          updated.ID,
		Name:        updated.Name,
		Description: updated.Description,
		CreatedAt:   updated.CreatedAt,
		UpdatedAt:   updated.UpdatedAt,
	})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	existing, err := s.store.GetSkill(r.Context(), skillID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.store.DeleteSkill(r.Context(), skillID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if root, err := skills.RootDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(root, existing.Name))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSkillFiles(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	files, err := s.store.ListSkillFiles(r.Context(), skillID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := make([]skillFileResponse, 0, len(files))
	for _, file := range files {
		response = append(response, skillFileResponse{
			Path:        file.Path,
			ContentBase: base64.StdEncoding.EncodeToString(file.Content),
			ContentType: file.ContentType,
			SizeBytes:   file.SizeBytes,
			UpdatedAt:   file.UpdatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": response})
}

func (s *Server) upsertSkillFiles(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	skill, err := s.store.GetSkill(r.Context(), skillID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if skill == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	var req skillFilesUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	files := ensureSkillMarkdown(req.Files)
	fileEntries, err := decodeSkillFiles(files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := upsertSkillFiles(r.Context(), s.store, skill.ID, fileEntries, now); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, path := range req.DeletePaths {
		if err := s.store.DeleteSkillFile(r.Context(), skill.ID, path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := materializeSkillFiles(r.Context(), s.store, skill.ID, skill.Name, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSkillFiles(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	skill, err := s.store.GetSkill(r.Context(), skillID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if skill == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	var req skillFilesUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	for _, path := range req.DeletePaths {
		if err := s.store.DeleteSkillFile(r.Context(), skill.ID, path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := materializeSkillFiles(r.Context(), s.store, skill.ID, skill.Name, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.Contains(name, string(os.PathSeparator)) {
		return fmt.Errorf("name must not include path separators")
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("name must use letters, numbers, dots, underscores, or hyphens")
	}
	return nil
}

func ensureSkillMarkdown(files []skillFilePayload) []skillFilePayload {
	for _, file := range files {
		if strings.EqualFold(filepath.Base(file.Path), "SKILL.md") {
			return files
		}
	}
	return append(files, skillFilePayload{Path: "SKILL.md", ContentBase: base64.StdEncoding.EncodeToString([]byte("")), ContentType: "text/markdown"})
}

func decodeSkillFiles(files []skillFilePayload) ([]store.SkillFile, error) {
	entries := make([]store.SkillFile, 0, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			return nil, fmt.Errorf("file path required")
		}
		if strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return nil, fmt.Errorf("invalid file path")
		}
		decoded, err := base64.StdEncoding.DecodeString(file.ContentBase)
		if err != nil {
			return nil, fmt.Errorf("invalid file content for %s", path)
		}
		contentType := strings.TrimSpace(file.ContentType)
		if contentType == "" && strings.EqualFold(filepath.Base(path), "SKILL.md") {
			contentType = "text/markdown"
		}
		entries = append(entries, store.SkillFile{
			ID:          uuid.New().String(),
			Path:        path,
			Content:     decoded,
			ContentType: contentType,
			SizeBytes:   int64(len(decoded)),
		})
	}
	return entries, nil
}

func upsertSkillFiles(ctx context.Context, store store.Store, skillID string, files []store.SkillFile, now string) error {
	for _, file := range files {
		file.SkillID = skillID
		file.CreatedAt = now
		file.UpdatedAt = now
		if err := store.UpsertSkillFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func materializeSkillFiles(ctx context.Context, store store.Store, skillID string, skillName string, oldName string) error {
	root, err := skills.RootDir()
	if err != nil {
		return err
	}
	files, err := store.ListSkillFiles(ctx, skillID)
	if err != nil {
		return err
	}
	if err := skills.MaterializeSkill(root, skillName, files); err != nil {
		return err
	}
	if oldName != "" && oldName != skillName {
		_ = os.RemoveAll(filepath.Join(root, oldName))
	}
	return nil
}
