package docker

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/docker/docker/pkg/jsonmessage"

	"github.com/replicate/cog/pkg/util/console"
)

// ProgressWriter renders keyed progress updates with Docker's jsonmessage renderer.
//
// It uses the same ANSI cursor handling and terminal resize behavior as
// `docker push`.
type ProgressWriter struct {
	mu   sync.Mutex
	pw   *io.PipeWriter
	done chan error
	once sync.Once
}

// NewProgressWriter creates a ProgressWriter that renders progress to stderr.
func NewProgressWriter() *ProgressWriter {
	return newProgressWriter(os.Stderr, os.Stderr.Fd(), console.IsTTY(os.Stderr))
}

func newProgressWriter(out io.Writer, terminalFd uintptr, isTTY bool) *ProgressWriter {
	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		done <- jsonmessage.DisplayJSONMessagesStream(pr, out, terminalFd, isTTY, nil)
	}()

	return &ProgressWriter{
		pw:   pw,
		done: done,
	}
}

// Write sends a progress update for an item identified by id.
// status is the current operation, such as "Pushing".
// current and total are the byte counts for the progress bar.
func (p *ProgressWriter) Write(id, status string, current, total int64) {
	msg := jsonmessage.JSONMessage{
		ID:     id,
		Status: status,
		Progress: &jsonmessage.JSONProgress{
			Current: current,
			Total:   total,
		},
	}
	p.writeMessage(msg)
}

// WriteStatus replaces a progress row with a status and no progress bar.
func (p *ProgressWriter) WriteStatus(id, status string) {
	msg := jsonmessage.JSONMessage{
		ID:       id,
		Status:   status,
		Progress: &jsonmessage.JSONProgress{},
	}
	p.writeMessage(msg)
}

// WriteLine writes a non-progress line and clears the tracked progress rows.
func (p *ProgressWriter) WriteLine(message string) {
	p.writeMessage(jsonmessage.JSONMessage{Status: message})
}

func (p *ProgressWriter) writeMessage(msg jsonmessage.JSONMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pw == nil {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = p.pw.Write(data)
}

// Close shuts down the progress display. Safe to call multiple times.
func (p *ProgressWriter) Close() {
	p.once.Do(func() {
		p.mu.Lock()
		pw := p.pw
		p.pw = nil
		p.mu.Unlock()

		if pw != nil {
			_ = pw.Close()
			<-p.done
		}
	})
}
