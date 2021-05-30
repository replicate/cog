package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/containerd/console"
	"github.com/lab47/vterm/parser"
	"github.com/lab47/vterm/screen"
	"github.com/lab47/vterm/state"
	"github.com/morikuni/aec"
)

var spinnerSet = spinner.CharSets[11]

type DisplayEntry struct {
	d       *Display
	line    uint
	index   int
	indent  int
	spinner bool
	text    string
	status  string

	body []string

	next *DisplayEntry
}

type Display struct {
	mu      sync.Mutex
	Entries []*DisplayEntry

	w       io.Writer
	newEnt  chan *DisplayEntry
	updates chan *DisplayEntry
	resize  chan struct{} // sent to when an entry has resized itself.
	line    uint
	width   int

	wg       sync.WaitGroup
	spinning int
}

func NewDisplay(ctx context.Context, w io.Writer) *Display {
	d := &Display{
		w:       w,
		width:   80,
		updates: make(chan *DisplayEntry),
		resize:  make(chan struct{}),
		newEnt:  make(chan *DisplayEntry),
	}

	if f, ok := w.(*os.File); ok {
		if c, err := console.ConsoleFromFile(f); err == nil {
			if sz, err := c.Size(); err == nil {
				if sz.Width >= 10 {
					d.width = int(sz.Width) - 1
				}
			}
		}
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.Display(ctx)
	}()

	return d
}

func (d *Display) Close() error {
	d.wg.Wait()
	return nil
}

func (d *Display) flushAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for range d.Entries {
		fmt.Fprintln(d.w, "")
	}

	d.line = uint(len(d.Entries))
}

func (d *Display) renderEntry(ent *DisplayEntry, spin int) {
	b := aec.EmptyBuilder

	diff := d.line - ent.line

	text := strings.TrimRight(ent.text, " \t\n")

	if len(text) >= d.width {
		text = text[:d.width-1]
	}

	prefix := ""
	if ent.spinner {
		prefix = spinnerSet[spin] + " "
	}

	var statusColor *aec.Builder
	if ent.status != "" {
		icon, ok := statusIcons[ent.status]
		if !ok {
			icon = ent.status
		}

		if len(prefix) > 0 {
			prefix = prefix + " " + icon + " "
		} else {
			prefix = icon + " "
		}

		if codes, ok := colorStatus[ent.status]; ok {
			statusColor = b.With(codes...)
		}
	}

	line := fmt.Sprintf("%s%s%s",
		b.
			Up(diff).
			Column(0).
			EraseLine(aec.EraseModes.All).
			ANSI,
		prefix,
		text,
	)

	if statusColor != nil {
		line = statusColor.ANSI.Apply(line)
	}

	fmt.Fprint(d.w, line)

	for _, body := range ent.body {
		fmt.Fprintf(d.w, "%s%s",
			b.
				Down(1).
				Column(0).
				ANSI,
			body,
		)
		diff--
	}

	fmt.Fprintf(d.w, "%s",
		b.
			Down(diff).
			Column(0).
			ANSI,
	)
}

func (d *Display) Display(ctx context.Context) {
	// d.flushAll()

	ticker := time.NewTicker(time.Second / 6)

	var spin int

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			spin++
			if spin >= len(spinnerSet) {
				spin = 0
			}

			d.mu.Lock()
			update := d.spinning > 0

			if !update {
				d.mu.Unlock()
				continue
			}

			for _, ent := range d.Entries {
				if !ent.spinner {
					continue
				}

				d.renderEntry(ent, spin)
			}

			d.mu.Unlock()
		case ent := <-d.newEnt:
			d.mu.Lock()
			ent.line = d.line
			d.Entries = append(d.Entries, ent)
			d.line++
			d.line += uint(len(ent.body))
			fmt.Fprintln(d.w, "")
			for i := 0; i < len(ent.body); i++ {
				fmt.Fprintln(d.w, "")
			}

			d.mu.Unlock()

		case ent := <-d.updates:
			d.mu.Lock()
			d.renderEntry(ent, spin)
			d.mu.Unlock()
		case <-d.resize:
			d.mu.Lock()

			var newLine uint

			for _, ent := range d.Entries {
				newLine++
				newLine += uint(len(ent.body))
			}

			diff := newLine - d.line

			// TODO should we support shrinking?
			if diff > 0 {
				// Pad down
				for i := uint(0); i < diff; i++ {
					fmt.Fprintln(d.w, "")
				}

				d.line = newLine

				var cnt uint

				for _, ent := range d.Entries {
					ent.line = cnt
					cnt++
					cnt += uint(len(ent.body))

					d.renderEntry(ent, spin)
				}
			}

			d.mu.Unlock()
		}
	}
}

func (d *Display) NewStatus(indent int) *DisplayEntry {
	de := &DisplayEntry{
		d:      d,
		indent: indent,
	}

	d.newEnt <- de

	return de
}

func (d *Display) NewStatusWithBody(indent, lines int) *DisplayEntry {
	de := &DisplayEntry{
		d:      d,
		indent: indent,
		body:   make([]string, lines),
	}

	d.newEnt <- de

	return de
}

func (e *DisplayEntry) StartSpinner() {
	e.d.mu.Lock()

	e.spinner = true
	e.d.spinning++

	e.d.mu.Unlock()

	e.d.updates <- e
}

func (e *DisplayEntry) StopSpinner() {
	e.d.mu.Lock()

	e.spinner = false
	e.d.spinning--

	e.d.mu.Unlock()

	e.d.updates <- e
}

func (e *DisplayEntry) SetStatus(status string) {
	e.d.mu.Lock()
	defer e.d.mu.Unlock()

	e.status = status
}

func (e *DisplayEntry) Update(str string, args ...interface{}) {
	e.d.mu.Lock()
	e.text = fmt.Sprintf(str, args...)
	e.d.mu.Unlock()

	e.d.updates <- e
}

func (e *DisplayEntry) SetBody(line int, data string) {
	e.d.mu.Lock()

	var resize bool

	if line >= len(e.body) {
		nb := make([]string, line+1)

		for i, s := range e.body {
			nb[i] = s
		}

		e.body = nb
		resize = true
	}

	e.body[line] = data
	e.d.mu.Unlock()

	if resize {
		e.d.resize <- struct{}{}
	}

	e.d.updates <- e
}

type Term struct {
	ent    *DisplayEntry
	scr    *screen.Screen
	w      io.Writer
	ctx    context.Context
	cancel func()

	output [][]rune

	wg       sync.WaitGroup
	parseErr error
}

func (t *Term) DamageDone(r state.Rect, cr screen.CellReader) error {
	for row := r.Start.Row; row <= r.End.Row; row++ {
		for col := r.Start.Col; col <= r.End.Col; col++ {
			cell := cr.GetCell(row, col)

			if cell == nil {
				t.output[row][col] = ' '
			} else {
				val, _ := cell.Value()

				if val == 0 {
					t.output[row][col] = ' '
				} else {
					t.output[row][col] = val
				}
			}
		}
	}

	for row := r.Start.Row; row <= r.End.Row; row++ {
		b := aec.EmptyBuilder
		blue := b.LightBlueF()
		t.ent.SetBody(row, fmt.Sprintf(" │ %s%s%s", blue.ANSI, string(t.output[row]), aec.Reset))
	}

	return nil
}

func (t *Term) MoveCursor(p state.Pos) error {
	// Ignore it.
	return nil
}

func (t *Term) SetTermProp(attr state.TermAttr, val interface{}) error {
	// Ignore it.
	return nil
}

func (t *Term) Output(data []byte) error {
	// Ignore it.
	return nil
}

func (t *Term) StringEvent(kind string, data []byte) error {
	// Ignore them.
	return nil
}

func NewTerm(ctx context.Context, d *DisplayEntry, height, width int) (*Term, error) {
	term := &Term{
		ent:    d,
		output: make([][]rune, height),
	}

	for i := range term.output {
		term.output[i] = make([]rune, width)
	}

	scr, err := screen.NewScreen(height, width, term)
	if err != nil {
		return nil, err
	}

	term.scr = scr

	st, err := state.NewState(height, width, scr)
	if err != nil {
		return nil, err
	}

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	term.w = w

	prs, err := parser.NewParser(r, st)
	if err != nil {
		return nil, err
	}

	term.ctx, term.cancel = context.WithCancel(ctx)

	term.wg.Add(1)
	go func() {
		defer term.wg.Done()

		err := prs.Drive(term.ctx)
		if err != nil && err != context.Canceled {
			term.parseErr = err
		}
	}()

	return term, nil
}

func (t *Term) Write(b []byte) (int, error) {
	return t.w.Write(b)
}

func (t *Term) Close() error {
	t.cancel()
	t.wg.Wait()
	return t.parseErr
}
