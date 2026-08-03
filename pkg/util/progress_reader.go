package util

import (
	"io"
	"time"
)

const progressInterval = 250 * time.Millisecond

type progressReader struct {
	reader     io.Reader
	complete   int64
	lastUpdate time.Time
	interval   time.Duration
	report     func(int64)
}

// NewProgressReader reports throttled intermediate byte counts. It does not
// report EOF; callers should emit their own completion event.
func NewProgressReader(reader io.Reader, report func(int64)) io.Reader {
	return &progressReader{
		reader:   reader,
		interval: progressInterval,
		report:   report,
	}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n == 0 {
		return n, err
	}

	r.complete += int64(n)
	now := time.Now()
	if r.lastUpdate.IsZero() || now.Sub(r.lastUpdate) >= r.interval {
		r.lastUpdate = now
		r.report(r.complete)
	}
	return n, err
}
