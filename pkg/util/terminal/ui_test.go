package terminal

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamedValues(t *testing.T) {
	require := require.New(t)

	var buf bytes.Buffer
	var ui nonInteractiveUI
	ui.NamedValues([]NamedValue{
		{"hello", "a"},
		{"this", "is"},
		{"a", "test"},
		{"of", "foo"},
		{"the_key_value", "style"},
	},
		WithWriter(&buf),
	)

	expected := `
          hello: a
           this: is
              a: test
             of: foo
  the_key_value: style

`

	require.Equal(strings.TrimLeft(expected, "\n"), buf.String())
}

func TestNamedValues_server(t *testing.T) {
	require := require.New(t)

	var buf bytes.Buffer
	var ui nonInteractiveUI
	ui.Output("Server configuration:", WithHeaderStyle(), WithWriter(&buf))
	ui.NamedValues([]NamedValue{
		{"DB Path", "data.db"},
		{"gRPC Address", "127.0.0.1:1234"},
		{"HTTP Address", "127.0.0.1:1235"},
		{"URL Service", "api.alpha.waypoint.run:443 (account: token)"},
	},
		WithWriter(&buf),
	)

	expected := `
» Server configuration:
       DB Path: data.db
  gRPC Address: 127.0.0.1:1234
  HTTP Address: 127.0.0.1:1235
   URL Service: api.alpha.waypoint.run:443 (account: token)

`

	require.Equal(expected, buf.String())
}

func TestStatusStyle(t *testing.T) {
	require := require.New(t)

	var buf bytes.Buffer
	var ui nonInteractiveUI
	ui.Output(strings.TrimSpace(`
one
two
  three`),
		WithWriter(&buf),
		WithInfoStyle(),
	)

	expected := `  one
  two
    three
`

	require.Equal(expected, buf.String())
}
