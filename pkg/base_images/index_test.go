package base_images

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexFiltering(t *testing.T) {
	tests := []struct {
		Name        string
		constraints []constraint
		input       []string
		want        []string
	}{
		{
			Name:        "no filters",
			constraints: []constraint{},
			input: []string{
				"name1,cpu,22.04,,3.10,run,dev",
				"name2,gpu,22.04,11.8,3.10,run,dev",
			},
			want: []string{
				"name1,cpu,22.04,,3.10,run,dev",
				"name2,gpu,22.04,11.8,3.10,run,dev",
			},
		},
		{
			Name: "filter by accelerator",
			constraints: []constraint{
				ForAccelerator(AcceleratorGPU),
			},
			input: []string{
				"name1,cpu,22.04,,3.10,run,dev",
				"name2,gpu,22.04,11.8,3.10,run,dev",
			},
			want: []string{
				"name2,gpu,22.04,11.8,3.10,run,dev",
			},
		},
		{
			Name: "filter by python version",
			constraints: []constraint{
				PythonConstraint("3.10"),
			},
			input: []string{
				"name1,cpu,22.04,,3.10,run,dev",
				"name2,cpu,22.04,,3.11,run,dev",
				"name3,cpu,22.04,,3.9,run,dev",
				"name4,gpu,22.04,11.8,3.10,run,dev",
			},
			want: []string{
				"name1,cpu,22.04,,3.10,run,dev",
				"name4,gpu,22.04,11.8,3.10,run,dev",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {

			var expected []*BaseImage
			for _, l := range test.want {
				img, err := parseRecord(strings.Split(l, ","))
				require.NoError(t, err)
				expected = append(expected, img)
			}

			var buf bytes.Buffer
			for _, l := range test.input {
				fmt.Fprintln(&buf, l)
			}

			idx, err := newIndex(&buf)
			require.NoError(t, err)

			got, err := idx.Query(test.constraints...)
			require.NoError(t, err)

			assert.ElementsMatch(t, expected, got)
		})
	}
}
