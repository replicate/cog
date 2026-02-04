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

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

var weightsDest string

func newWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "weights",
		Short:  "Manage model weights",
		Long:   "Commands for managing model weight files.",
		Hidden: true,
	}

	cmd.AddCommand(newWeightsBuildCommand())
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

	cmd.Flags().StringVar(&weightsDest, "dest", "/cache/", "Container path prefix for weights")
	addConfigFlag(cmd)
	return cmd
}

func weightsBuildCommand(cmd *cobra.Command, args []string) error {
	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var (
		cfg        = src.Config
		projectDir = src.ProjectDir
	)

	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	console.Infof("Processing %d weight source(s)...", len(cfg.Weights))

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: weightsDest,
	})

	lock, err := gen.Generate(projectDir, cfg.Weights)
	if err != nil {
		return fmt.Errorf("failed to generate weights lock: %w", err)
	}

	lockPath := filepath.Join(projectDir, model.WeightsLockFilename)
	if err := lock.Save(lockPath); err != nil {
		return fmt.Errorf("failed to save weights.lock: %w", err)
	}

	// Print summary
	var totalSize int64
	for _, f := range lock.Files {
		totalSize += f.Size
		console.Infof("  %s -> %s (%s)", f.Name, f.Dest, formatSize(f.Size))
	}

	console.Infof("\nGenerated %s with %d file(s) (%s total)",
		model.WeightsLockFilename, len(lock.Files), formatSize(totalSize))

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

	var (
		cfg        = src.Config
		projectDir = src.ProjectDir
	)

	// Determine image name
	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}
	if imageName == "" {
		return fmt.Errorf("To push weights, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog weights push registry.example.com/your-username/model-name'")
	}

	// Parse repository from image name (strip tag if present)
	ref, err := name.ParseReference(imageName, name.Insecure)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", imageName, err)
	}
	repo := ref.Context().Name()

	// Check weights.lock exists
	lockPath := filepath.Join(projectDir, model.WeightsLockFilename)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return fmt.Errorf("weights.lock not found; run 'cog weights build' first")
	}

	// Load weights.lock
	lock, err := model.LoadWeightsLock(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load weights.lock: %w", err)
	}

	if len(lock.Files) == 0 {
		return fmt.Errorf("weights.lock contains no files")
	}

	// Generate filePaths map from weights config
	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: weightsDest,
	})

	_, filePaths, err := gen.GenerateWithFilePaths(projectDir, cfg.Weights)
	if err != nil {
		return fmt.Errorf("failed to resolve weight files: %w", err)
	}

	// Push weight files as layers concurrently with progress tracking
	console.Infof("Pushing %d weight file(s) to %s...", len(lock.Files), repo)

	var (
		regClient = registry.NewRegistryClient()
		pusher    = model.NewWeightsPusher(regClient)
		tracker   = newProgressTracker(lock.Files)

		// Start progress display in background
		displayCtx, cancelDisplay = context.WithCancel(ctx)
		displayDone               = make(chan struct{})
	)

	go func() {
		defer close(displayDone)
		tracker.displayLoop(displayCtx)
	}()

	// Set up progress callback to update the tracker
	progressFn := func(p model.WeightsPushProgress) {
		if p.Done {
			if p.Err != nil {
				tracker.setError(p.Name, p.Err)
			} else {
				tracker.setComplete(p.Name)
			}
			return
		}

		if p.Total > 0 {
			tracker.setTotal(p.Name, p.Total)
		}
		tracker.update(p.Name, p.Complete, p.Total)
	}

	// Push weights using the model layer
	result, err := pusher.Push(ctx, model.WeightsPushOptions{
		Repo:       repo,
		Lock:       lock,
		FilePaths:  filePaths,
		ProgressFn: progressFn,
	})

	// Stop progress display
	cancelDisplay()
	<-displayDone

	// Print final status for each file
	tracker.printFinalStatus()

	if err != nil {
		return fmt.Errorf("failed to push weights: %w", err)
	}

	// Count errors from results
	var errorCount int
	for _, f := range result.Files {
		if f.Err != nil {
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("failed to push %d/%d weight files", errorCount, len(lock.Files))
	}

	console.Infof("\nPushed %d weight blobs to %s", len(lock.Files), repo)
	console.Infof("Total: %s", formatSize(result.TotalSize))

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
	name     string
	complete int64
	total    int64
	done     bool
	err      error
}

func newProgressTracker(files []model.WeightFile) *progressTracker {
	pt := &progressTracker{
		files:   make([]fileProgress, len(files)),
		fileMap: make(map[string]int, len(files)),
		isTTY:   term.IsTerminal(os.Stderr.Fd()),
	}
	for i, f := range files {
		pt.files[i] = fileProgress{
			name:  f.Name,
			total: f.Size, // Initial estimate from lock file
		}
		pt.fileMap[f.Name] = i
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

func (pt *progressTracker) printFinalStatus() {
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
		} else {
			console.Infof("  %s: %s", f.name, formatSize(f.total))
		}
	}
}
