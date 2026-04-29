package model

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

func TestPlanImport_NewWeight(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"model.safetensors": []byte("weights-data"),
		"config.json":       []byte(`{"hidden_size": 768}`),
	})

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "my-model", Target: "/src/weights", Source: &config.WeightSourceConfig{URI: "weights"}},
		},
	}, projectDir)

	lockPath := filepath.Join(projectDir, "weights.lock")
	builder := NewWeightBuilder(src, nil, lockPath)

	spec := testWeightSpec(t, "my-model", "weights", "/src/weights")
	plan, err := builder.PlanImport(context.Background(), spec)
	require.NoError(t, err)

	assert.Equal(t, "my-model", plan.Spec.Name())
	assert.Equal(t, PlanStatusNew, plan.Status)
	assert.Len(t, plan.FilteredFiles(), 2)
	assert.Greater(t, plan.TotalSize(), int64(0))
	assert.Empty(t, plan.Changes)
}

func TestPlanImport_Unchanged(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"config.json": []byte(`{"hidden_size": 768}`),
	})

	weights := []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "weights"}},
	}
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)

	wb, _ := newTestBuilder(t, projectDir, weights)
	spec := testWeightSpec(t, "w", "weights", "/src/w")
	_, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lockPath := filepath.Join(projectDir, "weights.lock")
	planner := NewWeightBuilder(src, nil, lockPath)
	plan, err := planner.PlanImport(context.Background(), spec)
	require.NoError(t, err)

	assert.Equal(t, PlanStatusUnchanged, plan.Status)
	assert.Empty(t, plan.Changes)
}

func TestPlanImport_ConfigChanged(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"model.safetensors": []byte("data"),
		"config.json":       []byte("{}"),
		"model.onnx":        []byte("onnx-data"),
	})

	weights := []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "weights"}},
	}

	wb, _ := newTestBuilder(t, projectDir, weights)
	spec := testWeightSpec(t, "w", "weights", "/src/w")
	_, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	specWithExclude, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "w",
		Target: "/src/w",
		Source: &config.WeightSourceConfig{URI: "weights", Exclude: []string{"*.onnx"}},
	})
	require.NoError(t, err)

	lockPath := filepath.Join(projectDir, "weights.lock")
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)
	planner := NewWeightBuilder(src, nil, lockPath)
	plan, err := planner.PlanImport(context.Background(), specWithExclude)
	require.NoError(t, err)

	assert.Equal(t, PlanStatusConfigChanged, plan.Status)
	require.NotEmpty(t, plan.Changes)
	assert.Contains(t, plan.Changes[0], "exclude")

	filtered := plan.FilteredFiles()
	assert.Len(t, filtered, 2)
	for _, f := range filtered {
		assert.NotEqual(t, "model.onnx", f.Path)
	}
}

func TestPlanImport_WithFilter_ShowsExcluded(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"model.safetensors": []byte("data"),
		"config.json":       []byte("{}"),
		"model.onnx":        []byte("onnx-data"),
	})

	spec, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "w",
		Target: "/src/w",
		Source: &config.WeightSourceConfig{URI: "weights", Exclude: []string{"*.onnx"}},
	})
	require.NoError(t, err)

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "weights", Exclude: []string{"*.onnx"}}},
		},
	}, projectDir)

	lockPath := filepath.Join(projectDir, "weights.lock")
	planner := NewWeightBuilder(src, nil, lockPath)
	plan, err := planner.PlanImport(context.Background(), spec)
	require.NoError(t, err)

	require.NotEmpty(t, plan.UnfilteredFiles)
	assert.Len(t, plan.UnfilteredFiles, 3)
	assert.Len(t, plan.FilteredFiles(), 2)

	excluded := plan.ExcludedFiles()
	require.Len(t, excluded, 1)
	assert.Equal(t, "model.onnx", excluded[0].Path)
}

func TestPlanImport_UpstreamChanged(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"config.json": []byte("v1"),
	})

	weights := []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "weights"}},
	}

	wb, _ := newTestBuilder(t, projectDir, weights)
	spec := testWeightSpec(t, "w", "weights", "/src/w")
	_, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"config.json": []byte("v2-different-content"),
	})

	lockPath := filepath.Join(projectDir, "weights.lock")
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)
	planner := NewWeightBuilder(src, nil, lockPath)
	plan, err := planner.PlanImport(context.Background(), spec)
	require.NoError(t, err)

	assert.Equal(t, PlanStatusUpstreamChanged, plan.Status)
	require.Len(t, plan.Changes, 1)
	assert.Contains(t, plan.Changes[0], "fingerprint")
}

func TestDescribeSpecDrift(t *testing.T) {
	tests := []struct {
		name    string
		config  *WeightSpec
		lock    *WeightSpec
		wantLen int
		wantSub string
	}{
		{
			name:    "URI changed",
			config:  &WeightSpec{URI: "hf://org/new", Target: "/src/w"},
			lock:    &WeightSpec{URI: "hf://org/old", Target: "/src/w"},
			wantLen: 1,
			wantSub: "uri",
		},
		{
			name:    "target changed",
			config:  &WeightSpec{URI: "hf://org/m", Target: "/src/new"},
			lock:    &WeightSpec{URI: "hf://org/m", Target: "/src/old"},
			wantLen: 1,
			wantSub: "target",
		},
		{
			name:    "include changed",
			config:  &WeightSpec{URI: "hf://org/m", Target: "/src/w", Include: []string{"*.json"}},
			lock:    &WeightSpec{URI: "hf://org/m", Target: "/src/w"},
			wantLen: 1,
			wantSub: "include",
		},
		{
			name:    "multiple changes",
			config:  &WeightSpec{URI: "new-uri", Target: "/new", Exclude: []string{"*.bin"}},
			lock:    &WeightSpec{URI: "old-uri", Target: "/old"},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := describeSpecDrift(tt.config, tt.lock)
			assert.Len(t, changes, tt.wantLen)
			if tt.wantSub != "" {
				assert.Contains(t, changes[0], tt.wantSub)
			}
		})
	}
}

func TestBuildFromPlan_MatchesBuild(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"model.safetensors": []byte("weights-data"),
		"config.json":       []byte(`{"hidden_size": 768}`),
		"model.onnx":        []byte("onnx-data"),
	})

	weights := []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{
			URI: "weights", Exclude: []string{"*.onnx"},
		}},
	}

	spec, err := WeightSpecFromConfig(weights[0])
	require.NoError(t, err)

	// Plan with nil-store builder.
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)
	lockPath := filepath.Join(projectDir, "weights.lock")
	planner := NewWeightBuilder(src, nil, lockPath)
	plan, err := planner.PlanImport(context.Background(), spec)
	require.NoError(t, err)
	assert.Equal(t, PlanStatusNew, plan.Status)

	// BuildFromPlan with a real-store builder.
	builder, _ := newTestBuilder(t, projectDir, weights)
	artifact, err := builder.BuildFromPlan(context.Background(), spec, plan)
	require.NoError(t, err)

	wa, ok := artifact.(*WeightArtifact)
	require.True(t, ok)
	assert.Equal(t, "w", wa.Name())
	assert.Equal(t, "/src/w", wa.Entry.Target)

	// Verify the onnx file was excluded.
	for _, f := range wa.Entry.Files {
		assert.NotEqual(t, "model.onnx", f.Path, "excluded file should not appear")
	}
	assert.Len(t, wa.Entry.Files, 2)
}

func TestPlanImport_NoLockfile(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights", map[string][]byte{
		"data.bin": []byte("some data"),
	})

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "weights"}},
		},
	}, projectDir)

	lockPath := filepath.Join(projectDir, lockfile.WeightsLockFilename)
	planner := NewWeightBuilder(src, nil, lockPath)
	spec := testWeightSpec(t, "w", "weights", "/src/w")
	plan, err := planner.PlanImport(context.Background(), spec)
	require.NoError(t, err)
	assert.Equal(t, PlanStatusNew, plan.Status)
}
