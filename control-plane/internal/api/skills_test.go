package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/skills"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func TestListSkills(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListSkills", mock.Anything).Return([]store.Skill{{ID: "1", Name: "alpha"}}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/skills")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload map[string][]skillResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Len(t, payload["skills"], 1)
		storeMock.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListSkills", mock.Anything).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/skills")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestCreateSkill(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("CreateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(nil).Twice()
		storeMock.On("ListSkillFiles", mock.Anything, mock.AnythingOfType("string")).Return([]store.SkillFile{
			{Path: "SKILL.md", Content: []byte(""), ContentType: "text/markdown"},
			{Path: "tool.txt", Content: []byte("hi"), ContentType: "text/plain"},
		}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"alpha","description":"desc","files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `","content_type":"text/plain"}]}`
		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
		rootDir, err := skills.RootDir()
		require.NoError(t, err)
		_, err = os.Stat(filepath.Join(rootDir, "alpha", "tool.txt"))
		require.NoError(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid name", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(`{"name":"bad name"}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid base64", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"alpha","files":[{"path":"tool.txt","content_base64":"@@"}]}`
		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateSkill", mock.Anything, mock.Anything).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"alpha"}`
		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("upsert error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("CreateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"alpha","files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}]}`
		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("materialize error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("CreateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(nil).Twice()
		storeMock.On("ListSkillFiles", mock.Anything, mock.AnythingOfType("string")).Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"name":"alpha","files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}]}`
		resp, err := http.Post(server.URL+"/skills", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestUpdateSkill(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "missing").Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/missing", strings.NewReader(`{"name":"alpha"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"name":"alpha"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("invalid json", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader("{"))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("invalid name", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"name":"bad name"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success rename", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "old"}, nil).Once()
		storeMock.On("UpdateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{{Path: "SKILL.md", Content: []byte(""), ContentType: "text/markdown"}}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"name":"new"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("update error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpdateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"name":"alpha"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("empty name uses existing", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha", Description: "desc"}, nil).Once()
		storeMock.On("UpdateSkill", mock.Anything, mock.MatchedBy(func(skill store.Skill) bool {
			return skill.Name == "alpha"
		})).Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"description":""}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("materialize error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpdateSkill", mock.Anything, mock.AnythingOfType("store.Skill")).Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/skills/skill-1", strings.NewReader(`{"name":"alpha"}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestDeleteSkill(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "missing").Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/missing", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("DeleteSkill", mock.Anything, "skill-1").Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("delete", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)
		rootDir, err := skills.RootDir()
		require.NoError(t, err)
		os.MkdirAll(filepath.Join(rootDir, "alpha"), 0o755)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("DeleteSkill", mock.Anything, "skill-1").Return(nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestListSkillFiles(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{
			{Path: "SKILL.md", Content: []byte("hi"), ContentType: "text/markdown", SizeBytes: 2, UpdatedAt: "now"},
		}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/skills/skill-1/files")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload map[string][]skillFileResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("hi")), payload["files"][0].ContentBase)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/skills/skill-1/files")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestUpsertSkillFiles(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "missing").Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/skills/missing/files", "application/json", strings.NewReader(`{"files":[]}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(`{"files":[]}`))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("invalid json", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("invalid base64", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"files":[{"path":"tool.txt","content_base64":"@@"}]}`
		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("upsert error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}]}`
		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("delete error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(nil).Twice()
		storeMock.On("DeleteSkillFile", mock.Anything, "skill-1", "old.txt").Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}],"delete_paths":["old.txt"]}`
		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(nil).Twice()
		storeMock.On("DeleteSkillFile", mock.Anything, "skill-1", "old.txt").Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{
			{Path: "SKILL.md", Content: []byte(""), ContentType: "text/markdown"},
			{Path: "tool.txt", Content: []byte("hi"), ContentType: "text/plain"},
		}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `","content_type":"text/plain"}],"delete_paths":["old.txt"]}`
		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("materialize error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(nil).Twice()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := `{"files":[{"path":"tool.txt","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}]}`
		resp, err := http.Post(server.URL+"/skills/skill-1/files", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestDeleteSkillFiles(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "missing").Return(nil, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/missing/files", strings.NewReader(`{"delete_paths":["old.txt"]}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1/files", strings.NewReader(`{"delete_paths":["old.txt"]}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("invalid json", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1/files", strings.NewReader("{"))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("DeleteSkillFile", mock.Anything, "skill-1", "old.txt").Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{{Path: "SKILL.md", Content: []byte(""), ContentType: "text/markdown"}}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1/files", strings.NewReader(`{"delete_paths":["old.txt"]}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("delete error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("DeleteSkillFile", mock.Anything, "skill-1", "old.txt").Return(errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1/files", strings.NewReader(`{"delete_paths":["old.txt"]}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("materialize error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)

		storeMock := &MockStore{}
		storeMock.On("GetSkill", mock.Anything, "skill-1").Return(&store.Skill{ID: "skill-1", Name: "alpha"}, nil).Once()
		storeMock.On("DeleteSkillFile", mock.Anything, "skill-1", "old.txt").Return(nil).Once()
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		req, err := http.NewRequest(http.MethodDelete, server.URL+"/skills/skill-1/files", strings.NewReader(`{"delete_paths":["old.txt"]}`))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestValidateSkillName(t *testing.T) {
	require.Error(t, validateSkillName(""))
	require.Error(t, validateSkillName("bad name"))
	require.Error(t, validateSkillName("bad/dir"))
	require.NoError(t, validateSkillName("good-name"))
}

func TestEnsureSkillMarkdown(t *testing.T) {
	files := ensureSkillMarkdown([]skillFilePayload{{Path: "SKILL.md", ContentBase: ""}})
	require.Len(t, files, 1)
	files = ensureSkillMarkdown([]skillFilePayload{{Path: "tool.txt", ContentBase: ""}})
	require.Len(t, files, 2)
}

func TestDecodeSkillFiles(t *testing.T) {
	_, err := decodeSkillFiles([]skillFilePayload{{Path: "", ContentBase: ""}})
	require.Error(t, err)

	_, err = decodeSkillFiles([]skillFilePayload{{Path: "../evil", ContentBase: ""}})
	require.Error(t, err)

	_, err = decodeSkillFiles([]skillFilePayload{{Path: "a.txt", ContentBase: "@@"}})
	require.Error(t, err)

	entries, err := decodeSkillFiles([]skillFilePayload{{Path: "SKILL.md", ContentBase: base64.StdEncoding.EncodeToString([]byte(""))}})
	require.NoError(t, err)
	require.Equal(t, "text/markdown", entries[0].ContentType)
}

func TestUpsertSkillFilesHelper(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("UpsertSkillFile", mock.Anything, mock.AnythingOfType("store.SkillFile")).Return(errors.New("boom")).Once()
	files := []store.SkillFile{{Path: "tool.txt", Content: []byte("hi")}}
	err := upsertSkillFiles(context.Background(), storeMock, "skill-1", files, "now")
	require.Error(t, err)
	storeMock.AssertExpectations(t)
}

func TestMaterializeSkillFiles(t *testing.T) {
	t.Run("root dir error", func(t *testing.T) {
		home, hasHome := os.LookupEnv("HOME")
		xdg, hasXdg := os.LookupEnv("XDG_CONFIG_HOME")
		t.Cleanup(func() {
			if hasHome {
				_ = os.Setenv("HOME", home)
			} else {
				_ = os.Unsetenv("HOME")
			}
			if hasXdg {
				_ = os.Setenv("XDG_CONFIG_HOME", xdg)
			} else {
				_ = os.Unsetenv("XDG_CONFIG_HOME")
			}
		})
		_ = os.Unsetenv("HOME")
		_ = os.Unsetenv("XDG_CONFIG_HOME")
		storeMock := &MockStore{}
		err := materializeSkillFiles(context.Background(), storeMock, "skill-1", "alpha", "")
		require.Error(t, err)
	})

	t.Run("list error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)
		storeMock := &MockStore{}
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return(nil, errors.New("boom")).Once()
		err := materializeSkillFiles(context.Background(), storeMock, "skill-1", "alpha", "")
		require.Error(t, err)
		storeMock.AssertExpectations(t)
	})

	t.Run("materialize error", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)
		storeMock := &MockStore{}
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{}, nil).Once()
		err := materializeSkillFiles(context.Background(), storeMock, "skill-1", "", "")
		require.Error(t, err)
		storeMock.AssertExpectations(t)
	})

	t.Run("remove old name", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", root)
		rootDir, err := skills.RootDir()
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "old"), 0o755))

		storeMock := &MockStore{}
		storeMock.On("ListSkillFiles", mock.Anything, "skill-1").Return([]store.SkillFile{}, nil).Once()
		err = materializeSkillFiles(context.Background(), storeMock, "skill-1", "new", "old")
		require.NoError(t, err)
		storeMock.AssertExpectations(t)
	})
}
