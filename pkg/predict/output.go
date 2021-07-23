package predict

import "io"

type OutputValue struct {
	Buffer   io.Reader
	MimeType string
}

type Output struct {
	Values    map[string]OutputValue
	SetupTime float64
	RunTime   float64
}
