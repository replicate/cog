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
					{Name: "model-a", Target: "/weights/a", Sources: []WeightLockSource{{URI: "file://./a", Include: []string{}, Exclude: []string{}}}},
					{Name: "model-b", Target: "/weights/b", Sources: []WeightLockSource{{URI: "file://./b", Include: []string{"*.bin"}, Exclude: []string{"README*"}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "model-a", Target: "/weights/a", Sources: []ConfigSourceEntry{{URI: "file://./a", Include: []string{}, Exclude: []string{}}}},
				{Name: "model-b", Target: "/weights/b", Sources: []ConfigSourceEntry{{URI: "file://./b", Include: []string{"*.bin"}, Exclude: []string{"README*"}}}},
			},
			want: []DriftResult{},
		},
		{
			name: "orphaned: lockfile entry not in config",
			lock: &WeightsLock{
				Version: Version,
				Weights: []WeightLockEntry{
					{Name: "kept", Target: "/kept", Sources: []WeightLockSource{{URI: "file://./kept", Include: []string{}, Exclude: []string{}}}},
					{Name: "removed", Target: "/removed", Sources: []WeightLockSource{{URI: "file://./removed", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "kept", Target: "/kept", Sources: []ConfigSourceEntry{{URI: "file://./kept", Include: []string{}, Exclude: []string{}}}},
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
					{Name: "existing", Target: "/existing", Sources: []WeightLockSource{{URI: "file://./existing", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "existing", Target: "/existing", Sources: []ConfigSourceEntry{{URI: "file://./existing", Include: []string{}, Exclude: []string{}}}},
				{Name: "new-weight", Target: "/new", Sources: []ConfigSourceEntry{{URI: "file://./new"}}},
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
					{Name: "w", Target: "/w", Sources: []WeightLockSource{{URI: "file://./old", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", Target: "/w", Sources: []ConfigSourceEntry{{URI: "file://./new", Include: []string{}, Exclude: []string{}}}},
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
					{Name: "w", Target: "/old-path", Sources: []WeightLockSource{{URI: "file://./w", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", Target: "/new-path", Sources: []ConfigSourceEntry{{URI: "file://./w", Include: []string{}, Exclude: []string{}}}},
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
					{Name: "w", Target: "/w", Sources: []WeightLockSource{{URI: "file://./w", Include: []string{"*.bin"}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", Target: "/w", Sources: []ConfigSourceEntry{{URI: "file://./w", Include: []string{"*.safetensors"}, Exclude: []string{}}}},
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
					{Name: "w", Target: "/w", Sources: []WeightLockSource{{URI: "file://./w", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", Target: "/w", Sources: []ConfigSourceEntry{{URI: "file://./w", Include: []string{}, Exclude: []string{"README*"}}}},
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
					{Name: "w", Target: "/old-path", Sources: []WeightLockSource{{URI: "file://./old", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "w", Target: "/new-path", Sources: []ConfigSourceEntry{{URI: "file://./new", Include: []string{}, Exclude: []string{}}}},
			},
			want: []DriftResult{
				{Name: "w", Kind: DriftConfigChanged, Details: "target: /old-path → /new-path; uri: file://./old → file://./new"},
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
				{Name: "a", Target: "/a", Sources: []ConfigSourceEntry{{URI: "file://./a"}}},
				{Name: "b", Target: "/b", Sources: []ConfigSourceEntry{{URI: "file://./b"}}},
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
				{Name: "a", Target: "/a", Sources: []ConfigSourceEntry{{URI: "file://./a"}}},
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
					{Name: "ok", Target: "/ok", Sources: []WeightLockSource{{URI: "file://./ok", Include: []string{}, Exclude: []string{}}}},
					{Name: "stale", Target: "/stale", Sources: []WeightLockSource{{URI: "file://./old-uri", Include: []string{}, Exclude: []string{}}}},
					{Name: "orphan", Target: "/orphan", Sources: []WeightLockSource{{URI: "file://./orphan", Include: []string{}, Exclude: []string{}}}},
				},
			},
			config: []ConfigWeight{
				{Name: "ok", Target: "/ok", Sources: []ConfigSourceEntry{{URI: "file://./ok", Include: []string{}, Exclude: []string{}}}},
				{Name: "stale", Target: "/stale", Sources: []ConfigSourceEntry{{URI: "file://./new-uri", Include: []string{}, Exclude: []string{}}}},
				{Name: "brand-new", Target: "/brand-new", Sources: []ConfigSourceEntry{{URI: "file://./brand-new"}}},
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
