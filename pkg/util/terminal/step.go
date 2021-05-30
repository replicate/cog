package terminal

import (
	"context"
	"io"
)

const (
	TermRows    = 10
	TermColumns = 100
)

// fancyStepGroup implements StepGroup with live updating and a display
// "window" for live terminal output (when using TermOutput).
type fancyStepGroup struct {
	ctx    context.Context
	cancel func()

	display *Display

	steps int
	done  chan struct{}
}

// Start a step in the output
func (f *fancyStepGroup) Add(str string, args ...interface{}) Step {
	f.steps++

	ent := f.display.NewStatus(0)

	ent.StartSpinner()
	ent.Update(str, args...)

	return &fancyStep{
		sg:  f,
		ent: ent,
	}
}

func (f *fancyStepGroup) Wait() {
	if f.steps > 0 {
	loop:
		for {
			select {
			case <-f.done:
				f.steps--

				if f.steps <= 0 {
					break loop
				}
			case <-f.ctx.Done():
				break loop
			}
		}
	}

	f.cancel()

	f.display.Close()
}

type fancyStep struct {
	sg  *fancyStepGroup
	ent *DisplayEntry

	done   bool
	status string

	term *Term
}

func (f *fancyStep) TermOutput() io.Writer {
	if f.term == nil {
		t, err := NewTerm(f.sg.ctx, f.ent, TermRows, TermColumns)
		if err != nil {
			panic(err)
		}

		f.term = t
	}

	return f.term
}

func (f *fancyStep) Update(str string, args ...interface{}) {
	f.ent.Update(str, args...)
}

func (f *fancyStep) Status(status string) {
	f.status = status
	f.ent.SetStatus(status)
}

func (f *fancyStep) Done() {
	if f.done {
		return
	}

	if f.status == "" {
		f.Status(StatusOK)
	}

	f.signalDone()
}

func (f *fancyStep) Abort() {
	if f.done {
		return
	}

	f.Status(StatusError)
	f.signalDone()
}

func (f *fancyStep) signalDone() {
	f.done = true
	f.ent.StopSpinner()

	// We don't want to block here because Wait might not yet have been
	// called. So instead we just spawn the wait update in a goroutine
	// that can also be cancaled by the context.
	go func() {
		select {
		case f.sg.done <- struct{}{}:
		case <-f.sg.ctx.Done():
			return
		}
	}()
}
