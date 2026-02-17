package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func TestListContextNodes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListContextNodes", mock.Anything).Return([]store.ContextNode{{ID: "1", Name: "root", NodeType: "folder"}}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload map[string][]contextNodeResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Len(t, payload["nodes"], 1)
		storeMock.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListContextNodes", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestGetContextFile(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetContextFile", mock.Anything, "missing").Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context/files/missing")
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetContextFile", mock.Anything, "node-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context/files/node-1")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("wrong type", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetContextFile", mock.Anything, "node-1").Return(&store.ContextNode{ID: "node-1", NodeType: "folder"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context/files/node-1")
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetContextFile", mock.Anything, "node-1").Return(&store.ContextNode{ID: "node-1", NodeType: "file", Name: "file.txt", Content: []byte("hi")}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/context/files/node-1")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload contextFileResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("hi")), payload.ContentBase)
		storeMock.AssertExpectations(t)
	})
}

func TestCreateContextFolder(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/folders", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid name", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/folders", "application/json", strings.NewReader(`{"name":"bad/name"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateContextFolder", mock.Anything, mock.AnythingOfType("store.ContextNode")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/folders", "application/json", strings.NewReader(`{"name":"docs"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateContextFolder", mock.Anything, mock.AnythingOfType("store.ContextNode")).Return(nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/folders", "application/json", strings.NewReader(`{"name":"docs"}`))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestCreateContextFile(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/files", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid name", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/files", "application/json", strings.NewReader(`{"name":"bad/name"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid base64", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/context/files", "application/json", strings.NewReader(`{"name":"file.txt","content_base64":"@@"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateContextFile", mock.Anything, mock.AnythingOfType("store.ContextNode")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"file.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`
		resp, err := http.Post(server.URL+"/context/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateContextFile", mock.Anything, mock.AnythingOfType("store.ContextNode")).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(&store.MemorySettings{Enabled: true}, nil).Once()
		storeMock.On("UpsertMemoryEntry", mock.Anything, mock.MatchedBy(func(entry store.MemoryEntry) bool {
			return entry.Content != "" && entry.Metadata["source"] == "context_file"
		})).Return(true, nil).Once()
		cfg := config.Config{MemoryMinContentChars: 1, MemoryChunkChars: 50, MemoryChunkOverlap: 10, MemoryMaxChunks: 2}
		server := newTestServer(t, storeMock, &MockBroker{}, nil, cfg)
		defer server.Close()

		payload := `{"name":"file.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hello context memory")) + `"}`
		resp, err := http.Post(server.URL+"/context/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestDeleteContextNode(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("DeleteContextNode", mock.Anything, "node-1").Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/context/node-1", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("DeleteContextNode", mock.Anything, "node-1").Return(nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/context/node-1", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestValidateContextName(t *testing.T) {
	require.Error(t, validateContextName(""))
	require.Error(t, validateContextName("bad/name"))
	require.NoError(t, validateContextName("ok"))
}
