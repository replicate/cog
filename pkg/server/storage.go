package server

import (
	"io"
	"context"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/replicate/modelserver/pkg/global"
)

func UploadToStorage(file io.Reader, filename string) error {
	client, err := NewStorageClient()
	if err != nil {
		return err
	}
	bucket := client.Bucket(global.GCSBucket)
	obj := bucket.Object(filename)
	w := obj.NewWriter(context.Background())
	if _, err := io.Copy(w, file); err != nil {
		return err
	}
	return nil
}

func NewStorageClient() (*storage.Client, error) {
	authOption := option.WithTokenSource(global.TokenSource)
	client, err := storage.NewClient(context.Background(), authOption)
	return client, err
}
