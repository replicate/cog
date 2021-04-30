package storage

import (
	"errors"
	"io"
)

type Storage interface {
	Upload(user string, name string, id string, reader io.Reader) error
	Download(user string, name string, id string) ([]byte, error) // TODO(andreas): return reader
	DownloadFile(user string, name string, id string, path string) ([]byte, error)
	Delete(user string, name string, id string) error
}

var NotFound = errors.New("Not found")
