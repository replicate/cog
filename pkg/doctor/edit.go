package doctor

import "sort"

// byteEdit represents a single byte-range edit to a source buffer.
// If replacement is empty, it is a pure deletion.
type byteEdit struct {
	start       uint32
	end         uint32
	replacement []byte
}

// applyEdits applies a list of byte-range edits to source and returns the
// resulting buffer. Edits must not overlap. They are sorted descending by
// start so earlier byte offsets remain valid during application.
func applyEdits(source []byte, edits []byteEdit) []byte {
	if len(edits) == 0 {
		return source
	}
	sorted := make([]byteEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].start > sorted[j].start
	})

	result := make([]byte, len(source))
	copy(result, source)
	for _, e := range sorted {
		if int(e.start) > len(result) {
			continue
		}
		end := min(int(e.end), len(result))
		// Replace result[e.start:end] with e.replacement.
		tail := append([]byte{}, result[end:]...)
		result = append(result[:e.start], e.replacement...)
		result = append(result, tail...)
	}
	return result
}
