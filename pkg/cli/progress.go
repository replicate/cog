package cli

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/docker/docker/pkg/jsonmessage"

	"github.com/replicate/cog/pkg/util/console"
)

// progressWriter adapts push progress callbacks to Docker's jsonmessage rendering.
//
// This uses the same ANSI cursor movement and progress display as `docker push`,
// which handles terminal resizing correctly: each line is erased and rewritten
// individually (ESC[2K + cursor up/down per line), rather than relying on a
// bulk cursor-up count that can desync when lines wrap after a terminal resize.
type progressWriter struct {
	mu   sync.Mutex
	pw   *io.PipeWriter
	done chan error
	once sync.Once
}

// newProgressWriter creates a progressWriter that renders push progress to stderr
// using Docker's jsonmessage format, matching the output of `docker push`.
func newProgressWriter() *progressWriter {
	pr, pw := io.Pipe()
	isTTY := console.IsTTY(os.Stderr)
	done := make(chan error, 1)

	go func() {
		done <- jsonmessage.DisplayJSONMessagesStream(pr, os.Stderr, os.Stderr.Fd(), isTTY, nil)
	}()

	return &progressWriter{
		pw:   pw,
		done: done,
	}
}

// Write sends a progress update for a specific layer/artifact.
// id is a unique identifier for the item (layer digest, artifact name).
// status is the current operation (e.g. "Pushing").
// current and total are the byte counts for the progress bar.
func (p *progressWriter) Write(id, status string, current, total int64) {
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

// WriteStatus sends a status-only message for a specific layer/artifact
// (no progress bar), e.g. "Pushed", "FAILED", or retry messages.
func (p *progressWriter) WriteStatus(id, status string) {
	msg := jsonmessage.JSONMessage{
		ID:     id,
		Status: status,
	}
	p.writeMessage(msg)
}

func (p *progressWriter) writeMessage(msg jsonmessage.JSONMessage) {
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
func (p *progressWriter) Close() {
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
