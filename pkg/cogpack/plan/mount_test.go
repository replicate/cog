package plan

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMount_JSON_Serialization(t *testing.T) {
	tests := []struct {
		name  string
		mount Mount
		want  string
	}{
		{
			name: "local mount",
			mount: Mount{
				Source: Input{Local: "wheel-context"},
				Target: "/mnt/wheel",
			},
			want: `{"source":{"local":"wheel-context"},"target":"/mnt/wheel"}`,
		},
		{
			name: "stage mount",
			mount: Mount{
				Source: Input{Stage: "python-deps"},
				Target: "/mnt/deps",
			},
			want: `{"source":{"stage":"python-deps"},"target":"/mnt/deps"}`,
		},
		{
			name: "image mount",
			mount: Mount{
				Source: Input{Image: "ubuntu:20.04"},
				Target: "/mnt/base",
			},
			want: `{"source":{"image":"ubuntu:20.04"},"target":"/mnt/base"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.mount)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(data))

			// Test unmarshaling
			var mount Mount
			err = json.Unmarshal(data, &mount)
			require.NoError(t, err)
			assert.Equal(t, tt.mount, mount)
		})
	}
}

func TestExec_WithMounts(t *testing.T) {
	exec := Exec{
		Command: "uv pip install /mnt/wheel/*.whl",
		Mounts: []Mount{
			{
				Source: Input{Local: "wheel-context"},
				Target: "/mnt/wheel",
			},
			{
				Source: Input{Stage: "cache-stage"},
				Target: "/mnt/cache",
			},
		},
	}

	// Test JSON serialization
	data, err := json.Marshal(exec)
	require.NoError(t, err)

	var unmarshaled Exec
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, exec.Command, unmarshaled.Command)
	assert.Len(t, unmarshaled.Mounts, 2)
	assert.Equal(t, exec.Mounts[0].Source.Local, unmarshaled.Mounts[0].Source.Local)
	assert.Equal(t, exec.Mounts[0].Target, unmarshaled.Mounts[0].Target)
	assert.Equal(t, exec.Mounts[1].Source.Stage, unmarshaled.Mounts[1].Source.Stage)
	assert.Equal(t, exec.Mounts[1].Target, unmarshaled.Mounts[1].Target)
}

func TestCopy_Serialization(t *testing.T) {
	copy := Copy{
		From: Input{Stage: "source-stage"},
		Src:  []string{"/app/src"},
		Dest: "/app/dest",
	}

	// Test JSON serialization
	data, err := json.Marshal(copy)
	require.NoError(t, err)

	var unmarshaled Copy
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, copy.From, unmarshaled.From)
	assert.Equal(t, copy.Src, unmarshaled.Src)
	assert.Equal(t, copy.Dest, unmarshaled.Dest)
}

func TestAdd_Serialization(t *testing.T) {
	add := Add{
		Src:  []string{"https://example.com/file.tar.gz"},
		Dest: "/app/downloads",
	}

	// Test JSON serialization
	data, err := json.Marshal(add)
	require.NoError(t, err)

	var unmarshaled Add
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, add.Src, unmarshaled.Src)
	assert.Equal(t, add.Dest, unmarshaled.Dest)
}