package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/memory"
)

func TestNotifyDiscordAutomationCompletionPostsEmbed(t *testing.T) {
	var (
		receivedMethod string
		receivedBody   []byte
	)
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	s := NewServer(memory.New(), &MockBroker{}, nil, config.Config{
		DiscordWebhookURL: discord.URL,
	})

	hugeSummary := strings.Repeat("A", 1400)
	err := s.notifyDiscordAutomationCompletion(
		store.Automation{ID: "a1", Name: "Daily Brief"},
		store.AutomationInboxEntry{
			ID:               "i1",
			RunID:            "r1",
			Status:           "failed",
			Trigger:          "manual",
			CompletionReason: "tool_execution_failed",
			FinalResponse:    hugeSummary,
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, receivedMethod)

	payload := discordWebhookPayload{}
	require.NoError(t, json.Unmarshal(receivedBody, &payload))
	require.Len(t, payload.Embeds, 1)
	require.Equal(t, "Gavryn automation failed: Daily Brief", payload.Embeds[0].Title)
	require.LessOrEqual(t, len(payload.Embeds[0].Description), 900)

	fields := payload.Embeds[0].Fields
	require.GreaterOrEqual(t, len(fields), 3)
}

func TestNotifyDiscordAutomationCompletionSkipsWhenNotConfigured(t *testing.T) {
	s := NewServer(memory.New(), &MockBroker{}, nil, config.Config{})
	err := s.notifyDiscordAutomationCompletion(
		store.Automation{ID: "a1", Name: "No Webhook"},
		store.AutomationInboxEntry{ID: "i1", Status: "completed"},
	)
	require.NoError(t, err)
}
