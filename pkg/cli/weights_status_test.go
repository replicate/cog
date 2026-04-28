package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

func TestStatusResultsToEntries(t *testing.T) {
	results := []model.WeightStatusResult{
		{
			Name:   "base",
			Target: "/weights/base",
			Status: model.WeightStatusReady,
			LockEntry: &lockfile.WeightLockEntry{
				Size:           4096,
				SizeCompressed: 2048,
				Layers:         []lockfile.WeightLockLayer{{Digest: "sha256:l1"}, {Digest: "sha256:l2"}},
				Files:          []lockfile.WeightLockFile{{Path: "a.bin"}, {Path: "b.bin"}, {Path: "c.bin"}},
				Digest:         "sha256:manifest123",
				Source:         lockfile.WeightLockSource{URI: "file://./weights"},
			},
			Layers: []model.LayerStatusResult{
				{Digest: "sha256:l1", Size: 2048, Status: model.LayerStatusReady},
				{Digest: "sha256:l2", Size: 2048, Status: model.LayerStatusReady},
			},
		},
		{
			Name:   "pending",
			Target: "/weights/new",
			Status: model.WeightStatusPending,
		},
	}

	entries := statusResultsToEntries(results)

	require.Len(t, entries, 2)

	assert.Equal(t, "base", entries[0].Name)
	assert.Equal(t, model.WeightStatusReady, entries[0].Status)
	assert.Equal(t, int64(4096), entries[0].Size)
	assert.Equal(t, int64(2048), entries[0].SizeCompressed)
	assert.Equal(t, 2, entries[0].LayerCount)
	assert.Equal(t, 3, entries[0].FileCount)
	assert.Equal(t, "sha256:manifest123", entries[0].Digest)
	require.NotNil(t, entries[0].Source)
	assert.Equal(t, "file://./weights", entries[0].Source.URI)
	require.Len(t, entries[0].Layers, 2)
	assert.Equal(t, model.LayerStatusReady, entries[0].Layers[0].Status)

	assert.Equal(t, "pending", entries[1].Name)
	assert.Equal(t, model.WeightStatusPending, entries[1].Status)
	assert.Equal(t, int64(0), entries[1].Size)
	assert.Nil(t, entries[1].Source)
	assert.Empty(t, entries[1].Layers)
}

func TestWeightsStatusJSONOutput(t *testing.T) {
	out := &WeightsStatusOutput{
		Weights: []WeightStatusEntry{
			{
				Name:           "base",
				Target:         "/weights/base",
				Status:         model.WeightStatusReady,
				Size:           4096,
				SizeCompressed: 2048,
				LayerCount:     2,
				FileCount:      3,
				Digest:         "sha256:abc123",
				Source:         &WeightStatusSource{URI: "file://./weights", Fingerprint: "sha256:def456"},
				Layers: []LayerStatusEntry{
					{Digest: "sha256:l1", Size: 2048, Status: "ready"},
					{Digest: "sha256:l2", Size: 2048, Status: "ready"},
				},
			},
			{
				Name:   "pending-weight",
				Target: "/weights/new",
				Status: model.WeightStatusPending,
			},
		},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(out))

	var decoded WeightsStatusOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))

	require.Len(t, decoded.Weights, 2)

	ready := decoded.Weights[0]
	assert.Equal(t, "base", ready.Name)
	assert.Equal(t, "ready", ready.Status)
	assert.Len(t, ready.Layers, 2)

	pending := decoded.Weights[1]
	assert.Equal(t, "pending-weight", pending.Name)
	assert.Equal(t, "pending", pending.Status)
	assert.Empty(t, pending.Layers)
}

func TestFormatDigestShort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sha256:a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6", "sha256:a1b2c3d4e5f6"},
		{"sha256:short", "sha256:short"},
		{"noprefix", "noprefix"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, formatDigestShort(tt.input))
		})
	}
}

func TestPrintWeightsStatusText(t *testing.T) {
	// Smoke test — just make sure it doesn't panic on various inputs.
	printWeightsStatusText(&WeightsStatusOutput{}, false)
	printWeightsStatusText(&WeightsStatusOutput{
		Weights: []WeightStatusEntry{
			{Name: "a", Target: "/a", Status: "ready", Size: 1073741824, LayerCount: 3, Digest: "sha256:abcdef123456"},
			{Name: "b", Target: "/b", Status: "pending"},
			{Name: "c", Target: "/c", Status: "orphaned", Size: 512, LayerCount: 1, Digest: "sha256:orphan999999"},
		},
	}, false)
}

func TestPrintWeightsStatusText_Verbose(t *testing.T) {
	// Smoke test for verbose output with layer tree.
	printWeightsStatusText(&WeightsStatusOutput{
		Weights: []WeightStatusEntry{
			{
				Name: "base", Target: "/w", Status: "incomplete",
				Size: 5000000000, LayerCount: 3, Digest: "sha256:abc123",
				Layers: []LayerStatusEntry{
					{Digest: "sha256:l1", Size: 2000000000, Status: "ready"},
					{Digest: "sha256:l2", Size: 2000000000, Status: "missing"},
					{Digest: "sha256:l3", Size: 1000000000, Status: "ready"},
				},
			},
		},
	}, true)
}
