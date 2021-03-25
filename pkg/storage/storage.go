package storage

import (
	"io"
)

type Storage interface {
	Upload(user string, name string, id string, reader io.Reader) error
	Download(user string, name string, id string) ([]byte, error)
	Delete(user string, name string, id string) error
}
