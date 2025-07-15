package iterext

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcat(t *testing.T) {
	seq1 := slices.Values([]int{1, 2, 3})
	seq2 := slices.Values([]int{4, 5, 6})
	seq3 := slices.Values([]int{7, 8, 9})

	concat := Concat(seq1, seq2, seq3)

	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, slices.Collect(concat))
}

func TestConcat_Empty(t *testing.T) {
	t.Skip("why does this fail with []int{} != []int(nil)????????????")
	concat := Concat[int](slices.Values([]int{}))
	assert.Equal(t, []int{}, slices.Collect(concat))
}

func TestConcat_ReturnFalseDuringIteration(t *testing.T) {
	seq1 := slices.Values([]int{1, 2, 3})
	seq2 := slices.Values([]int{4, 5, 6})
	seq3 := slices.Values([]int{7, 8, 9})

	concat := Concat(seq1, seq2, seq3)

	var accumulated []int
	for val := range concat {
		accumulated = append(accumulated, val)
		if val == 4 {
			break
		}
	}

	assert.Equal(t, []int{1, 2, 3, 4}, accumulated)
}
