package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type toolRunnerResponse struct {
	Status    string           `json:"status"`
	Output    map[string]any   `json:"output"`
	Artifacts []map[string]any `json:"artifacts"`
	Error     string           `json:"error,omitempty"`
}

type writeWorkspaceRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type execWorkspaceRequest struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Cwd       string            `json:"cwd"`
	TimeoutMs int               `json:"timeout_ms"`
	Env       map[string]string `json:"env"`
}

type processStartRequest struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
}

type workspaceFileNode struct {
	Name     string              `json:"name"`
	Path     string              `json:"path"`
	Type     string              `json:"type"`
	Children []workspaceFileNode `json:"children,omitempty"`
}

type workspaceListEntry struct {
	Name string
	Path string
	Type string
}

type workspaceTreeState struct {
	nodes int
}

const (
	maxWorkspaceTreeDepth = 6
	maxWorkspaceTreeNodes = 2000
)

func (s *Server) listWorkspace(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	pathValue := r.URL.Query().Get("path")
	if pathValue == "" {
		pathValue = "."
	}
	result, err := s.executeToolRunner(r.Context(), runID, "editor.list", map[string]any{"path": pathValue}, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) listWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	root := strings.TrimSpace(r.URL.Query().Get("path"))
	if root == "" {
		root = "."
	}
	state := &workspaceTreeState{}
	files, err := s.buildWorkspaceTree(r.Context(), runID, root, 0, state)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"files": files})
}

func (s *Server) readWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	pathValue := r.URL.Query().Get("path")
	if pathValue == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	encoding := r.URL.Query().Get("encoding")
	input := map[string]any{"path": pathValue}
	if encoding != "" {
		input["encoding"] = encoding
	}
	result, err := s.executeToolRunner(r.Context(), runID, "editor.read", input, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) writeWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	var req writeWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	input := map[string]any{"path": req.Path, "content": req.Content}
	if req.Encoding != "" {
		input["encoding"] = req.Encoding
	}
	result, err := s.executeToolRunner(r.Context(), runID, "editor.write", input, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	pathValue := r.URL.Query().Get("path")
	if pathValue == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	recursive := false
	if value := r.URL.Query().Get("recursive"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			http.Error(w, "recursive must be boolean", http.StatusBadRequest)
			return
		}
		recursive = parsed
	}
	input := map[string]any{"path": pathValue, "recursive": recursive}
	result, err := s.executeToolRunner(r.Context(), runID, "editor.delete", input, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) statWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	pathValue := r.URL.Query().Get("path")
	if pathValue == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	result, err := s.executeToolRunner(r.Context(), runID, "editor.stat", map[string]any{"path": pathValue}, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) execWorkspaceProcess(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	var req execWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		http.Error(w, "command required", http.StatusBadRequest)
		return
	}
	command := strings.TrimSpace(req.Command)
	args := req.Args
	if len(args) == 0 && strings.Contains(command, " ") {
		fields := strings.Fields(command)
		if len(fields) > 0 {
			command = fields[0]
			if len(fields) > 1 {
				args = fields[1:]
			}
		}
	}
	input := map[string]any{"command": command, "args": args}
	if req.Cwd != "" {
		input["cwd"] = req.Cwd
	}
	if req.Env != nil {
		input["env"] = req.Env
	}
	result, err := s.executeToolRunner(r.Context(), runID, "process.exec", input, req.TimeoutMs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) listWorkspaceProcesses(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	result, err := s.executeToolRunner(r.Context(), runID, "process.list", map[string]any{}, 0)
	if err != nil {
		processes, storeErr := s.store.ListRunProcesses(r.Context(), runID)
		if storeErr != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		serialized := make([]map[string]any, 0, len(processes))
		for _, process := range processes {
			serialized = append(serialized, runProcessToOutput(process, false, 0))
		}
		writeJSON(w, &toolRunnerResponse{
			Status: "completed",
			Output: map[string]any{"processes": serialized},
		})
		return
	}
	writeJSON(w, result)
}

func (s *Server) startWorkspaceProcess(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	var req processStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		http.Error(w, "command required", http.StatusBadRequest)
		return
	}
	input := map[string]any{
		"command": req.Command,
		"args":    req.Args,
	}
	if strings.TrimSpace(req.Cwd) != "" {
		input["cwd"] = req.Cwd
	}
	if req.Env != nil {
		input["env"] = req.Env
	}
	result, err := s.executeToolRunner(r.Context(), runID, "process.start", input, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) getWorkspaceProcess(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	processID := chi.URLParam(r, "pid")
	if strings.TrimSpace(processID) == "" {
		http.Error(w, "process id required", http.StatusBadRequest)
		return
	}
	result, err := s.executeToolRunner(r.Context(), runID, "process.status", map[string]any{"process_id": processID}, 0)
	if err != nil {
		process, storeErr := s.store.GetRunProcess(r.Context(), runID, processID)
		if storeErr != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if process == nil {
			http.Error(w, "process not found", http.StatusNotFound)
			return
		}
		writeJSON(w, &toolRunnerResponse{
			Status: "completed",
			Output: runProcessToOutput(*process, false, 0),
		})
		return
	}
	writeJSON(w, result)
}

func (s *Server) getWorkspaceProcessLogs(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	processID := chi.URLParam(r, "pid")
	if strings.TrimSpace(processID) == "" {
		http.Error(w, "process id required", http.StatusBadRequest)
		return
	}
	tail := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("tail")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			http.Error(w, "tail must be a positive integer", http.StatusBadRequest)
			return
		}
		tail = parsed
	}
	result, err := s.executeToolRunner(r.Context(), runID, "process.logs", map[string]any{"process_id": processID, "tail": tail}, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) stopWorkspaceProcess(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	processID := chi.URLParam(r, "pid")
	if strings.TrimSpace(processID) == "" {
		http.Error(w, "process id required", http.StatusBadRequest)
		return
	}
	result, err := s.executeToolRunner(r.Context(), runID, "process.stop", map[string]any{"process_id": processID}, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) stopAllWorkspaceProcesses(ctx context.Context, runID string) (int, error) {
	baseURL := strings.TrimRight(s.cfg.ToolRunnerURL, "/")
	if baseURL == "" {
		return 0, nil
	}
	payload, _ := json.Marshal(map[string]any{"force": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/runs/"+runID+"/processes/cleanup", bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, callErr := s.httpClient.Do(req)
		if callErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var toolResp toolRunnerResponse
				if decodeErr := json.NewDecoder(resp.Body).Decode(&toolResp); decodeErr == nil && toolResp.Output != nil {
					if stopped, ok := readOptionalInt(toolResp.Output, "stopped"); ok {
						return stopped, nil
					}
				}
			}
		}
	}

	processes := make([]store.RunProcess, 0)
	result, err := s.executeToolRunner(ctx, runID, "process.list", map[string]any{}, 0)
	if err != nil || result == nil || result.Output == nil {
		return 0, nil
	}
	if raw, ok := result.Output["processes"].([]any); ok {
		for _, item := range raw {
			processMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			processID, _ := processMap["process_id"].(string)
			status, _ := processMap["status"].(string)
			if strings.TrimSpace(processID) == "" {
				continue
			}
			processes = append(processes, store.RunProcess{
				RunID:     runID,
				ProcessID: strings.TrimSpace(processID),
				Status:    strings.TrimSpace(status),
			})
		}
	}

	stoppedCount := 0
	var firstErr error
	for _, process := range processes {
		if !isRunnableProcessState(process.Status) {
			continue
		}
		_, stopErr := s.executeToolRunner(ctx, runID, "process.stop", map[string]any{
			"process_id": process.ProcessID,
			"force":      true,
		}, 0)
		if stopErr != nil {
			if strings.Contains(strings.ToLower(stopErr.Error()), "process not found") {
				continue
			}
			if firstErr == nil {
				firstErr = stopErr
			}
			continue
		}
		stoppedCount++
	}
	return stoppedCount, firstErr
}

func isRunnableProcessState(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "running", "starting":
		return true
	default:
		return false
	}
}

func (s *Server) executeToolRunner(ctx context.Context, runID string, toolName string, input map[string]any, timeoutMs int) (*toolRunnerResponse, error) {
	baseURL := strings.TrimRight(s.cfg.ToolRunnerURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("tool runner url not configured")
	}
	invocationID := uuid.New().String()
	payload := map[string]any{
		"contract_version": "tool_contract_v2",
		"run_id":           runID,
		"invocation_id":    invocationID,
		"idempotency_key":  invocationID,
		"tool_name":        toolName,
		"input":            input,
		"policy_context": map[string]any{
			"profile": "default",
		},
	}
	if timeoutMs > 0 {
		payload["timeout_ms"] = timeoutMs
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/tools/execute", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(resp.Body)
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = fmt.Sprintf("tool runner returned status %d", resp.StatusCode)
		}
		return nil, errors.New(message)
	}
	var result toolRunnerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Server) buildWorkspaceTree(ctx context.Context, runID string, pathValue string, depth int, state *workspaceTreeState) ([]workspaceFileNode, error) {
	if depth > maxWorkspaceTreeDepth {
		return nil, nil
	}
	if state != nil && state.nodes >= maxWorkspaceTreeNodes {
		return nil, nil
	}
	input := map[string]any{"path": pathValue}
	result, err := s.executeToolRunner(ctx, runID, "editor.list", input, 0)
	if err != nil {
		return nil, err
	}
	entries, err := parseWorkspaceEntries(result.Output)
	if err != nil {
		return nil, err
	}
	files := make([]workspaceFileNode, 0, len(entries))
	for _, entry := range entries {
		if state != nil && state.nodes >= maxWorkspaceTreeNodes {
			break
		}
		node := workspaceFileNode{Name: entry.Name, Path: entry.Path, Type: entry.Type}
		if entry.Type == "directory" {
			children, err := s.buildWorkspaceTree(ctx, runID, entry.Path, depth+1, state)
			if err == nil && len(children) > 0 {
				node.Children = children
			}
		}
		files = append(files, node)
		if state != nil {
			state.nodes++
		}
	}
	return files, nil
}

func parseWorkspaceEntries(output map[string]any) ([]workspaceListEntry, error) {
	entriesValue, ok := output["entries"]
	if !ok {
		return nil, fmt.Errorf("list output missing entries")
	}
	entriesSlice, ok := entriesValue.([]any)
	if !ok {
		return nil, fmt.Errorf("list output entries invalid")
	}
	entries := make([]workspaceListEntry, 0, len(entriesSlice))
	for _, item := range entriesSlice {
		entryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entryMap["name"].(string)
		pathValue, _ := entryMap["path"].(string)
		entryType, _ := entryMap["type"].(string)
		if name == "" || pathValue == "" || entryType == "" {
			continue
		}
		entries = append(entries, workspaceListEntry{Name: name, Path: pathValue, Type: entryType})
	}
	return entries, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func runProcessToOutput(process store.RunProcess, includeLogs bool, tail int) map[string]any {
	output := map[string]any{
		"process_id":   process.ProcessID,
		"run_id":       process.RunID,
		"command":      process.Command,
		"args":         process.Args,
		"cwd":          process.Cwd,
		"status":       process.Status,
		"pid":          process.PID,
		"started_at":   process.StartedAt,
		"ended_at":     process.EndedAt,
		"signal":       process.Signal,
		"preview_urls": process.PreviewURLs,
	}
	if process.ExitCode != nil {
		output["exit_code"] = *process.ExitCode
	} else {
		output["exit_code"] = nil
	}
	if includeLogs {
		output["logs"] = []any{}
		output["logs_tail"] = tail
	}
	return output
}
