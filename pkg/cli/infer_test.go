package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIterateInferFileLines(t *testing.T) {
	text := `foo=bar,baz=qux
# comment, newline below

value
"# value"
 foo ,  bar , qux
foo="bar" , baz="qux"
`
	r := strings.NewReader(text)
	ch := iterateInferFileLines(r)
	require.Equal(t, &inferFileLine{1, []string{"foo=bar", "baz=qux"}}, <-ch)
	require.Equal(t, &inferFileLine{4, []string{"value"}}, <-ch)
	require.Equal(t, &inferFileLine{5, []string{`"# value"`}}, <-ch)
	require.Equal(t, &inferFileLine{6, []string{"foo", "bar", "qux"}}, <-ch)
	require.Equal(t, &inferFileLine{7, []string{`foo="bar"`, `baz="qux"`}}, <-ch)
}
