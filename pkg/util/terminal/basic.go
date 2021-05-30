package terminal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bgentry/speakeasy"
	"github.com/containerd/console"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	sshterm "golang.org/x/crypto/ssh/terminal"
)

// basicUI
type basicUI struct {
	ctx    context.Context
	status *spinnerStatus
}

// Returns a UI which will write to the current processes
// stdout/stderr.
func ConsoleUI(ctx context.Context) UI {
	// We do both of these checks because some sneaky environments fool
	// one or the other and we really only want the glint-based UI in
	// truly interactive environments.
	glint := isatty.IsTerminal(os.Stdout.Fd()) && sshterm.IsTerminal(int(os.Stdout.Fd()))
	if glint {
		glint = false
		if c, err := console.ConsoleFromFile(os.Stdout); err == nil {
			if sz, err := c.Size(); err == nil {
				glint = sz.Height > 0 && sz.Width > 0
			}
		}
	}

	if glint {
		return GlintUI(ctx)
	} else {
		return NonInteractiveUI(ctx)
	}
}

// Input implements UI
func (ui *basicUI) Input(input *Input) (string, error) {
	var buf bytes.Buffer

	// Write the prompt, add a space.
	ui.Output(input.Prompt, WithStyle(input.Style), WithWriter(&buf))
	fmt.Fprint(color.Output, strings.TrimRight(buf.String(), "\r\n"))
	fmt.Fprint(color.Output, " ")

	// Ask for input in a go-routine so that we can ignore it.
	errCh := make(chan error, 1)
	lineCh := make(chan string, 1)
	go func() {
		var line string
		var err error
		if input.Secret && isatty.IsTerminal(os.Stdin.Fd()) {
			line, err = speakeasy.Ask("")
		} else {
			r := bufio.NewReader(os.Stdin)
			line, err = r.ReadString('\n')
		}
		if err != nil {
			errCh <- err
			return
		}

		lineCh <- strings.TrimRight(line, "\r\n")
	}()

	select {
	case err := <-errCh:
		return "", err
	case line := <-lineCh:
		return line, nil
	case <-ui.ctx.Done():
		// Print newline so that any further output starts properly
		fmt.Fprintln(color.Output)
		return "", ui.ctx.Err()
	}
}

// Interactive implements UI
func (ui *basicUI) Interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// Output implements UI
func (ui *basicUI) Output(msg string, raw ...interface{}) {
	msg, style, w := Interpret(msg, raw...)

	switch style {
	case HeaderStyle:
		msg = colorHeader.Sprintf("\n==> %s", msg)
	case ErrorStyle:
		msg = colorError.Sprint(msg)
	case ErrorBoldStyle:
		msg = colorErrorBold.Sprint(msg)
	case WarningStyle:
		msg = colorWarning.Sprint(msg)
	case WarningBoldStyle:
		msg = colorWarningBold.Sprint(msg)
	case SuccessStyle:
		msg = colorSuccess.Sprint(msg)
	case SuccessBoldStyle:
		msg = colorSuccessBold.Sprint(msg)
	case InfoStyle:
		lines := strings.Split(msg, "\n")
		for i, line := range lines {
			lines[i] = colorInfo.Sprintf("    %s", line)
		}

		msg = strings.Join(lines, "\n")
	}

	st := ui.status
	if st != nil {
		if st.Pause() {
			defer st.Start()
		}
	}

	// Write it
	fmt.Fprintln(w, msg)
}

// NamedValues implements UI
func (ui *basicUI) NamedValues(rows []NamedValue, opts ...Option) {
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
	colorInfo.Fprintln(cfg.Writer, buf.String())
}

// OutputWriters implements UI
func (ui *basicUI) OutputWriters() (io.Writer, io.Writer, error) {
	return os.Stdout, os.Stderr, nil
}

// Status implements UI
func (ui *basicUI) Status() Status {
	if ui.status == nil {
		ui.status = newSpinnerStatus(ui.ctx)
	}

	return ui.status
}

func (ui *basicUI) StepGroup() StepGroup {
	ctx, cancel := context.WithCancel(ui.ctx)
	display := NewDisplay(ctx, color.Output)

	return &fancyStepGroup{
		ctx:     ctx,
		cancel:  cancel,
		display: display,
		done:    make(chan struct{}),
	}
}
