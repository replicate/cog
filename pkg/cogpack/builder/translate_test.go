package builder

import (
	"context"
	"testing"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
)

func TestTranslatePlan_Basic(t *testing.T) {
	p := &plan.Plan{
		Platform:  plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: baseimg.BaseImage{Build: "ubuntu:22.04", Runtime: "ubuntu:22.04", Metadata: baseimg.BaseImageMetadata{Packages: map[string]baseimg.Package{}}},
	}

	stg, err := p.AddStage(plan.PhaseBase, "base", "base")
	if err != nil {
		t.Fatalf("AddStage: %v", err)
	}
	stg.Source = plan.Input{Image: "ubuntu:22.04"}
	stg.Operations = []plan.Op{plan.Exec{Command: "echo hello"}}

	_, _, err = translatePlan(context.Background(), p)
	if err != nil {
		t.Fatalf("translatePlan failed: %v", err)
	}
}
