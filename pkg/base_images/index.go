package base_images

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-version"
)

//go:embed base-images.csv
var baseImagesCSV []byte

type index struct {
	images []*BaseImage
}

func (i *index) Query(constraints ...Constraint) ([]*BaseImage, error) {
	var images []*BaseImage

	filter, err := joinConstraints(constraints...)
	if err != nil {
		return nil, err
	}

	for _, img := range i.images {
		if filter(img) {
			images = append(images, img)
		}
	}
	return images, nil
}

func joinConstraints(constraints ...Constraint) (filter, error) {
	filterFuncs := make([]filter, len(constraints))
	for i, constraint := range constraints {
		f, err := constraint()
		if err != nil {
			return nil, err
		}
		filterFuncs[i] = f
	}

	fn := func(img *BaseImage) bool {
		for _, filter := range filterFuncs {
			if !filter(img) {
				return false
			}
		}
		return true
	}
	return fn, nil
}

func defaultIndex() (*index, error) {
	return newIndex(bytes.NewReader(baseImagesCSV))
}

func newIndex(r io.Reader) (*index, error) {
	csvR := csv.NewReader(r)
	csvR.ReuseRecord = true

	var images []*BaseImage
	var fieldParseErr fieldParseErr

	// Attempt to read first record (may be header)
	firstRecord, err := csvR.Read()
	if err != nil {
		if err == io.EOF {
			return &index{images: []*BaseImage{}}, nil
		}
		return nil, err
	}

	// Detect header row (starts with "name" or "accelerator") – allow surrounding spaces
	header0 := ""
	if len(firstRecord) > 0 {
		header0 = strings.TrimSpace(firstRecord[0])
	}
	if !(header0 == "name" || header0 == "accelerator") {
		// Not a header row – process it as data
		img, err := parseRecord(firstRecord)
		if err != nil {
			if errors.As(err, &fieldParseErr) {
				_, col := csvR.FieldPos(fieldParseErr.field)
				return nil, fmt.Errorf("error parsing data row 1, col %d: %w", col, err)
			}
			return nil, err
		}
		images = append(images, img)
	}

	for {
		record, err := csvR.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		img, err := parseRecord(record)
		if err != nil {
			if errors.As(err, &fieldParseErr) {
				line, col := csvR.FieldPos(fieldParseErr.field)
				return nil, fmt.Errorf("error parsing line %d, col %d: %w", line, col, err)
			}
			return nil, err
		}

		images = append(images, img)
	}

	return &index{images: images}, nil
}

func parseRecord(record []string) (*BaseImage, error) {
	// CSV structure (current, only supported):
	//   0: name (ignored)
	//   1: accelerator
	//   2: ubuntu_version
	//   3: cuda_version
	//   4: python_version
	//   5: run_tag
	//   6: dev_tag

	if len(record) < 7 {
		return nil, fmt.Errorf("invalid record length: expected >=7 fields, got %d", len(record))
	}

	const (
		acceleratorField = 1
		ubuntuField      = 2
		cudaField        = 3
		pythonField      = 4
		runTagField      = 5
		devTagField      = 6
	)

	img := &BaseImage{
		RunTag: record[runTagField],
		DevTag: record[devTagField],
	}

	// Trim whitespace around all parsed fields first
	accel := strings.TrimSpace(record[acceleratorField])

	switch accel {
	case "cpu":
		img.Accelerator = AcceleratorCPU
	case "gpu":
		img.Accelerator = AcceleratorGPU
	default:
		return nil, fieldParseErr{field: acceleratorField, err: fmt.Errorf("invalid accelerator: %q", accel)}
	}

	// ubuntu version
	if s := strings.TrimSpace(record[ubuntuField]); s != "" {
		if v, err := version.NewVersion(s); err == nil {
			img.UbuntuVersion = v
		} else {
			return nil, fieldParseErr{field: ubuntuField, err: err}
		}
	}

	// cuda version
	if s := strings.TrimSpace(record[cudaField]); s != "" {
		if v, err := version.NewVersion(s); err == nil {
			img.CudaVersion = v
		} else {
			return nil, fieldParseErr{field: cudaField, err: err}
		}
	}

	// python version
	if s := strings.TrimSpace(record[pythonField]); s != "" {
		if v, err := version.NewVersion(s); err == nil {
			img.PythonVersion = v
		} else {
			return nil, fieldParseErr{field: pythonField, err: err}
		}
	}

	// Trim whitespace around all parsed fields first
	img.RunTag = strings.TrimSpace(img.RunTag)
	img.DevTag = strings.TrimSpace(img.DevTag)

	return img, nil
}

type fieldParseErr struct {
	field int
	err   error
}

func (e fieldParseErr) Error() string {
	return e.err.Error()
}
