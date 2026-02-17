package api

import (
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

func TestGetMemorySettings(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/memory")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload memorySettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.False(t, payload.Enabled)
		storeMock.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/memory")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("configured", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(&store.MemorySettings{Enabled: true, CreatedAt: "before", UpdatedAt: "now"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/memory")
		require.NoError(t, err)
		defer resp.Body.Close()

		var payload memorySettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.True(t, payload.Enabled)
		require.Equal(t, "before", payload.CreatedAt)
		storeMock.AssertExpectations(t)
	})
}

func TestUpdateMemorySettings(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/memory", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/memory", "application/json", strings.NewReader(`{"enabled":true}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("upsert error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		storeMock.On("UpsertMemorySettings", mock.Anything, mock.AnythingOfType("store.MemorySettings")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/memory", "application/json", strings.NewReader(`{"enabled":true}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetMemorySettings", mock.Anything).Return(&store.MemorySettings{Enabled: true, CreatedAt: "before"}, nil).Once()
		storeMock.On("UpsertMemorySettings", mock.Anything, mock.AnythingOfType("store.MemorySettings")).Return(nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/memory", "application/json", strings.NewReader(`{"enabled":false}`))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload memorySettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.False(t, payload.Enabled)
		storeMock.AssertExpectations(t)
	})
}
