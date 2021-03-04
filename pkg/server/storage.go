package server

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"

	"github.com/replicate/modelserver/pkg/global"
)

type Storage struct {
	client *storage.Client
}

func NewStorage() (*Storage, error) {
	authOption := option.WithTokenSource(global.TokenSource)
	client, err := storage.NewClient(context.Background(), authOption)
	if err != nil {
		return nil, err
	}
	return &Storage{client: client}, nil
}

func (s *Storage) bucket() *storage.BucketHandle {
	return s.client.Bucket(global.GCSBucket)
}

func (s *Storage) Upload(file io.Reader, filename string) error {
	bucket := s.bucket()
	obj := bucket.Object(filename)
	w := obj.NewWriter(context.Background())
	if _, err := io.Copy(w, file); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("Failed to close GCS writer")
	}
	return nil
}

func (s *Storage) Download(filename string) ([]byte, error) {
	bucket := s.bucket()
	obj := bucket.Object(filename)
	reader, err := obj.NewReader(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Failed to open %s: %w", filename, err)
	}
	log.Infof("Reading from gs://%s/%s", global.GCSBucket, filename)
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", filename, err)
	}
	return contents, nil
}
