package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/personality"
)

func TestGetPersonalitySettings_DefaultFallback(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("GetPersonalitySettings", mock.Anything).Return(nil, nil).Once()
	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	resp, err := http.Get(server.URL + "/settings/personality")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "default", payload["source"])
	require.Equal(t, personality.Default, payload["content"])
	storeMock.AssertExpectations(t)
}

func TestUpdatePersonalitySettings(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("GetPersonalitySettings", mock.Anything).Return(nil, nil).Once()
	storeMock.On("UpsertPersonalitySettings", mock.Anything, mock.AnythingOfType("store.PersonalitySettings")).Return(nil).Once()
	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	body := personalitySettingsRequest{Content: "Be Gavryn."}
	encoded, _ := json.Marshal(body)
	resp, err := http.Post(server.URL+"/settings/personality", "application/json", bytes.NewReader(encoded))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload personalitySettingsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "stored", payload.Source)
	require.Equal(t, "Be Gavryn.", payload.Content)
	require.NotEmpty(t, payload.CreatedAt)
	require.NotEmpty(t, payload.UpdatedAt)
	_, err = time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	require.NoError(t, err)
	storeMock.AssertExpectations(t)
}
