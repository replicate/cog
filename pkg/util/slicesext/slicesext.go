package slicesext

import (
	"slices"
)

func StableSort(s []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(s)))
}
