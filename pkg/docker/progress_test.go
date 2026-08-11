package docker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProgressWriterStatusReusesProgressLine(t *testing.T) {
	var output bytes.Buffer
	progress := newProgressWriter(&output, 0, true)
	progress.Write("weights/model.bin", "Downloading", 1, 2)
	progress.WriteStatus("weights/model.bin", "Download complete")
	progress.Close()

	assert.Equal(t, 1, strings.Count(output.String(), "\n"))
	assert.Contains(t, output.String(), "Download complete")
}
