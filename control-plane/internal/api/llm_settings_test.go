package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/secrets"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func TestGetLLMSettings(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		cfg := config.Config{LLMMode: "remote", LLMProvider: "openai", LLMModel: "gpt-4"}
		server := newTestServer(t, storeMock, &MockBroker{}, nil, cfg)
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		defer resp.Body.Close()

		var payload llmSettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.False(t, payload.Configured)
		require.Equal(t, "openai", payload.Provider)
		storeMock.AssertExpectations(t)
	})

	t.Run("configured with hint", func(t *testing.T) {
		storeMock := &MockStore{}
		key := "0123456789abcdef0123456789abcdef"
		cipher, err := secrets.Encrypt([]byte(key), "sk-test-1234")
		require.NoError(t, err)
		settings := &store.LLMSettings{Provider: "openai", Model: "gpt-4", APIKeyEnc: cipher}
		storeMock.On("GetLLMSettings", mock.Anything).Return(settings, nil).Once()
		cfg := config.Config{LLMSecretsKey: key}
		server := newTestServer(t, storeMock, &MockBroker{}, nil, cfg)
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		defer resp.Body.Close()

		var payload llmSettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.True(t, payload.Configured)
		require.True(t, payload.HasAPIKey)
		require.Equal(t, "1234", payload.APIKeyHint)
		storeMock.AssertExpectations(t)
	})

	t.Run("configured with bad key", func(t *testing.T) {
		storeMock := &MockStore{}
		cipher, err := secrets.Encrypt([]byte("0123456789abcdef0123456789abcdef"), "sk-test-9999")
		require.NoError(t, err)
		settings := &store.LLMSettings{Provider: "openai", Model: "gpt-4", APIKeyEnc: cipher}
		storeMock.On("GetLLMSettings", mock.Anything).Return(settings, nil).Once()
		cfg := config.Config{LLMSecretsKey: "bad"}
		server := newTestServer(t, storeMock, &MockBroker{}, nil, cfg)
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		defer resp.Body.Close()

		var payload llmSettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.True(t, payload.HasAPIKey)
		require.Empty(t, payload.APIKeyHint)
		storeMock.AssertExpectations(t)
	})

	t.Run("configured without secrets", func(t *testing.T) {
		storeMock := &MockStore{}
		settings := &store.LLMSettings{Provider: "openai", Model: "gpt-4", APIKeyEnc: "encrypted"}
		storeMock.On("GetLLMSettings", mock.Anything).Return(settings, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		defer resp.Body.Close()

		var payload llmSettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.True(t, payload.HasAPIKey)
		require.Empty(t, payload.APIKeyHint)
		storeMock.AssertExpectations(t)
	})

	t.Run("decrypt error", func(t *testing.T) {
		storeMock := &MockStore{}
		settings := &store.LLMSettings{Provider: "openai", Model: "gpt-4", APIKeyEnc: "bad"}
		storeMock.On("GetLLMSettings", mock.Anything).Return(settings, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMSecretsKey: "0123456789abcdef0123456789abcdef"})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/settings/llm")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestUpdateLLMSettings(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid secrets key", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMSecretsKey: "bad"})
		defer server.Close()

		payload := `{"provider":"openai","api_key":"key"}`
		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing api key", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		payload := `{"provider":"openai"}`
		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		key := "0123456789abcdef0123456789abcdef"
		cfg := config.Config{LLMSecretsKey: key, LLMMode: "remote", LLMProvider: "openai"}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		var saved store.LLMSettings
		storeMock.On("UpsertLLMSettings", mock.Anything, mock.AnythingOfType("store.LLMSettings")).Run(func(args mock.Arguments) {
			saved = args.Get(1).(store.LLMSettings)
		}).Return(nil).Once()
		storeMock.On("GetLLMSettings", mock.Anything).Return(&saved, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, cfg)
		defer server.Close()

		payload := `{"provider":"openai","model":"gpt-4","api_key":"sk-test"}`
		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var response llmSettingsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&response))
		require.True(t, response.HasAPIKey)
		storeMock.AssertExpectations(t)
	})

	t.Run("existing settings", func(t *testing.T) {
		storeMock := &MockStore{}
		existing := &store.LLMSettings{Mode: "remote", Provider: "openai", Model: "gpt-4", APIKeyEnc: "encrypted", CreatedAt: "before"}
		storeMock.On("GetLLMSettings", mock.Anything).Return(existing, nil).Once()
		storeMock.On("UpsertLLMSettings", mock.Anything, mock.AnythingOfType("store.LLMSettings")).Return(nil).Once()
		storeMock.On("GetLLMSettings", mock.Anything).Return(existing, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMMode: "local"})
		defer server.Close()

		payload := `{"mode":"local"}`
		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"provider":"openai"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("existing key", func(t *testing.T) {
		storeMock := &MockStore{}
		existing := &store.LLMSettings{Mode: "remote", Provider: "openai", Model: "gpt-4", APIKeyEnc: "encrypted", CreatedAt: "before"}
		storeMock.On("GetLLMSettings", mock.Anything).Return(existing, nil).Once()
		storeMock.On("UpsertLLMSettings", mock.Anything, mock.AnythingOfType("store.LLMSettings")).Return(nil).Once()
		storeMock.On("GetLLMSettings", mock.Anything).Return(existing, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"model":"gpt-4"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("local mode without key", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		storeMock.On("UpsertLLMSettings", mock.Anything, mock.AnythingOfType("store.LLMSettings")).Return(nil).Once()
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Mode: "local", Provider: "openai"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"mode":"local","provider":"openai"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("settings without key", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"provider":"openai"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("encrypt error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMSecretsKey: "0123456789abcdef0123456789abcdef"})
		defer server.Close()

		original := encryptLLMSecret
		encryptLLMSecret = func(key []byte, plaintext string) (string, error) {
			return "", errors.New("encrypt")
		}
		t.Cleanup(func() {
			encryptLLMSecret = original
		})

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"provider":"openai","api_key":"sk-test"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("upsert error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		storeMock.On("UpsertLLMSettings", mock.Anything, mock.AnythingOfType("store.LLMSettings")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMSecretsKey: "0123456789abcdef0123456789abcdef"})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm", "application/json", strings.NewReader(`{"provider":"openai","api_key":"sk-test"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestTestLLMSettings(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		providerMock := &MockProvider{}
		providerMock.On("Generate", mock.Anything, mock.Anything).Return("pong", nil).Once()

		original := newLLMProvider
		newLLMProvider = func(cfg llm.Config) (llm.Provider, error) {
			return providerMock, nil
		}
		t.Cleanup(func() {
			newLLMProvider = original
		})

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		payload := `{"provider":"openai","model":"gpt-4","api_key":"sk-test"}`
		resp, err := http.Post(server.URL+"/settings/llm/test", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		providerMock.AssertExpectations(t)
	})

	t.Run("build config error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		payload := `{"provider":"openai"}`
		resp, err := http.Post(server.URL+"/settings/llm/test", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("provider error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		original := newLLMProvider
		newLLMProvider = func(cfg llm.Config) (llm.Provider, error) {
			return nil, errors.New("provider")
		}
		t.Cleanup(func() {
			newLLMProvider = original
		})

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		payload := `{"provider":"openai","model":"gpt-4","api_key":"sk-test"}`
		resp, err := http.Post(server.URL+"/settings/llm/test", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("generate error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		providerMock := &MockProvider{}
		providerMock.On("Generate", mock.Anything, mock.Anything).Return("", errors.New("fail")).Once()

		original := newLLMProvider
		newLLMProvider = func(cfg llm.Config) (llm.Provider, error) {
			return providerMock, nil
		}
		t.Cleanup(func() {
			newLLMProvider = original
		})

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer server.Close()

		payload := `{"provider":"openai","model":"gpt-4","api_key":"sk-test"}`
		resp, err := http.Post(server.URL+"/settings/llm/test", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		providerMock.AssertExpectations(t)
	})

	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/settings/llm/test", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestListLLMModels(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"b"},{"id":"a"}]}`))
		}))
		defer server.Close()

		apiServer := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
		defer apiServer.Close()

		payload := map[string]string{"provider": "openai", "api_key": "key", "base_url": server.URL}
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)

		resp, err := http.Post(apiServer.URL+"/settings/llm/models", "application/json", bytes.NewReader(encoded))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var response map[string][]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&response))
		require.Equal(t, []string{"a", "b"}, response["models"])
	})

	t.Run("invalid json", func(t *testing.T) {
		apiServer := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer apiServer.Close()

		resp, err := http.Post(apiServer.URL+"/settings/llm/models", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("use stored settings", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		}))
		defer testServer.Close()

		key := "0123456789abcdef0123456789abcdef"
		cipher, err := secrets.Encrypt([]byte(key), "sk-test")
		require.NoError(t, err)
		settings := &store.LLMSettings{Provider: "openai", BaseURL: testServer.URL, APIKeyEnc: cipher, Model: "gpt-4"}
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(settings, nil).Once()
		apiServer := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai", LLMSecretsKey: key})
		defer apiServer.Close()

		resp, err := http.Post(apiServer.URL+"/settings/llm/models", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("fetch error", func(t *testing.T) {
		apiServer := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer apiServer.Close()

		resp, err := http.Post(apiServer.URL+"/settings/llm/models", "application/json", strings.NewReader(`{"provider":"openai"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestBuildLLMConfig(t *testing.T) {
	storeMock := &MockStore{}
	key := "0123456789abcdef0123456789abcdef"
	encoded, err := secrets.Encrypt([]byte(key), "sk-test")
	require.NoError(t, err)
	storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openrouter", APIKeyEnc: encoded, Model: "gpt-4"}, nil).Once()
	server := NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMSecretsKey: key, LLMProvider: "openrouter", LLMModel: "gpt-4"})

	llmConfig, err := server.buildLLMConfig(context.Background(), llmSettingsRequest{})
	require.NoError(t, err)
	require.Equal(t, "openrouter", llmConfig.Provider)
	require.Equal(t, "", llmConfig.OpenAIAPIKey)
	require.Equal(t, "sk-test", llmConfig.OpenRouterAPIKey)
	storeMock.AssertExpectations(t)

	storeMock = &MockStore{}
	storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai", APIKeyEnc: "cipher"}, nil).Once()
	server = NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai", LLMSecretsKey: "bad"})
	_, err = server.buildLLMConfig(context.Background(), llmSettingsRequest{})
	require.Error(t, err)

	storeMock = &MockStore{}
	storeMock.On("GetLLMSettings", mock.Anything).Return(nil, errors.New("boom")).Once()
	server = NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
	_, err = server.buildLLMConfig(context.Background(), llmSettingsRequest{})
	require.Error(t, err)

	storeMock = &MockStore{}
	storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai", APIKeyEnc: "bad"}, nil).Once()
	server = NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai", LLMSecretsKey: "0123456789abcdef0123456789abcdef"})
	_, err = server.buildLLMConfig(context.Background(), llmSettingsRequest{})
	require.Error(t, err)

	storeMock = &MockStore{}
	storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
	server = NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
	localConfig, err := server.buildLLMConfig(context.Background(), llmSettingsRequest{Mode: "local", Provider: "openai"})
	require.NoError(t, err)
	require.Equal(t, "local", localConfig.Mode)

	storeMock = &MockStore{}
	server = NewServer(storeMock, &MockBroker{}, nil, config.Config{LLMProvider: "openai"})
	apiConfig, err := server.buildLLMConfig(context.Background(), llmSettingsRequest{Provider: "openai", APIKey: "sk-test"})
	require.NoError(t, err)
	require.Equal(t, "sk-test", apiConfig.OpenAIAPIKey)
}

func TestFetchModels(t *testing.T) {
	t.Run("opencode-zen", func(t *testing.T) {
		// OpenCode now fetches from API or returns fallback
		models, err := fetchModels("opencode-zen", llmModelsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, models)
	})

	t.Run("missing api key", func(t *testing.T) {
		_, err := fetchModels("openai", llmModelsRequest{})
		require.Error(t, err)
	})

	t.Run("codex", func(t *testing.T) {
		models, err := fetchModels("codex", llmModelsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, models)
	})

	t.Run("http error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer testServer.Close()

		_, err := fetchModels("openai", llmModelsRequest{APIKey: "key", BaseURL: testServer.URL})
		require.Error(t, err)
	})

	t.Run("decode error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{"))
		}))
		defer testServer.Close()

		_, err := fetchModels("openai", llmModelsRequest{APIKey: "key", BaseURL: testServer.URL})
		require.Error(t, err)
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := fetchModels("openai", llmModelsRequest{APIKey: "key", BaseURL: "http://[::1"})
		require.Error(t, err)
	})

	t.Run("default openai base url", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.openai.com/v1/models", req.URL.String())
			body := io.NopCloser(strings.NewReader(`{"data":[{"id":""},{"id":"gpt-4"}]}`))
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})
		t.Cleanup(func() {
			http.DefaultTransport = original
		})

		models, err := fetchModels("openai", llmModelsRequest{APIKey: "key"})
		require.NoError(t, err)
		require.Equal(t, []string{"gpt-4"}, models)
	})

	t.Run("default openrouter base url", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://openrouter.ai/api/v1/models", req.URL.String())
			body := io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-4"}]}`))
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})
		t.Cleanup(func() {
			http.DefaultTransport = original
		})

		models, err := fetchModels("openrouter", llmModelsRequest{APIKey: "key"})
		require.NoError(t, err)
		require.Equal(t, []string{"gpt-4"}, models)
	})

	t.Run("request error", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		})
		t.Cleanup(func() {
			http.DefaultTransport = original
		})

		_, err := fetchModels("openai", llmModelsRequest{APIKey: "key", BaseURL: "http://example.com"})
		require.Error(t, err)
	})
}

func TestProviderNeedsKey(t *testing.T) {
	require.True(t, providerNeedsKey("openai"))
	require.True(t, providerNeedsKey("openrouter"))
	require.True(t, providerNeedsKey("opencode-zen"))
	require.False(t, providerNeedsKey("codex"))
}

func TestFetchOpenCodeModels(t *testing.T) {
	// Test that fetchOpenCodeModels returns models (either from API or fallback)
	models, err := fetchOpenCodeModels()
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Greater(t, len(models), 0)

	// Verify models are sorted and unique
	for i := 1; i < len(models); i++ {
		require.Less(t, models[i-1], models[i], "models should be sorted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFirstNonEmpty(t *testing.T) {
	require.Equal(t, "value", firstNonEmpty("value", "fallback"))
	require.Equal(t, "fallback", firstNonEmpty(" ", "fallback"))
}
