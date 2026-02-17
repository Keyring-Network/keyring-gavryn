package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/memory"
)

func TestAutomationsCRUD(t *testing.T) {
	server := newTestServer(t, memory.New(), &MockBroker{}, nil, config.Config{})
	defer server.Close()

	createPayload := map[string]any{
		"name":     "Daily RWA Brief",
		"prompt":   "Browse web and summarize the top RWA stories.",
		"model":    "gpt-5.2-codex",
		"days":     []string{"mon", "wed", "fri"},
		"time":     "09:30",
		"timezone": "UTC",
		"enabled":  true,
	}
	createBody, err := json.Marshal(createPayload)
	require.NoError(t, err)

	createResp, err := http.Post(server.URL+"/automations", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var created automationSchedule
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	require.NotEmpty(t, created.ID)
	require.Equal(t, "Daily RWA Brief", created.Name)
	require.Equal(t, "09:30", created.TimeOfDay)
	require.Equal(t, "UTC", created.Timezone)
	require.True(t, created.Enabled)

	listResp, err := http.Get(server.URL + "/automations")
	require.NoError(t, err)
	defer listResp.Body.Close()
	require.Equal(t, http.StatusOK, listResp.StatusCode)

	var listPayload automationsListResponse
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&listPayload))
	require.Len(t, listPayload.Automations, 1)
	require.Equal(t, 0, listPayload.UnreadCount)
	require.Equal(t, created.ID, listPayload.Automations[0].ID)
	require.NotEmpty(t, listPayload.Automations[0].NextRunAt)

	updatePayload := map[string]any{
		"name":    "Daily RWA + DeFi Brief",
		"enabled": false,
	}
	updateBody, err := json.Marshal(updatePayload)
	require.NoError(t, err)
	updateReq, err := http.NewRequest(http.MethodPut, server.URL+"/automations/"+created.ID, bytes.NewReader(updateBody))
	require.NoError(t, err)
	updateReq.Header.Set("Content-Type", "application/json")

	updateResp, err := http.DefaultClient.Do(updateReq)
	require.NoError(t, err)
	defer updateResp.Body.Close()
	require.Equal(t, http.StatusOK, updateResp.StatusCode)

	var updated automationSchedule
	require.NoError(t, json.NewDecoder(updateResp.Body).Decode(&updated))
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "Daily RWA + DeFi Brief", updated.Name)
	require.False(t, updated.Enabled)
	require.Empty(t, updated.NextRunAt)

	inboxResp, err := http.Get(server.URL + "/automations/" + created.ID + "/inbox")
	require.NoError(t, err)
	defer inboxResp.Body.Close()
	require.Equal(t, http.StatusOK, inboxResp.StatusCode)

	var inboxPayload automationDetailResponse
	require.NoError(t, json.NewDecoder(inboxResp.Body).Decode(&inboxPayload))
	require.Equal(t, created.ID, inboxPayload.Automation.ID)
	require.Equal(t, "Daily RWA + DeFi Brief", inboxPayload.Automation.Name)
	require.Len(t, inboxPayload.Inbox, 0)

	deleteReq, err := http.NewRequest(http.MethodDelete, server.URL+"/automations/"+created.ID, nil)
	require.NoError(t, err)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	require.NoError(t, err)
	defer deleteResp.Body.Close()
	require.Equal(t, http.StatusOK, deleteResp.StatusCode)

	listAfterDeleteResp, err := http.Get(server.URL + "/automations")
	require.NoError(t, err)
	defer listAfterDeleteResp.Body.Close()
	require.Equal(t, http.StatusOK, listAfterDeleteResp.StatusCode)

	var listAfterDelete automationsListResponse
	require.NoError(t, json.NewDecoder(listAfterDeleteResp.Body).Decode(&listAfterDelete))
	require.Len(t, listAfterDelete.Automations, 0)
	require.Equal(t, 0, listAfterDelete.UnreadCount)
}

func TestAutomationsCreateValidation(t *testing.T) {
	server := newTestServer(t, memory.New(), &MockBroker{}, nil, config.Config{})
	defer server.Close()

	resp, err := http.Post(
		server.URL+"/automations",
		"application/json",
		bytes.NewReader([]byte(`{"prompt":"missing name"}`)),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, err = http.Post(
		server.URL+"/automations",
		"application/json",
		bytes.NewReader([]byte(`{"name":"Missing prompt"}`)),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, err = http.Post(
		server.URL+"/automations",
		"application/json",
		bytes.NewReader([]byte(`{"name":"Invalid time","prompt":"x","time":"930"}`)),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestComputeNextRun(t *testing.T) {
	from := time.Date(2026, time.February, 9, 8, 0, 0, 0, time.UTC) // Monday
	next, err := computeNextRun([]string{"mon", "fri"}, "09:00", "UTC", from)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.February, 9, 9, 0, 0, 0, time.UTC), next)

	afterWindow := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC) // Monday after 09:00
	next, err = computeNextRun([]string{"mon", "fri"}, "09:00", "UTC", afterWindow)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.February, 13, 9, 0, 0, 0, time.UTC), next)
}
