package util

import (
	"io"
	"sync"
)

// RingBufferWriter is a writer that writes to an underlying writer and also maintains
// a ring buffer of the last N bytes written.
type RingBufferWriter struct {
	writer io.Writer
	buffer []byte
	size   int
	pos    int
	mu     sync.Mutex
}

// NewRingBufferWriter creates a new RingBufferWriter that writes to w and maintains
// a buffer of the last size bytes.
func NewRingBufferWriter(w io.Writer, size int) *RingBufferWriter {
	return &RingBufferWriter{
		writer: w,
		buffer: make([]byte, size),
		size:   size,
	}
}

// Write implements io.Writer interface
func (w *RingBufferWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Write to underlying writer
	n, err = w.writer.Write(p)
	if err != nil {
		return n, err
	}

	// Update ring buffer
	for _, b := range p {
		w.buffer[w.pos] = b
		w.pos = (w.pos + 1) % w.size
	}

	return n, nil
}

// String returns the contents of the ring buffer as a string
func (w *RingBufferWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If buffer is not full, return what we have
	if w.pos < w.size {
		return string(w.buffer[:w.pos])
	}

	// Otherwise, return the last size bytes
	return string(w.buffer[w.pos:]) + string(w.buffer[:w.pos])
}
