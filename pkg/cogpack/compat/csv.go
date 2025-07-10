package compat

import (
	"io"

	"github.com/gocarina/gocsv"
)

func readCSV[T any](r io.Reader) ([]T, error) {
	var data []T
	if err := gocsv.Unmarshal(r, &data); err != nil {
		return nil, err
	}

	return data, nil
}
