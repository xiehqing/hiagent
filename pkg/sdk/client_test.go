package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xiehqing/hiagent/internal/proto"
)

func TestClientVersionInfo(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/version", r.URL.Path)
		require.Equal(t, http.MethodGet, r.Method)
		_ = json.NewEncoder(w).Encode(proto.VersionInfo{
			Version:   "1.0.0",
			Commit:    "abc123",
			GoVersion: "go1.26.2",
			Platform:  "windows/amd64",
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	require.NoError(t, err)

	info, err := client.VersionInfo(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1.0.0", info.Version)
	require.Equal(t, "abc123", info.Commit)
}

func TestClientCreateWorkspace(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/workspaces", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req proto.Workspace
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "C:/repo", req.Path)

		_ = json.NewEncoder(w).Encode(proto.Workspace{
			ID:   "ws-1",
			Path: req.Path,
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	require.NoError(t, err)

	workspace, err := client.CreateWorkspace(context.Background(), proto.Workspace{
		Path: "C:/repo",
	})
	require.NoError(t, err)
	require.Equal(t, "ws-1", workspace.ID)
}

func TestClientErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(proto.Error{Message: "bad request"})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	require.NoError(t, err)

	err = client.Health(context.Background())
	require.Error(t, err)

	var sdkErr *Error
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode)
	require.Equal(t, "bad request", sdkErr.Message)
}

func TestNewClientRequiresAbsoluteBaseURL(t *testing.T) {
	t.Parallel()

	_, err := NewClient("127.0.0.1:8080")
	require.Error(t, err)
}
