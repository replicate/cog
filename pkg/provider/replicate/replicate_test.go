package replicate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
)

func TestReplicateProvider_Name(t *testing.T) {
	p := New()
	require.Equal(t, "replicate", p.Name())
}

func TestReplicateProvider_MatchesRegistry(t *testing.T) {
	p := New()

	// Should match default r8.im
	require.True(t, p.MatchesRegistry("r8.im"))

	// Should match the current global registry host (in case it was overridden)
	require.True(t, p.MatchesRegistry(global.ReplicateRegistryHost))

	// Should not match other registries
	require.False(t, p.MatchesRegistry("ghcr.io"))
	require.False(t, p.MatchesRegistry("docker.io"))
	require.False(t, p.MatchesRegistry("gcr.io"))
	require.False(t, p.MatchesRegistry("myregistry.example.com"))
}

func TestReplicateProvider_PostPush(t *testing.T) {
	p := New()
	opts := provider.PushOptions{
		Image: "r8.im/user/model",
	}

	t.Run("success", func(t *testing.T) {
		err := p.PostPush(context.Background(), opts, nil)
		require.NoError(t, err)
	})

	t.Run("repository not found error", func(t *testing.T) {
		// Simulate a NotFoundError from docker push (repository doesn't exist)
		pushErr := &command.NotFoundError{Ref: "r8.im/user/model", Object: "repository"}
		err := p.PostPush(context.Background(), opts, pushErr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "Unable to find existing Replicate model")
		require.Contains(t, err.Error(), "replicate.com and create a new model")
	})

	t.Run("tag not found error", func(t *testing.T) {
		// Tag not found errors should also trigger the helpful message
		pushErr := &command.NotFoundError{Ref: "r8.im/user/model:v1", Object: "tag"}
		err := p.PostPush(context.Background(), opts, pushErr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "Unable to find existing Replicate model")
	})
}

func TestCheckTokenFormat(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid CLI token",
			token:   "abc123def456",
			wantErr: false,
		},
		{
			name:    "API token rejected",
			token:   "r8_abc123",
			wantErr: true,
		},
		{
			name:    "empty token allowed (separate validation)",
			token:   "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTokenFormat(tt.token)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "API token")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyToken(t *testing.T) {
	t.Run("successful verification", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/cog/v1/verify-token", r.URL.Path)
			require.Equal(t, "POST", r.Method)

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"username": "testuser"})
		}))
		defer server.Close()

		username, err := verifyToken(server.URL, "valid-token")
		require.NoError(t, err)
		require.Equal(t, "testuser", username)
	})

	t.Run("empty token", func(t *testing.T) {
		_, err := verifyToken("http://localhost", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty")
	})

	t.Run("user not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := verifyToken(server.URL, "unknown-token")
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		_, err := verifyToken(server.URL, "some-token")
		require.Error(t, err)
		require.Contains(t, err.Error(), "500")
	})
}

func TestGetDisplayTokenURL(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/cog/v1/display-token-url", r.URL.Path)
			require.Equal(t, "GET", r.Method)

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"url": "https://replicate.com/auth/token"})
		}))
		defer server.Close()

		url, err := getDisplayTokenURL(server.URL)
		require.NoError(t, err)
		require.Equal(t, "https://replicate.com/auth/token", url)
	})

	t.Run("not replicate registry", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := getDisplayTokenURL(server.URL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not the Replicate registry")
	})
}

func TestAddressWithScheme(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"r8.im", "https://r8.im"},
		{"https://r8.im", "https://r8.im"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"myregistry.com", "https://myregistry.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := addressWithScheme(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
