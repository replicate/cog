package cli

import (
	"fmt"
	"math"
	"os"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/logrusorgru/aurora"
	"golang.org/x/term"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

const (
	maxDownloadWeightLen  = 40
	minDownloadWeightLen  = 8
	minDownloadFileLen    = 8
	minDownloadBarWidth   = 10
	maxDownloadBarWidth   = 24
	fallbackProgressWidth = 120
)

type weightDownloadProgress struct {
	mu      sync.Mutex
	out     *os.File
	isTTY   bool
	color   bool
	active  bool
	widthFn func() int
}

func newWeightDownloadProgress() *weightDownloadProgress {
	return &weightDownloadProgress{
		out:     os.Stderr,
		isTTY:   console.IsTTY(os.Stderr),
		color:   console.ConsoleInstance.Color,
		widthFn: stderrWidth,
	}
}

func writeWeightBuildProgress(pw *weightDownloadProgress, prog model.WeightBuildProgress) {
	name := prog.WeightName
	if name == "" {
		name = model.ShortDigest(prog.FileDigest)
	}

	if prog.Done && prog.Complete >= prog.Total {
		pw.WriteStatus(name, "done")
		return
	}
	pw.Write(name, prog.FilePath, prog.Complete, prog.Total)
}

func (p *weightDownloadProgress) Write(weight, file string, complete, total int64) {
	p.write(renderWeightDownloadLine(weight, file, complete, total, p.width(), p.color), false)
}

func (p *weightDownloadProgress) WriteStatus(weight, status string) {
	p.write(renderWeightDownloadStatusLine(weight, status, p.width(), p.color), true)
}

func (p *weightDownloadProgress) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active {
		_, _ = fmt.Fprintln(p.out)
		p.active = false
	}
}

func (p *weightDownloadProgress) write(line string, newline bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isTTY {
		if newline {
			_, _ = fmt.Fprintln(p.out, line)
		}
		return
	}

	_, _ = fmt.Fprintf(p.out, "\r\x1b[2K%s", line)
	p.active = true
	if newline {
		_, _ = fmt.Fprintln(p.out)
		p.active = false
	}
}

func (p *weightDownloadProgress) width() int {
	if p.widthFn == nil {
		return fallbackProgressWidth
	}
	width := p.widthFn()
	if width <= 0 {
		return fallbackProgressWidth
	}
	return width
}

func renderWeightDownloadLine(weight, file string, complete, total int64, width int, color bool) string {
	if width <= 0 {
		width = fallbackProgressWidth
	}
	weight = truncateProgressText(weight, maxDownloadWeightLen)
	file = path.Base(file)
	if file == "." || file == "/" {
		file = "downloading"
	}

	bytes := formatProgressBytes(complete, total)
	weight, file, barWidth, ok := fitDownloadBarLine(weight, file, bytes, width)
	if ok {
		return formatDownloadBarLine(weight, file, bytes, progressBar(complete, total, barWidth, color), color)
	}

	percent := formatProgressPercent(complete, total)
	weight, file, ok = fitDownloadPercentLine(weight, file, percent, bytes, width)
	if ok {
		return formatDownloadPercentLine(weight, file, percent, bytes, color)
	}

	line := formatDownloadMinimalLine(percent, bytes, color)
	if progressVisibleWidth(line) > width {
		return truncateProgressText(line, width)
	}
	return line
}

func renderWeightDownloadStatusLine(weight, status string, width int, color bool) string {
	if width <= 0 {
		width = fallbackProgressWidth
	}
	weight = truncateProgressText(weight, maxDownloadWeightLen)
	line := formatDownloadStatusLine(weight, status, color)
	if progressVisibleWidth(line) <= width {
		return line
	}

	excess := progressVisibleWidth(line) - width
	weight = truncateProgressText(weight, max(minDownloadWeightLen, progressRuneLen(weight)-excess))
	line = formatDownloadStatusLine(weight, status, color)
	if progressVisibleWidth(line) <= width {
		return line
	}
	return truncateProgressText(line, width)
}

func fitDownloadBarLine(weight, file, bytes string, width int) (string, string, int, bool) {
	for {
		available := width - progressVisibleWidth(formatDownloadBarPrefix(weight, file, bytes, false))
		if available >= minDownloadBarWidth {
			return weight, file, min(available, maxDownloadBarWidth), true
		}
		if progressRuneLen(file) > minDownloadFileLen {
			excess := minDownloadBarWidth - available
			file = truncateProgressText(file, max(minDownloadFileLen, progressRuneLen(file)-excess))
			continue
		}
		if progressRuneLen(weight) > minDownloadWeightLen {
			excess := minDownloadBarWidth - available
			weight = truncateProgressText(weight, max(minDownloadWeightLen, progressRuneLen(weight)-excess))
			continue
		}
		return weight, file, 0, false
	}
}

func fitDownloadPercentLine(weight, file, percent, bytes string, width int) (string, string, bool) {
	for {
		line := formatDownloadPercentLine(weight, file, percent, bytes, false)
		if progressVisibleWidth(line) <= width {
			return weight, file, true
		}
		excess := progressVisibleWidth(line) - width
		if progressRuneLen(file) > minDownloadFileLen {
			file = truncateProgressText(file, max(minDownloadFileLen, progressRuneLen(file)-excess))
			continue
		}
		if progressRuneLen(weight) > minDownloadWeightLen {
			weight = truncateProgressText(weight, max(minDownloadWeightLen, progressRuneLen(weight)-excess))
			continue
		}
		return weight, file, false
	}
}

func formatDownloadBarLine(weight, file, bytes, bar string, color bool) string {
	return formatDownloadBarPrefix(weight, file, bytes, color) + bar
}

func formatDownloadBarPrefix(weight, file, bytes string, color bool) string {
	return fmt.Sprintf("%s%s: %s  %s  ", cogProgressPrefix(color), weight, file, bytes)
}

func formatDownloadPercentLine(weight, file, percent, bytes string, color bool) string {
	return fmt.Sprintf("%s%s: %s  %s  %s", cogProgressPrefix(color), weight, file, percent, bytes)
}

func formatDownloadMinimalLine(percent, bytes string, color bool) string {
	return fmt.Sprintf("%s%s %s", cogProgressPrefix(color), percent, bytes)
}

func formatDownloadStatusLine(weight, status string, color bool) string {
	if status == "done" {
		return fmt.Sprintf("%s%s: %s done", cogProgressPrefix(color), weight, downloadDoneIcon(color))
	}
	return fmt.Sprintf("%s%s: %s", cogProgressPrefix(color), weight, status)
}

func cogProgressPrefix(color bool) string {
	gear := "⚙"
	if color {
		gear = aurora.Faint(gear).String()
	}
	return " " + gear + "  "
}

func downloadDoneIcon(color bool) string {
	icon := "✓"
	if color {
		return aurora.Green(icon).String()
	}
	return icon
}

func progressBar(complete, total int64, width int, color bool) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	complete = min(max(complete, 0), total)
	filled := int(math.Round(float64(complete) / float64(total) * float64(width)))
	filled = min(max(filled, 0), width)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	if color && filled > 0 {
		return aurora.Yellow(strings.Repeat("█", filled)).String() + aurora.Faint(strings.Repeat("░", width-filled)).String()
	}
	if color {
		return aurora.Faint(bar).String()
	}
	return bar
}

func formatProgressBytes(complete, total int64) string {
	return fmt.Sprintf("%s/%s", formatSize(complete), formatSize(total))
}

func formatProgressPercent(complete, total int64) string {
	if total <= 0 {
		return "0%"
	}
	complete = min(max(complete, 0), total)
	return fmt.Sprintf("%d%%", int(math.Round(float64(complete)/float64(total)*100)))
}

func truncateProgressText(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	if maxLen <= 2 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-2]) + ".."
}

func progressRuneLen(text string) int {
	return len([]rune(text))
}

func progressVisibleWidth(text string) int {
	width := 0
	inEscape := false
	for _, r := range text {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width += utf8.RuneLen(r)
		if r > 127 {
			width = width - utf8.RuneLen(r) + 1
		}
	}
	return width
}

func stderrWidth() int {
	fd := os.Stderr.Fd()
	if fd > math.MaxInt {
		return 0
	}
	width, _, err := term.GetSize(int(fd)) //nolint:gosec // bounded above
	if err != nil || width <= 0 {
		return 0
	}
	return width
}
