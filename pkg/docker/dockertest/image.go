package dockertest

import (
	"fmt"
	"path"
	"strings"
	"testing"
	"time"
)

// ImageRef returns an reference based on the unique test name and label.
// If the label is empty, it will default to "test-" followed by the current unix epoch time.
func ImageRef(t *testing.T, label string) string {
	if label == "" {
		label = fmt.Sprintf("test-%d", time.Now().Unix())
	}

	return fmt.Sprintf("cog-test/%s:%s", strings.ToLower(t.Name()), label)
}

func ImageRefWithRegistry(t *testing.T, registryAddr string, label string) string {
	return path.Join(registryAddr, ImageRef(t, label))
}
