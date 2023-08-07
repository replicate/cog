package weights

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
)

// Manifest contains metadata about weights files in a model
type Manifest struct {
	Files map[string]Metadata `json:"files"`
}

// Metadata contains information about a file
type Metadata struct {
	// CRC32 is the CRC32 checksum of the file encoded as a hexadecimal string
	CRC32 string `json:"crc32"`
}

// NewManifest creates a new manifest
func NewManifest() *Manifest {
	return &Manifest{}
}

// LoadManifest loads a manifest from a file
func LoadManifest(filename string) (*Manifest, error) {
	if _, err := os.Stat(filename); err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	m := &Manifest{}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Save saves a manifest to a file
func (m *Manifest) Save(filename string) error {
	if err := os.MkdirAll(path.Dir(filename), 0o755); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(m)
}

// Equal compares the files in two manifests for strict equality
func (m *Manifest) Equal(other *Manifest) bool {
	if len(m.Files) != len(other.Files) {
		return false
	}

	for path, crc32 := range m.Files {
		if otherCrc32, ok := other.Files[path]; !ok || otherCrc32 != crc32 {
			return false
		}
	}

	return true
}

// AddFile adds a file to the manifest, calculating its CRC32 checksum
func (m *Manifest) AddFile(path string) error {
	crc32Algo := crc32.NewIEEE()
	// generate checksum of file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()
	_, err = io.Copy(crc32Algo, file)
	if err != nil {
		return fmt.Errorf("failed to generate checksum of file %s: %w", path, err)
	}
	checksum := crc32Algo.Sum32()

	// encode checksum as hexadecimal string
	bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(bytes, checksum)
	encoded := hex.EncodeToString(bytes)

	if m.Files == nil {
		m.Files = make(map[string]Metadata)
	}
	m.Files[path] = Metadata{
		CRC32: encoded,
	}

	return nil
}
