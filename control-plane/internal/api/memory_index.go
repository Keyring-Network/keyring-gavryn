package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func (s *Server) indexMessageMemory(ctx context.Context, msg store.Message) {
	if !s.memoryEnabled(ctx) {
		return
	}
	if msg.Role != "user" && msg.Role != "assistant" {
		return
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return
	}
	if len([]rune(content)) < s.cfg.MemoryMinContentChars {
		return
	}
	chunks := chunkContent(content, s.cfg.MemoryChunkChars, s.cfg.MemoryChunkOverlap, s.cfg.MemoryMaxChunks)
	if len(chunks) == 0 {
		return
	}
	for index, chunk := range chunks {
		normalized := normalizeContent(chunk)
		fingerprint := memoryFingerprint("chat_message", msg.Role, normalized, strconv.Itoa(index))
		metadata := map[string]any{
			"source":      "chat_message",
			"run_id":      msg.RunID,
			"message_id":  msg.ID,
			"role":        msg.Role,
			"sequence":    msg.Sequence,
			"created_at":  msg.CreatedAt,
			"chunk_index": index,
			"chunk_total": len(chunks),
			"fingerprint": fingerprint,
		}
		entry := store.MemoryEntry{
			ID:        uuid.New().String(),
			Content:   chunk,
			Metadata:  metadata,
			CreatedAt: msg.CreatedAt,
			UpdatedAt: msg.CreatedAt,
			Embedding: readFloat32Slice(msg.Metadata, "embedding"),
		}
		if entry.CreatedAt == "" {
			entry.CreatedAt = nowUTC()
			entry.UpdatedAt = entry.CreatedAt
		}
		if _, err := s.store.UpsertMemoryEntry(ctx, entry); err != nil {
			s.recordMemoryIndexError(ctx, msg.RunID, "chat_message", err)
			return
		}
	}
}

func (s *Server) indexContextFileMemory(ctx context.Context, node store.ContextNode) {
	if !s.memoryEnabled(ctx) {
		return
	}
	if node.NodeType != "file" {
		return
	}
	if s.cfg.MemoryMaxContentBytes > 0 && int64(len(node.Content)) > int64(s.cfg.MemoryMaxContentBytes) {
		return
	}
	if !utf8.Valid(node.Content) {
		return
	}
	if !isTextContentType(node.ContentType) {
		return
	}
	content := strings.TrimSpace(string(node.Content))
	if content == "" {
		return
	}
	if len([]rune(content)) < s.cfg.MemoryMinContentChars {
		return
	}
	path := s.contextPathForNode(ctx, node)
	chunks := chunkContent(content, s.cfg.MemoryChunkChars, s.cfg.MemoryChunkOverlap, s.cfg.MemoryMaxChunks)
	if len(chunks) == 0 {
		return
	}
	for index, chunk := range chunks {
		normalized := normalizeContent(chunk)
		fingerprint := memoryFingerprint("context_file", path, normalized, strconv.Itoa(index))
		metadata := map[string]any{
			"source":       "context_file",
			"context_id":   node.ID,
			"context_path": path,
			"content_type": node.ContentType,
			"size_bytes":   node.SizeBytes,
			"created_at":   node.CreatedAt,
			"updated_at":   node.UpdatedAt,
			"chunk_index":  index,
			"chunk_total":  len(chunks),
			"fingerprint":  fingerprint,
		}
		entry := store.MemoryEntry{
			ID:        uuid.New().String(),
			Content:   chunk,
			Metadata:  metadata,
			CreatedAt: node.CreatedAt,
			UpdatedAt: node.UpdatedAt,
		}
		if entry.CreatedAt == "" {
			entry.CreatedAt = nowUTC()
			entry.UpdatedAt = entry.CreatedAt
		}
		if _, err := s.store.UpsertMemoryEntry(ctx, entry); err != nil {
			return
		}
	}
}

func (s *Server) memoryEnabled(ctx context.Context) bool {
	settings, err := s.store.GetMemorySettings(ctx)
	if err != nil || settings == nil {
		return false
	}
	return settings.Enabled
}

func (s *Server) recordMemoryIndexError(ctx context.Context, runID string, source string, err error) {
	if runID == "" || s.broker == nil {
		return
	}
	seq, _ := s.store.NextSeq(ctx, runID)
	event := store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      "memory_index_failed",
		Timestamp: nowUTC(),
		Source:    "control_plane",
		TraceID:   uuid.New().String(),
		Payload:   map[string]any{"source": source, "error": err.Error()},
	}
	_ = s.store.AppendEvent(ctx, event)
	s.broker.Publish(toEvent(event))
}

func (s *Server) contextPathForNode(ctx context.Context, node store.ContextNode) string {
	parts := []string{node.Name}
	if node.ParentID != "" {
		nodes, err := s.store.ListContextNodes(ctx)
		if err == nil {
			lookup := map[string]store.ContextNode{}
			for _, item := range nodes {
				lookup[item.ID] = item
			}
			current := node.ParentID
			visited := map[string]bool{}
			for current != "" {
				if visited[current] {
					break
				}
				visited[current] = true
				parent, ok := lookup[current]
				if !ok {
					break
				}
				parts = append([]string{parent.Name}, parts...)
				current = parent.ParentID
			}
		}
	}
	return "/context/" + strings.Join(parts, "/")
}

func chunkContent(content string, maxChars int, overlap int, maxChunks int) []string {
	if maxChars <= 0 {
		return []string{content}
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return []string{content}
	}
	step := maxChars - overlap
	if step <= 0 {
		step = maxChars
	}
	chunks := []string{}
	for start := 0; start < len(runes); start += step {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if maxChunks > 0 && len(chunks) >= maxChunks {
			break
		}
	}
	return chunks
}

func normalizeContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func memoryFingerprint(parts ...string) string {
	hasher := sha256.New()
	for index, part := range parts {
		if index > 0 {
			_, _ = hasher.Write([]byte("|"))
		}
		_, _ = hasher.Write([]byte(part))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func readFloat32Slice(metadata map[string]any, key string) []float32 {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	slice, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]float32, 0, len(slice))
	for _, item := range slice {
		switch v := item.(type) {
		case float32:
			results = append(results, v)
		case float64:
			results = append(results, float32(v))
		case int:
			results = append(results, float32(v))
		case int64:
			results = append(results, float32(v))
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				results = append(results, float32(parsed))
			}
		}
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

func isTextContentType(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "text/") {
		return true
	}
	switch trimmed {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml", "application/toml", "application/markdown":
		return true
	default:
		return false
	}
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
