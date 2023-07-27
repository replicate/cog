package weights

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

const weightsHashFilename = ".cog/weights_hash.json"

// FileHash defines the struct to store the hash of a single file
type FileHash struct {
	Path string `json:"path"`
	// CRC32 is the hexadecimal-encoded CRC32 hash checksum of the file
	CRC32 string `json:"crc32"`
}

// Hash defines the struct to store the hash of the weights files
type Hash struct {
	FileHashes []FileHash `json:"file_hashes"`
}

func NewHash() *Hash {
	return &Hash{}
}

func (h *Hash) ToMap() map[string]string {
	m := make(map[string]string)
	for _, fh := range h.FileHashes {
		m[fh.Path] = fh.CRC32
	}
	return m
}

func (h *Hash) Load() error {
	if _, err := os.Stat(weightsHashFilename); err != nil {
		return err
	}
	file, err := os.Open(weightsHashFilename)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	return decoder.Decode(h)
}

func (h *Hash) Save() error {
	file, err := os.Create(weightsHashFilename)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(h)
}

func (h *Hash) AddFileHash(path string) error {
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

	h.FileHashes = append(h.FileHashes, FileHash{
		Path:  path,
		CRC32: encoded,
	})
	return nil
}
