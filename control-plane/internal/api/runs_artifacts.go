package api

import (
	"context"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type listArtifactsResponse struct {
	Artifacts  []artifactResponse `json:"artifacts"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	Total      int                `json:"total"`
	TotalPages int                `json:"total_pages"`
}

type artifactResponse struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	Type           string         `json:"type"`
	Category       string         `json:"category,omitempty"`
	URI            string         `json:"uri"`
	ContentType    string         `json:"content_type"`
	SizeBytes      int64          `json:"size_bytes"`
	Checksum       string         `json:"checksum,omitempty"`
	Labels         []string       `json:"labels,omitempty"`
	RetentionClass string         `json:"retention_class,omitempty"`
	CreatedAt      string         `json:"created_at"`
	Metadata       map[string]any `json:"metadata"`
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	artifacts, err := s.store.ListArtifacts(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	queryText := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	categoryFilter := strings.TrimSpace(r.URL.Query().Get("category"))
	contentTypeFilter := strings.TrimSpace(r.URL.Query().Get("content_type"))
	labelFilter := splitCSV(r.URL.Query().Get("label"))
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 50)
	if pageSize > 200 {
		pageSize = 200
	}

	filtered := make([]store.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if categoryFilter != "" && !strings.EqualFold(strings.TrimSpace(artifact.Category), categoryFilter) {
			continue
		}
		if contentTypeFilter != "" && !strings.EqualFold(strings.TrimSpace(artifact.ContentType), contentTypeFilter) {
			continue
		}
		if len(labelFilter) > 0 && !containsAnyLabel(artifact.Labels, labelFilter) {
			continue
		}
		if queryText != "" && !artifactMatchesQuery(artifact, queryText) {
			continue
		}
		filtered = append(filtered, artifact)
	}

	sort.Slice(filtered, func(i, j int) bool {
		left := parseTime(filtered[i].CreatedAt)
		right := parseTime(filtered[j].CreatedAt)
		return left.After(right)
	})

	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	paged := filtered[start:end]
	response := listArtifactsResponse{
		Artifacts:  make([]artifactResponse, 0, len(paged)),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	}
	for _, artifact := range paged {
		response.Artifacts = append(response.Artifacts, artifactResponse{
			ID:             artifact.ID,
			RunID:          artifact.RunID,
			Type:           artifact.Type,
			Category:       artifact.Category,
			URI:            artifact.URI,
			ContentType:    artifact.ContentType,
			SizeBytes:      artifact.SizeBytes,
			Checksum:       artifact.Checksum,
			Labels:         append([]string{}, artifact.Labels...),
			RetentionClass: artifact.RetentionClass,
			CreatedAt:      artifact.CreatedAt,
			Metadata:       artifact.Metadata,
		})
	}
	writeJSON(w, response)
}

func artifactMatchesQuery(artifact store.Artifact, queryText string) bool {
	if strings.Contains(strings.ToLower(artifact.URI), queryText) {
		return true
	}
	if strings.Contains(strings.ToLower(artifact.Type), queryText) {
		return true
	}
	if strings.Contains(strings.ToLower(artifact.Category), queryText) {
		return true
	}
	if strings.Contains(strings.ToLower(artifact.ContentType), queryText) {
		return true
	}
	if strings.Contains(strings.ToLower(artifact.SearchableText), queryText) {
		return true
	}
	for _, label := range artifact.Labels {
		if strings.Contains(strings.ToLower(label), queryText) {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	results := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		results = append(results, strings.ToLower(trimmed))
	}
	return results
}

func containsAnyLabel(labels []string, filters []string) bool {
	if len(labels) == 0 || len(filters) == 0 {
		return false
	}
	for _, label := range labels {
		lower := strings.ToLower(strings.TrimSpace(label))
		for _, filter := range filters {
			if lower == filter {
				return true
			}
		}
	}
	return false
}

func parsePositiveInt(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func totalPages(total int, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (s *Server) upsertArtifactsFromEvent(ctx context.Context, event store.RunEvent) error {
	artifacts := extractArtifactsFromEvent(event)
	for _, artifact := range artifacts {
		if err := s.store.UpsertArtifact(ctx, artifact); err != nil {
			return err
		}
	}
	return nil
}

func extractArtifactsFromEvent(event store.RunEvent) []store.Artifact {
	if event.Payload == nil {
		return nil
	}
	eventType := events.NormalizeType(event.Type)
	createdAt := event.Timestamp
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	results := []store.Artifact{}
	if eventType == "browser.snapshot" {
		if artifact := artifactFromPayload(event.RunID, createdAt, event.Payload); artifact != nil {
			results = append(results, *artifact)
		}
		return results
	}
	if eventType == "document.created" {
		status := readString(event.Payload, "status")
		uri := readString(event.Payload, "uri")
		if status == "completed" && uri != "" {
			artifact := store.Artifact{
				ID:          uuid.New().String(),
				RunID:       event.RunID,
				Type:        "file",
				URI:         uri,
				ContentType: contentTypeFromURI(uri),
				CreatedAt:   createdAt,
				Metadata:    map[string]any{},
			}
			if filename := readString(event.Payload, "filename"); filename != "" {
				artifact.Metadata["filename"] = filename
			}
			results = append(results, artifact)
		}
		return results
	}
	if eventType != "tool.completed" {
		return nil
	}
	artifactsRaw, ok := event.Payload["artifacts"]
	if !ok {
		return nil
	}
	items, ok := artifactsRaw.([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		payload, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if artifact := artifactFromPayload(event.RunID, createdAt, payload); artifact != nil {
			results = append(results, *artifact)
		}
	}
	return results
}

func artifactFromPayload(runID string, createdAt string, payload map[string]any) *store.Artifact {
	uri := readString(payload, "uri")
	if uri == "" {
		return nil
	}
	artifactID := readString(payload, "artifact_id", "artifactId", "id")
	if artifactID == "" {
		artifactID = uuid.New().String()
	}
	artifactType := readString(payload, "type")
	if artifactType == "" {
		artifactType = "file"
	}
	metadata := map[string]any{}
	if rawMetadata, ok := payload["metadata"]; ok {
		if parsed, ok := rawMetadata.(map[string]any); ok {
			metadata = parsed
		}
	}
	return &store.Artifact{
		ID:          artifactID,
		RunID:       runID,
		Type:        artifactType,
		URI:         uri,
		ContentType: readString(payload, "content_type", "contentType"),
		SizeBytes:   readInt64(payload, "size_bytes", "sizeBytes"),
		CreatedAt:   createdAt,
		Metadata:    metadata,
	}
}

func readString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func readInt64(payload map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			switch typed := value.(type) {
			case float64:
				return int64(typed)
			case int64:
				return typed
			case int:
				return int64(typed)
			}
		}
	}
	return 0
}

func contentTypeFromURI(uri string) string {
	ext := strings.ToLower(filepath.Ext(uri))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".csv":
		return "text/csv"
	default:
		return ""
	}
}
