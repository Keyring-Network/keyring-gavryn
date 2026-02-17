package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type contextNodeResponse struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id,omitempty"`
	Name        string `json:"name"`
	NodeType    string `json:"node_type"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type contextFileResponse struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id,omitempty"`
	Name        string `json:"name"`
	NodeType    string `json:"node_type"`
	ContentBase string `json:"content_base64"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type contextFolderRequest struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

type contextFileRequest struct {
	Name        string `json:"name"`
	ParentID    string `json:"parent_id"`
	ContentBase string `json:"content_base64"`
	ContentType string `json:"content_type"`
}

func (s *Server) listContextNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListContextNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := make([]contextNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response = append(response, contextNodeResponse{
			ID:          node.ID,
			ParentID:    node.ParentID,
			Name:        node.Name,
			NodeType:    node.NodeType,
			ContentType: node.ContentType,
			SizeBytes:   node.SizeBytes,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"nodes": response})
}

func (s *Server) createContextFolder(w http.ResponseWriter, r *http.Request) {
	var req contextFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateContextName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	node := store.ContextNode{
		ID:        uuid.New().String(),
		ParentID:  strings.TrimSpace(req.ParentID),
		Name:      name,
		NodeType:  "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateContextFolder(r.Context(), node); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contextNodeResponse{
		ID:        node.ID,
		ParentID:  node.ParentID,
		Name:      node.Name,
		NodeType:  node.NodeType,
		CreatedAt: node.CreatedAt,
		UpdatedAt: node.UpdatedAt,
	})
}

func (s *Server) uploadContextFile(w http.ResponseWriter, r *http.Request) {
	var req contextFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateContextName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content, err := base64.StdEncoding.DecodeString(req.ContentBase)
	if err != nil {
		http.Error(w, "invalid file content", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	node := store.ContextNode{
		ID:          uuid.New().String(),
		ParentID:    strings.TrimSpace(req.ParentID),
		Name:        name,
		NodeType:    "file",
		Content:     content,
		ContentType: strings.TrimSpace(req.ContentType),
		SizeBytes:   int64(len(content)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateContextFile(r.Context(), node); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.indexContextFileMemory(r.Context(), node)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contextNodeResponse{
		ID:          node.ID,
		ParentID:    node.ParentID,
		Name:        node.Name,
		NodeType:    node.NodeType,
		ContentType: node.ContentType,
		SizeBytes:   node.SizeBytes,
		CreatedAt:   node.CreatedAt,
		UpdatedAt:   node.UpdatedAt,
	})
}

func (s *Server) getContextFile(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	node, err := s.store.GetContextFile(r.Context(), nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if node.NodeType != "file" {
		http.Error(w, "node is not a file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contextFileResponse{
		ID:          node.ID,
		ParentID:    node.ParentID,
		Name:        node.Name,
		NodeType:    node.NodeType,
		ContentBase: base64.StdEncoding.EncodeToString(node.Content),
		ContentType: node.ContentType,
		SizeBytes:   node.SizeBytes,
		CreatedAt:   node.CreatedAt,
		UpdatedAt:   node.UpdatedAt,
	})
}

func (s *Server) deleteContextNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	if err := s.store.DeleteContextNode(r.Context(), nodeID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateContextName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.Contains(name, string(os.PathSeparator)) {
		return fmt.Errorf("name must not include path separators")
	}
	return nil
}
