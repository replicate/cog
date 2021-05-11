package terminal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

type nonInteractiveUI struct {
	mu sync.Mutex
}

func NonInteractiveUI(ctx context.Context) UI {
	result := &nonInteractiveUI{}
	return result
}

func (ui *nonInteractiveUI) Input(input *Input) (string, error) {
	return "", ErrNonInteractive
}

// Interactive implements UI
func (ui *nonInteractiveUI) Interactive() bool {
	return false
}

// Output implements UI
func (ui *nonInteractiveUI) Output(msg string, raw ...interface{}) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	msg, style, w := Interpret(msg, raw...)

	switch style {
	case HeaderStyle:
		msg = "\n» " + msg
	case ErrorStyle, ErrorBoldStyle:
		lines := strings.Split(msg, "\n")
		if len(lines) > 0 {
			fmt.Fprintln(w, "! "+lines[0])
			for _, line := range lines[1:] {
				fmt.Fprintln(w, "  "+line)
			}
		}

		return

	case WarningStyle, WarningBoldStyle:
		msg = "warning: " + msg

	case SuccessStyle, SuccessBoldStyle:

	case InfoStyle:
		lines := strings.Split(msg, "\n")
		for i, line := range lines {
			lines[i] = colorInfo.Sprintf("  %s", line)
		}

		msg = strings.Join(lines, "\n")
	}

	fmt.Fprintln(w, msg)
}

// NamedValues implements UI
func (ui *nonInteractiveUI) NamedValues(rows []NamedValue, opts ...Option) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	cfg := &config{Writer: color.Output}
	for _, opt := range opts {
		opt(cfg)
	}

	var buf bytes.Buffer
	tr := tabwriter.NewWriter(&buf, 1, 8, 0, ' ', tabwriter.AlignRight)
	for _, row := range rows {
		switch v := row.Value.(type) {
		case int, uint, int8, uint8, int16, uint16, int32, uint32, int64, uint64:
			fmt.Fprintf(tr, "  %s: \t%d\n", row.Name, row.Value)
		case float32, float64:
			fmt.Fprintf(tr, "  %s: \t%f\n", row.Name, row.Value)
		case bool:
			fmt.Fprintf(tr, "  %s: \t%v\n", row.Name, row.Value)
		case string:
			if v == "" {
				continue
			}
			fmt.Fprintf(tr, "  %s: \t%s\n", row.Name, row.Value)
		default:
			fmt.Fprintf(tr, "  %s: \t%s\n", row.Name, row.Value)
		}
	}
	tr.Flush()

	fmt.Fprintln(cfg.Writer, buf.String())
}

// OutputWriters implements UI
func (ui *nonInteractiveUI) OutputWriters() (io.Writer, io.Writer, error) {
	return os.Stdout, os.Stderr, nil
}

// Status implements UI
func (ui *nonInteractiveUI) Status() Status {
	return &nonInteractiveStatus{mu: &ui.mu}
}

func (ui *nonInteractiveUI) StepGroup() StepGroup {
	return &nonInteractiveStepGroup{mu: &ui.mu}
}

// Table implements UI
func (ui *nonInteractiveUI) Table(tbl *Table, opts ...Option) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	// Build our config and set our options
	cfg := &config{Writer: color.Output}
	for _, opt := range opts {
		opt(cfg)
	}

	table := tablewriter.NewWriter(cfg.Writer)
	table.SetHeader(tbl.Headers)
	table.SetBorder(false)
	table.SetAutoWrapText(false)

	for _, row := range tbl.Rows {
		colors := make([]tablewriter.Colors, len(row))
		entries := make([]string, len(row))

		for i, ent := range row {
			entries[i] = ent.Value

			color, ok := colorMapping[ent.Color]
			if ok {
				colors[i] = tablewriter.Colors{color}
			}
		}

		table.Rich(entries, colors)
	}

	table.Render()
}

// HorizontalRule implements UI
func (ui *nonInteractiveUI) HorizontalRule() {
	fmt.Fprintln(color.Output, strings.Repeat("─", 10))
}

// ProcessHandover implements UI
func (ui *nonInteractiveUI) ProcessHandover(command string) {
	fmt.Fprintf(color.Output, "%s %s\n", strings.Repeat("─", 10), command)
}

// Close implements UI
func (ui *nonInteractiveUI) Close() error {
	return nil
}

type nonInteractiveStatus struct {
	mu *sync.Mutex
}

func (s *nonInteractiveStatus) Update(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(color.Output, msg)
}

func (s *nonInteractiveStatus) Step(status, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(color.Output, "%s: %s\n", textStatus[status], msg)
}

func (s *nonInteractiveStatus) Close() error {
	return nil
}

type nonInteractiveStepGroup struct {
	mu     *sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

// Start a step in the output
func (f *nonInteractiveStepGroup) Add(str string, args ...interface{}) Step {
	// Build our step
	step := &nonInteractiveStep{mu: f.mu}

	// Setup initial status
	step.Update(str, args...)

	// Grab the lock now so we can update our fields
	f.mu.Lock()
	defer f.mu.Unlock()

	// If we're closed we don't add this step to our waitgroup or document.
	// We still create a step and return a non-nil step so downstreams don't
	// crash.
	if !f.closed {
		// Add since we have a step
		step.wg = &f.wg
		f.wg.Add(1)
	}

	return step
}

func (f *nonInteractiveStepGroup) Wait() {
	f.mu.Lock()
	f.closed = true
	wg := &f.wg
	f.mu.Unlock()

	wg.Wait()
}

type nonInteractiveStep struct {
	mu         *sync.Mutex
	wg         *sync.WaitGroup
	termBuffer *bytes.Buffer
	done       bool
}

func (f *nonInteractiveStep) TermOutput() io.Writer {
	if f.termBuffer == nil {
		f.termBuffer = new(bytes.Buffer)
	}
	return &stripAnsiWriter{Next: f.termBuffer}
}

func (f *nonInteractiveStep) Update(str string, args ...interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintln(color.Output, "-> "+fmt.Sprintf(str, args...))
}

func (f *nonInteractiveStep) Status(status string) {}

func (f *nonInteractiveStep) Done() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.done {
		return
	}

	// Set done
	f.done = true

	// Unset the waitgroup
	f.wg.Done()
}

func (f *nonInteractiveStep) Abort() {
	if _, err := io.Copy(color.Output, f.termBuffer); err != nil {
		fmt.Fprintf(color.Output, "Error printing buffered terminal output: %s\n", err)
	}

	f.Done()
}

type stripAnsiWriter struct {
	Next io.Writer
}

func (w *stripAnsiWriter) Write(p []byte) (n int, err error) {
	return w.Next.Write(reAnsi.ReplaceAll(p, []byte{}))
}

var reAnsi = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))")
