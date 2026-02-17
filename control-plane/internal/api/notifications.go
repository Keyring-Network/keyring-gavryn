package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type discordWebhookPayload struct {
	Content string                `json:"content,omitempty"`
	Embeds  []discordWebhookEmbed `json:"embeds,omitempty"`
}

type discordWebhookEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

func (s *Server) notifyDiscordAutomationCompletion(schedule store.Automation, entry store.AutomationInboxEntry) error {
	webhookURL := strings.TrimSpace(s.cfg.DiscordWebhookURL)
	if webhookURL == "" {
		return nil
	}

	status := normalizeAutomationStatus(entry.Status)
	title := fmt.Sprintf("Gavryn automation %s", status)
	if strings.TrimSpace(schedule.Name) != "" {
		title = fmt.Sprintf("Gavryn automation %s: %s", status, strings.TrimSpace(schedule.Name))
	}

	description := strings.TrimSpace(entry.FinalResponse)
	if description == "" {
		description = strings.TrimSpace(entry.Error)
	}
	if description == "" {
		description = "Automation run completed."
	}
	description = truncateForDiscord(description, 900)

	fields := []discordEmbedField{
		{Name: "Status", Value: status, Inline: true},
		{Name: "Trigger", Value: fallbackString(entry.Trigger, "schedule"), Inline: true},
	}
	if runID := strings.TrimSpace(entry.RunID); runID != "" {
		fields = append(fields, discordEmbedField{Name: "Run ID", Value: runID, Inline: false})
	}
	if reason := strings.TrimSpace(entry.CompletionReason); reason != "" {
		fields = append(fields, discordEmbedField{Name: "Completion Reason", Value: truncateForDiscord(reason, 240), Inline: false})
	}

	payload := discordWebhookPayload{
		Embeds: []discordWebhookEmbed{
			{
				Title:       title,
				Description: description,
				Color:       discordStatusColor(status),
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
				Fields:      fields,
			},
		},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord webhook rejected request: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func normalizeAutomationStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed", "partial", "failed", "cancelled":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return "completed"
	}
}

func discordStatusColor(status string) int {
	switch normalizeAutomationStatus(status) {
	case "failed":
		return 15158332
	case "cancelled":
		return 10181046
	case "partial":
		return 16776960
	default:
		return 5763719
	}
}

func truncateForDiscord(value string, limit int) string {
	text := strings.TrimSpace(value)
	if text == "" || limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return strings.TrimSpace(text[:limit-3]) + "..."
}

func fallbackString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
