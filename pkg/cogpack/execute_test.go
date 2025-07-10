package cogpack

import (
	"context"
	"os"
	"testing"

	"github.com/replicate/cog/pkg/cogpack/plan"
)

type mockBuilder struct {
	called bool
}

func (m *mockBuilder) Build(ctx context.Context, p *plan.Plan, dir, tag string) error {
	m.called = true
	return nil
}

func TestEnabled(t *testing.T) {
	os.Setenv("COGPACK", "1")
	if !Enabled() {
		t.Fatal("expected Enabled() to be true when env set to 1")
	}
	os.Setenv("COGPACK", "false")
	if Enabled() {
		t.Fatal("expected Enabled() to be false when env set to false")
	}
}

func TestBuild_CallsBuilder(t *testing.T) {
	tmp := t.TempDir()
	// create minimal project file
	os.WriteFile(tmp+"/predict.py", []byte("# dummy"), 0644)

	mb := &mockBuilder{}
	_, err := Build(context.Background(), tmp, "test:latest", mb)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if !mb.called {
		t.Fatalf("builder.Build was not called")
	}
}

func TestExecutePlan_UsesProvidedPlan(t *testing.T) {
	plan := &plan.Plan{}
	mb := &mockBuilder{}
	if err := ExecutePlan(context.Background(), plan, "/tmp", "img:tag", mb); err != nil {
		t.Fatalf("ExecutePlan errored: %v", err)
	}
	if !mb.called {
		t.Fatal("ExecutePlan did not call builder")
	}
}
