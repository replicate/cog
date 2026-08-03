package util

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressReaderThrottlesReports(t *testing.T) {
	var reports []int64
	r := NewProgressReader(bytes.NewReader([]byte("abcdef")), func(complete int64) {
		reports = append(reports, complete)
	}).(*progressReader)
	r.interval = time.Hour

	buf := make([]byte, 2)
	_, err := r.Read(buf)
	require.NoError(t, err)
	_, err = r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, []int64{2}, reports)

	r.lastUpdate = time.Now().Add(-2 * r.interval)
	_, err = r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 6}, reports)
}
