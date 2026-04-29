package lockfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDrift(t *testing.T) {
	tests := []struct {
		name   string
		lock   *WeightsLock
		config []ConfigWeight
		want   []DriftResult
	}{
		{
			name: "no drift when config matches lockfile",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "model-a", Target: "/weights/a", Source: WeightLockSource{URI: "file://./a", Include: []string{}, Exclude: []string{}}},
					{Name: "model-b", Target: "/weights/b", Source: WeightLockSource{URI: "file://./b", Include: []string{"*.bin"}, Exclude: []string{"README*"}}},
				},
			},
			config: []ConfigWeight{
				{Name: "model-a", URI: "file://./a", Target: "/weights/a", Include: []string{}, Exclude: []string{}},
				{Name: "model-b", URI: "file://./b", Target: "/weights/b", Include: []string{"*.bin"}, Exclude: []string{"README*"}},
			},
			want: []DriftResult{},
		},
		{
			name: "orphaned: lockfile entry not in config",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "kept", Target: "/kept", Source: WeightLockSource{URI: "file://./kept", Include: []string{}, Exclude: []string{}}},
					{Name: "removed", Target: "/removed", Source: WeightLockSource{URI: "file://./removed", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "kept", URI: "file://./kept", Target: "/kept", Include: []string{}, Exclude: []string{}},
			},
			want: []DriftResult{
				{Name: "removed", Kind: DriftOrphaned},
			},
		},
		{
			name: "pending: config weight not in lockfile",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "existing", Target: "/existing", Source: WeightLockSource{URI: "file://./existing", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "existing", URI: "file://./existing", Target: "/existing", Include: []string{}, Exclude: []string{}},
				{Name: "new-weight", URI: "file://./new", Target: "/new"},
			},
			want: []DriftResult{
				{Name: "new-weight", Kind: DriftPending},
			},
		},
		{
			name: "config-changed: URI differs",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "w", Target: "/w", Source: WeightLockSource{URI: "file://./old", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", URI: "file://./new", Target: "/w", Include: []string{}, Exclude: []string{}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "uri: file://./old → file://./new"},
			},
		},
		{
			name: "config-changed: target differs",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "w", Target: "/old-path", Source: WeightLockSource{URI: "file://./w", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", URI: "file://./w", Target: "/new-path", Include: []string{}, Exclude: []string{}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "target: /old-path → /new-path"},
			},
		},
		{
			name: "config-changed: include differs",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "w", Target: "/w", Source: WeightLockSource{URI: "file://./w", Include: []string{"*.bin"}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", URI: "file://./w", Target: "/w", Include: []string{"*.safetensors"}, Exclude: []string{}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "include: [*.bin] → [*.safetensors]"},
			},
		},
		{
			name: "config-changed: exclude differs",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "w", Target: "/w", Source: WeightLockSource{URI: "file://./w", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", URI: "file://./w", Target: "/w", Include: []string{}, Exclude: []string{"README*"}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "exclude: [] → [README*]"},
			},
		},
		{
			name: "config-changed: multiple fields differ",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "w", Target: "/old-path", Source: WeightLockSource{URI: "file://./old", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", URI: "file://./new", Target: "/new-path", Include: []string{}, Exclude: []string{}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "uri: file://./old → file://./new; target: /old-path → /new-path"},
			},
		},
		{
			name:   "nil lockfile with no config weights",
			lock:   nil,
			config: nil,
			want:   []DriftResult{},
		},
		{
			name: "nil lockfile with config weights: all pending",
			lock: nil,
			config: []ConfigWeight{
				{Name: "a", URI: "file://./a", Target: "/a"},
				{Name: "b", URI: "file://./b", Target: "/b"},
			},
			want: []DriftResult{
				{Name: "a", Kind: DriftPending},
				{Name: "b", Kind: DriftPending},
			},
		},
		{
			name: "empty lockfile with config weights: all pending",
			lock: &WeightsLock{Version: Version, Weights: []WeightLockEntry{}},
			config: []ConfigWeight{
				{Name: "a", URI: "file://./a", Target: "/a"},
			},
			want: []DriftResult{
				{Name: "a", Kind: DriftPending},
			},
		},
		{
			name: "multiple drift types in one check",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "ok", Target: "/ok", Source: WeightLockSource{URI: "file://./ok", Include: []string{}, Exclude: []string{}}},
					{Name: "stale", Target: "/stale", Source: WeightLockSource{URI: "file://./old-uri", Include: []string{}, Exclude: []string{}}},
					{Name: "orphan", Target: "/orphan", Source: WeightLockSource{URI: "file://./orphan", Include: []string{}, Exclude: []string{}}},
				},
			},
			config: []ConfigWeight{
				{Name: "ok", URI: "file://./ok", Target: "/ok", Include: []string{}, Exclude: []string{}},
				{Name: "stale", URI: "file://./new-uri", Target: "/stale", Include: []string{}, Exclude: []string{}},
				{Name: "brand-new", URI: "file://./brand-new", Target: "/brand-new"},
			},
			want: []DriftResult{
				{Name: "stale", Kind: DriftConfigChanged, Details: "uri: file://./old-uri → file://./new-uri"},
				{Name: "brand-new", Kind: DriftPending},
				{Name: "orphan", Kind: DriftOrphaned},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckDrift(tt.lock, tt.config)
			if got == nil {
				got = []DriftResult{}
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want, got[i], "result[%d]", i)
			}
		})
	}
}
