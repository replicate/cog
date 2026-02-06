package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/moby/term"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

func newWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "weights",
		Short:  "Manage model weights",
		Long:   "Commands for managing model weight files.",
		Hidden: true,
	}

	cmd.AddCommand(newWeightsBuildCommand())
	cmd.AddCommand(newWeightsInspectCommand())
	cmd.AddCommand(newWeightsPushCommand())
	return cmd
}

func newWeightsBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Generate weights.lock from weight sources in cog.yaml",
		Long: `Reads the weights section from cog.yaml, processes each weight source,
and generates a weights.lock file containing metadata (digests, sizes) for each file.`,
		Args: cobra.NoArgs,
		RunE: weightsBuildCommand,
	}

	addConfigFlag(cmd)
	return cmd
}

func weightsBuildCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if len(src.Config.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	// Extract weight specs from the source
	var weightSpecs []*model.WeightSpec
	for _, spec := range src.ArtifactSpecs() {
		if ws, ok := spec.(*model.WeightSpec); ok {
			weightSpecs = append(weightSpecs, ws)
		}
	}

	console.Infof("Processing %d weight source(s)...", len(weightSpecs))

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, global.Version, lockPath)

	// Build each weight artifact (hashes file, updates lockfile)
	var totalSize int64
	for _, ws := range weightSpecs {
		artifact, buildErr := builder.Build(ctx, ws)
		if buildErr != nil {
			return fmt.Errorf("failed to build weight %q: %w", ws.Name(), buildErr)
		}

		wa, ok := artifact.(*model.WeightArtifact)
		if !ok {
			return fmt.Errorf("unexpected artifact type %T for weight %q", artifact, ws.Name())
		}
		size := wa.Descriptor().Size
		totalSize += size
		console.Infof("  %s -> %s (%s)", wa.Name(), wa.Target, formatSize(size))
	}

	console.Infof("\nGenerated %s with %d file(s) (%s total)",
		model.WeightsLockFilename, len(weightSpecs), formatSize(totalSize))

	return nil
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func newWeightsPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [IMAGE]",
		Short: "Push weights to a registry",
		Long: `Reads weights.lock and pushes weight files as an OCI artifact to a registry.

The registry is determined from the image name, which can be:
- Specified as an argument: cog weights push registry.example.com/user/model
- Set in cog.yaml as the 'image' field`,
		Args: cobra.MaximumNArgs(1),
		RunE: weightsPushCommand,
	}

	addConfigFlag(cmd)
	return cmd
}

func weightsPushCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	cfg := src.Config

	// Determine image name
	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}
	if imageName == "" {
		return fmt.Errorf("To push weights, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog weights push registry.example.com/your-username/model-name'")
	}

	// Parse as repository only — reject tags/digests since weight tags are auto-generated.
	parsedRepo, err := name.NewRepository(imageName, name.Insecure)
	if err != nil {
		// NewRepository fails for inputs with :tag or @digest — check if it's a valid ref
		if ref, refErr := name.ParseReference(imageName, name.Insecure); refErr == nil {
			return fmt.Errorf("image reference %q includes a tag or digest — provide only the repository (e.g., %q)", imageName, ref.Context().Name())
		}
		return fmt.Errorf("invalid repository %q: %w", imageName, err)
	}
	repo := parsedRepo.Name()

	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	// Build weight artifacts (reads lockfile as cache, hashes files)
	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, global.Version, lockPath)

	var artifacts []*model.WeightArtifact
	for _, spec := range src.ArtifactSpecs() {
		ws, ok := spec.(*model.WeightSpec)
		if !ok {
			continue
		}
		artifact, buildErr := builder.Build(ctx, ws)
		if buildErr != nil {
			return fmt.Errorf("failed to build weight %q: %w", ws.Name(), buildErr)
		}
		wa, ok := artifact.(*model.WeightArtifact)
		if !ok {
			return fmt.Errorf("unexpected artifact type %T for weight %q", artifact, ws.Name())
		}
		artifacts = append(artifacts, wa)
	}

	if len(artifacts) == 0 {
		return fmt.Errorf("no weight artifacts to push")
	}

	console.Infof("Pushing %d weight file(s) to %s...", len(artifacts), repo)

	var (
		regClient                 = registry.NewRegistryClient()
		pusher                    = model.NewWeightPusher(regClient)
		tracker                   = newProgressTracker(artifacts)
		displayCtx, cancelDisplay = context.WithCancel(ctx)
		displayDone               = make(chan struct{})
	)

	go func() {
		defer close(displayDone)
		tracker.displayLoop(displayCtx)
	}()

	// Push each weight artifact concurrently
	type pushResult struct {
		name string
		ref  string
		size int64
		err  error
	}

	const maxConcurrency = 4
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan pushResult, len(artifacts))
	var wg sync.WaitGroup

	for _, wa := range artifacts {
		wg.Add(1)
		go func(wa *model.WeightArtifact) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			artName := wa.Name()

			result, pushErr := pusher.Push(ctx, repo, wa, model.WeightPushOptions{
				ProgressFn: func(p model.WeightPushProgress) {
					tracker.clearRetrying(artName)
					if p.Total > 0 {
						tracker.setTotal(artName, p.Total)
					}
					tracker.update(artName, p.Complete, p.Total)
				},
				RetryFn: func(event model.WeightRetryEvent) bool {
					tracker.setRetrying(event.Name, event.Attempt, event.MaxAttempts, event.NextRetryIn, event.Err)
					if !tracker.isTTY {
						console.Warnf("  %s: retrying (%d/%d) in %s: %v",
							event.Name, event.Attempt, event.MaxAttempts,
							event.NextRetryIn.Round(time.Second), event.Err)
					}
					return true
				},
			})

			if pushErr != nil {
				tracker.setError(artName, pushErr)
				results <- pushResult{name: artName, err: pushErr}
			} else {
				tracker.setComplete(artName)
				results <- pushResult{name: artName, ref: result.Ref, size: wa.Descriptor().Size}
			}
		}(wa)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var totalSize int64
	var errorCount int
	refs := make(map[string]string) // name -> ref
	for r := range results {
		if r.err != nil {
			errorCount++
		} else {
			totalSize += r.size
			refs[r.name] = r.ref
		}
	}

	// Stop progress display
	cancelDisplay()
	<-displayDone

	tracker.printFinalStatus(refs)

	if errorCount > 0 {
		return fmt.Errorf("failed to push %d/%d weight files", errorCount, len(artifacts))
	}

	console.Infof("\nPushed %d weight artifact(s) to %s", len(artifacts), repo)
	console.Infof("Total: %s", formatSize(totalSize))

	return nil
}

// progressTracker tracks upload progress for multiple concurrent files
type progressTracker struct {
	mu       sync.Mutex
	files    []fileProgress
	fileMap  map[string]int // name -> index in files slice
	isTTY    bool
	lastDraw time.Time
}

type fileProgress struct {
	name      string
	complete  int64
	total     int64
	done      bool
	err       error
	retrying  bool
	retryInfo string // Human-readable retry status
}

func newProgressTracker(artifacts []*model.WeightArtifact) *progressTracker {
	pt := &progressTracker{
		files:   make([]fileProgress, len(artifacts)),
		fileMap: make(map[string]int, len(artifacts)),
		isTTY:   term.IsTerminal(os.Stderr.Fd()),
	}
	for i, a := range artifacts {
		pt.files[i] = fileProgress{
			name:  a.Name(),
			total: a.Descriptor().Size, // Initial estimate from build
		}
		pt.fileMap[a.Name()] = i
	}
	return pt
}

func (pt *progressTracker) setTotal(name string, total int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].total = total
	}
}

func (pt *progressTracker) update(name string, complete, total int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].complete = complete
		if total > 0 {
			pt.files[idx].total = total
		}
	}
}

func (pt *progressTracker) setComplete(name string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].done = true
		pt.files[idx].complete = pt.files[idx].total
	}
}

func (pt *progressTracker) setError(name string, err error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].done = true
		pt.files[idx].err = err
		pt.files[idx].retrying = false
		pt.files[idx].retryInfo = ""
	}
}

func (pt *progressTracker) setRetrying(name string, attempt, maxAttempts int, nextRetryIn time.Duration, err error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].retrying = true
		pt.files[idx].retryInfo = fmt.Sprintf("retry %d/%d in %s", attempt, maxAttempts, nextRetryIn.Round(time.Second))
		// Reset progress for retry attempt
		pt.files[idx].complete = 0
	}
}

func (pt *progressTracker) clearRetrying(name string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if idx, ok := pt.fileMap[name]; ok {
		pt.files[idx].retrying = false
		pt.files[idx].retryInfo = ""
	}
}

func (pt *progressTracker) displayLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pt.draw()
		}
	}
}

func (pt *progressTracker) draw() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if !pt.isTTY {
		// In non-TTY mode, don't redraw - we'll print final status only
		return
	}

	// Move cursor up to overwrite previous output
	if !pt.lastDraw.IsZero() {
		// Move cursor up by the number of files
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
	}

	for _, f := range pt.files {
		line := pt.formatProgressLine(f)
		// Clear line and print
		fmt.Fprintf(os.Stderr, "\033[2K%s\n", line)
	}

	pt.lastDraw = time.Now()
}

func (pt *progressTracker) formatProgressLine(f fileProgress) string {
	// Truncate name if too long
	name := f.name
	if len(name) > 30 {
		name = "..." + name[len(name)-27:]
	}

	if f.err != nil {
		return fmt.Sprintf("  %-30s  FAILED", name)
	}

	if f.done {
		return fmt.Sprintf("  %-30s  %s  done", name, formatSize(f.total))
	}

	// Show retry status if retrying
	if f.retrying && f.retryInfo != "" {
		return fmt.Sprintf("  %-30s  %s", name, f.retryInfo)
	}

	if f.total == 0 {
		return fmt.Sprintf("  %-30s  waiting...", name)
	}

	percent := float64(f.complete) / float64(f.total) * 100
	if percent > 100 {
		percent = 100
	}

	// Create a simple progress bar
	barWidth := 20
	filled := min(int(percent/100*float64(barWidth)), barWidth)
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)

	return fmt.Sprintf("  %-30s  [%s] %5.1f%%  %s / %s",
		name, bar, percent, formatSize(f.complete), formatSize(f.total))
}

func (pt *progressTracker) printFinalStatus(refs map[string]string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.isTTY && !pt.lastDraw.IsZero() {
		// Clear the progress lines we drew
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
		for range pt.files {
			fmt.Fprintf(os.Stderr, "\033[2K\n")
		}
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
	}

	// Sort files by name for consistent output
	sortedFiles := make([]fileProgress, len(pt.files))
	copy(sortedFiles, pt.files)
	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].name < sortedFiles[j].name
	})

	for _, f := range sortedFiles {
		if f.err != nil {
			console.Warnf("  %s: FAILED - %v", f.name, f.err)
		} else if ref, ok := refs[f.name]; ok {
			console.Infof("  %s: %s", f.name, ref)
		} else {
			console.Infof("  %s: %s", f.name, formatSize(f.total))
		}
	}
}
