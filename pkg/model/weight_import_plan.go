package model

import (
	"context"
	"fmt"
	"slices"

	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/lockfile"
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
	return p.Resolved.mergedFiles
}

// TotalSize returns the sum of filtered file sizes.
func (p *WeightImportPlan) TotalSize() int64 {
	var total int64
	for _, f := range p.Resolved.mergedFiles {
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
	included := make(map[string]bool, len(p.Resolved.mergedFiles))
	for _, f := range p.Resolved.mergedFiles {
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

	// Keep the unfiltered set if any source has patterns active.
	hasPatterns := false
	for _, src := range ws.Sources {
		if len(src.Include) > 0 || len(src.Exclude) > 0 {
			hasPatterns = true
			break
		}
	}
	if hasPatterns {
		plan.UnfilteredFiles = resolved.unfilteredFiles()
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

	if fingerprintsChanged(existing, resolved) {
		plan.Status = PlanStatusUpstreamChanged
		plan.Changes = describeFingerprintDrift(existing, resolved)
		return plan, nil
	}

	plan.Status = PlanStatusUnchanged
	return plan, nil
}

// describeSpecDrift returns human-readable descriptions of what differs
// between the config spec and the lockfile spec.
func describeSpecDrift(cfg, lock *WeightSpec) []string {
	var changes []string
	if cfg.Target != lock.Target {
		changes = append(changes, fmt.Sprintf("target: %q → %q", lock.Target, cfg.Target))
	}
	if len(cfg.Sources) != len(lock.Sources) {
		changes = append(changes, fmt.Sprintf("source count: %d → %d", len(lock.Sources), len(cfg.Sources)))
	} else {
		for i := range cfg.Sources {
			cs, ls := cfg.Sources[i], lock.Sources[i]
			prefix := ""
			if len(cfg.Sources) > 1 {
				prefix = fmt.Sprintf("source[%d].", i)
			}
			if cs.URI != ls.URI {
				changes = append(changes, fmt.Sprintf("%suri: %q → %q", prefix, ls.URI, cs.URI))
			}
			if !slices.Equal(cs.Include, ls.Include) {
				changes = append(changes, fmt.Sprintf("%sinclude: %v → %v", prefix, ls.Include, cs.Include))
			}
			if !slices.Equal(cs.Exclude, ls.Exclude) {
				changes = append(changes, fmt.Sprintf("%sexclude: %v → %v", prefix, ls.Exclude, cs.Exclude))
			}
		}
	}
	return changes
}

// fingerprintsChanged reports whether any source's fingerprint differs
// between the lockfile entry and the resolved inventory.
func fingerprintsChanged(existing *lockfile.WeightLockEntry, resolved *resolvedInventory) bool {
	if len(existing.Sources) != len(resolved.perSource) {
		return true
	}
	for i := range existing.Sources {
		if existing.Sources[i].Fingerprint != resolved.perSource[i].full.Fingerprint {
			return true
		}
	}
	return false
}

// describeFingerprintDrift returns human-readable descriptions of which
// source fingerprints changed.
func describeFingerprintDrift(existing *lockfile.WeightLockEntry, resolved *resolvedInventory) []string {
	var changes []string
	n := min(len(existing.Sources), len(resolved.perSource))
	for i := range n {
		if existing.Sources[i].Fingerprint != resolved.perSource[i].full.Fingerprint {
			prefix := ""
			if len(existing.Sources) > 1 {
				prefix = fmt.Sprintf("source[%d] ", i)
			}
			changes = append(changes, fmt.Sprintf("%sfingerprint: %s → %s",
				prefix, existing.Sources[i].Fingerprint, resolved.perSource[i].full.Fingerprint))
		}
	}
	if len(existing.Sources) != len(resolved.perSource) {
		changes = append(changes, fmt.Sprintf("source count: %d → %d",
			len(existing.Sources), len(resolved.perSource)))
	}
	return changes
}
