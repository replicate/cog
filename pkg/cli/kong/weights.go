package kong

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

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// WeightsCmd implements `cog weights` (hidden) with subcommands.
type WeightsCmd struct {
	Build WeightsBuildCmd `cmd:"" help:"Generate weights.lock from weight sources in cog.yaml"`
	Push  WeightsPushCmd  `cmd:"" help:"Push weights to a registry"`
}

// WeightsBuildCmd implements `cog weights build`.
type WeightsBuildCmd struct {
	Dest       string `help:"Container path prefix for weights" default:"/cache/"`
	ConfigFile string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
}

func (c *WeightsBuildCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	src, err := model.NewSource(c.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if len(src.Config.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", c.ConfigFile)
	}

	console.Infof("Processing %d weight source(s)...", len(src.Config.Weights))

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: c.Dest,
	})

	lock, err := gen.Generate(ctx, src.ProjectDir, src.Config.Weights)
	if err != nil {
		return fmt.Errorf("failed to generate weights lock: %w", err)
	}

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	if err := lock.Save(lockPath); err != nil {
		return fmt.Errorf("failed to save weights.lock: %w", err)
	}

	var totalSize int64
	for _, f := range lock.Files {
		totalSize += f.Size
		console.Infof("  %s -> %s (%s)", f.Name, f.Dest, formatSize(f.Size))
	}

	console.Infof("\nGenerated %s with %d file(s) (%s total)",
		model.WeightsLockFilename, len(lock.Files), formatSize(totalSize))

	return nil
}

// WeightsPushCmd implements `cog weights push [IMAGE]`.
type WeightsPushCmd struct {
	Image      string `arg:"" optional:"" help:"Image name to determine registry"`
	ConfigFile string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
}

func (c *WeightsPushCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	src, err := model.NewSource(c.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	imageName := src.Config.Image
	if c.Image != "" {
		imageName = c.Image
	}
	if imageName == "" {
		return fmt.Errorf("To push weights, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog weights push registry.example.com/your-username/model-name'")
	}

	ref, err := name.ParseReference(imageName, name.Insecure)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", imageName, err)
	}
	repo := ref.Context().Name()

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return fmt.Errorf("weights.lock not found; run 'cog weights build' first")
	}

	lock, err := model.LoadWeightsLock(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load weights.lock: %w", err)
	}

	if len(lock.Files) == 0 {
		return fmt.Errorf("weights.lock contains no files")
	}

	if len(src.Config.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", c.ConfigFile)
	}

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: "/cache/",
	})

	_, filePaths, err := gen.GenerateWithFilePaths(ctx, src.ProjectDir, src.Config.Weights)
	if err != nil {
		return fmt.Errorf("failed to resolve weight files: %w", err)
	}

	console.Infof("Pushing %d weight file(s) to %s...", len(lock.Files), repo)

	regClient := registry.NewRegistryClient()
	pusher := model.NewWeightsPusher(regClient)
	tracker := newProgressTracker(lock.Files)

	displayCtx, cancelDisplay := context.WithCancel(ctx)
	displayDone := make(chan struct{})

	go func() {
		defer close(displayDone)
		tracker.displayLoop(displayCtx)
	}()

	progressFn := func(p model.WeightsPushProgress) {
		if p.Done {
			if p.Err != nil {
				tracker.setError(p.Name, p.Err)
			} else {
				tracker.setComplete(p.Name)
			}
			return
		}
		tracker.clearRetrying(p.Name)
		if p.Total > 0 {
			tracker.setTotal(p.Name, p.Total)
		}
		tracker.update(p.Name, p.Complete, p.Total)
	}

	retryFn := func(event model.WeightsRetryEvent) bool {
		tracker.setRetrying(event.Name, event.Attempt, event.MaxAttempts, event.NextRetryIn, event.Err)
		if !tracker.isTTY {
			console.Warnf("  %s: retrying (%d/%d) in %s: %v",
				event.Name, event.Attempt, event.MaxAttempts,
				event.NextRetryIn.Round(time.Second), event.Err)
		}
		return true
	}

	result, err := pusher.Push(ctx, model.WeightsPushOptions{
		Repo:       repo,
		Lock:       lock,
		FilePaths:  filePaths,
		ProgressFn: progressFn,
		RetryFn:    retryFn,
	})

	cancelDisplay()
	<-displayDone
	tracker.printFinalStatus()

	if err != nil {
		return fmt.Errorf("failed to push weights: %w", err)
	}

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

// --- Progress tracker ---

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

type progressTracker struct {
	mu       sync.Mutex
	files    []fileProgress
	fileMap  map[string]int
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
	retryInfo string
}

func newProgressTracker(files []model.WeightFile) *progressTracker {
	pt := &progressTracker{
		files:   make([]fileProgress, len(files)),
		fileMap: make(map[string]int, len(files)),
		isTTY:   term.IsTerminal(os.Stderr.Fd()),
	}
	for i, f := range files {
		pt.files[i] = fileProgress{name: f.Name, total: f.Size}
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
		return
	}
	if !pt.lastDraw.IsZero() {
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
	}
	for _, f := range pt.files {
		fmt.Fprintf(os.Stderr, "\033[2K%s\n", pt.formatLine(f))
	}
	pt.lastDraw = time.Now()
}

func (pt *progressTracker) formatLine(f fileProgress) string {
	n := f.name
	if len(n) > 30 {
		n = "..." + n[len(n)-27:]
	}
	if f.err != nil {
		return fmt.Sprintf("  %-30s  FAILED", n)
	}
	if f.done {
		return fmt.Sprintf("  %-30s  %s  done", n, formatSize(f.total))
	}
	if f.retrying && f.retryInfo != "" {
		return fmt.Sprintf("  %-30s  %s", n, f.retryInfo)
	}
	if f.total == 0 {
		return fmt.Sprintf("  %-30s  waiting...", n)
	}
	pct := float64(f.complete) / float64(f.total) * 100
	if pct > 100 {
		pct = 100
	}
	barWidth := 20
	filled := min(int(pct/100*float64(barWidth)), barWidth)
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)
	return fmt.Sprintf("  %-30s  [%s] %5.1f%%  %s / %s",
		n, bar, pct, formatSize(f.complete), formatSize(f.total))
}

func (pt *progressTracker) printFinalStatus() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if pt.isTTY && !pt.lastDraw.IsZero() {
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
		for range pt.files {
			fmt.Fprintf(os.Stderr, "\033[2K\n")
		}
		fmt.Fprintf(os.Stderr, "\033[%dA", len(pt.files))
	}
	sorted := make([]fileProgress, len(pt.files))
	copy(sorted, pt.files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	for _, f := range sorted {
		if f.err != nil {
			console.Warnf("  %s: FAILED - %v", f.name, f.err)
		} else {
			console.Infof("  %s: %s", f.name, formatSize(f.total))
		}
	}
}
