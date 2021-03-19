package storage

import (
	"io"
)

type Storage interface {
	Upload(reader io.Reader, id string) error
	Download(id string) ([]byte, error)
	Delete(id string) error
}
