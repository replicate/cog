package model

import (
	"context"
	"fmt"
	"slices"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// WeightImportPlanStatus describes what would happen to a weight on import.
type WeightImportPlanStatus string

const (
	PlanStatusNew             WeightImportPlanStatus = "new"
	PlanStatusUnchanged       WeightImportPlanStatus = "unchanged"
	PlanStatusConfigChanged   WeightImportPlanStatus = "config-changed"
	PlanStatusUpstreamChanged WeightImportPlanStatus = "upstream-changed"
)

// WeightImportPlan is the result of planning one weight's import without
// executing it. It contains everything needed to show the user what
// would happen and to pass pre-computed inventory into Build.
type WeightImportPlan struct {
	Spec *WeightSpec

	Status  WeightImportPlanStatus
	Changes []string // human-readable list of what changed

	// Resolved is the pre-computed inventory from planning. Build can
	// reuse this to avoid re-walking the source.
	Resolved *resolvedInventory

	// UnfilteredFiles is populated when include/exclude patterns are
	// active, so the caller can show what was excluded.
	UnfilteredFiles []weightsource.InventoryFile
}

// FilteredFiles returns the filtered inventory files.
func (p *WeightImportPlan) FilteredFiles() []weightsource.InventoryFile {
	return p.Resolved.filtered.Files
}

// TotalSize returns the sum of filtered file sizes.
func (p *WeightImportPlan) TotalSize() int64 {
	var total int64
	for _, f := range p.Resolved.filtered.Files {
		total += f.Size
	}
	return total
}

// ExcludedFiles returns files that were in the unfiltered inventory but
// not in the filtered set.
func (p *WeightImportPlan) ExcludedFiles() []weightsource.InventoryFile {
	if len(p.UnfilteredFiles) == 0 {
		return nil
	}
	included := make(map[string]bool, len(p.Resolved.filtered.Files))
	for _, f := range p.Resolved.filtered.Files {
		included[f.Path] = true
	}
	var excluded []weightsource.InventoryFile
	for _, f := range p.UnfilteredFiles {
		if !included[f.Path] {
			excluded = append(excluded, f)
		}
	}
	return excluded
}

// PlanImport runs the inventory + filter steps for one weight without
// ingressing, packing, or pushing. It compares the result against the
// existing lockfile to determine what would change on a real import.
func (b *WeightBuilder) PlanImport(ctx context.Context, ws *WeightSpec) (*WeightImportPlan, error) {
	resolved, err := b.resolveInventory(ctx, ws)
	if err != nil {
		return nil, err
	}

	plan := &WeightImportPlan{
		Spec:     ws,
		Resolved: resolved,
	}

	// Keep the unfiltered set if patterns are active.
	if len(ws.Include) > 0 || len(ws.Exclude) > 0 {
		plan.UnfilteredFiles = resolved.full.Files
	}

	// Compare against lockfile.
	lock, err := loadLockfileOrEmpty(b.lockPath)
	if err != nil {
		return nil, err
	}

	existing := lock.FindWeight(ws.Name())
	if existing == nil {
		plan.Status = PlanStatusNew
		return plan, nil
	}

	lockSpec := WeightSpecFromLock(*existing)
	if !ws.Equal(lockSpec) {
		plan.Status = PlanStatusConfigChanged
		plan.Changes = describeSpecDrift(ws, lockSpec)
		return plan, nil
	}

	if existing.Source.Fingerprint != resolved.full.Fingerprint {
		plan.Status = PlanStatusUpstreamChanged
		plan.Changes = []string{fmt.Sprintf("fingerprint: %s → %s",
			existing.Source.Fingerprint, resolved.full.Fingerprint)}
		return plan, nil
	}

	plan.Status = PlanStatusUnchanged
	return plan, nil
}

// describeSpecDrift returns human-readable descriptions of what differs
// between the config spec and the lockfile spec.
func describeSpecDrift(config, lock *WeightSpec) []string {
	var changes []string
	if config.URI != lock.URI {
		changes = append(changes, fmt.Sprintf("uri: %q → %q", lock.URI, config.URI))
	}
	if config.Target != lock.Target {
		changes = append(changes, fmt.Sprintf("target: %q → %q", lock.Target, config.Target))
	}
	if !slices.Equal(config.Include, lock.Include) {
		changes = append(changes, fmt.Sprintf("include: %v → %v", lock.Include, config.Include))
	}
	if !slices.Equal(config.Exclude, lock.Exclude) {
		changes = append(changes, fmt.Sprintf("exclude: %v → %v", lock.Exclude, config.Exclude))
	}
	return changes
}
