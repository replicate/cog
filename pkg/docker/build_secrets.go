package docker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/session/secrets"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

func ParseSecretsFromHost(workingDir string, secrets []string) (secrets.SecretStore, error) {
	sources := make([]secretsprovider.Source, 0, len(secrets))

	for _, secret := range secrets {
		src, err := parseSecretFromHost(workingDir, secret)
		if err != nil {
			return nil, err
		}
		sources = append(sources, *src)
	}

	return secretsprovider.NewStore(sources)
}

func parseSecretFromHost(workingDir, secret string) (*secretsprovider.Source, error) {
	fields, err := csvvalue.Fields(secret, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse csv secret: %w", err)
	}

	src := secretsprovider.Source{}

	var typ string
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return nil, errors.Errorf("invalid field %q must be a key=value pair", field)
		}
		key = strings.ToLower(key)
		switch key {
		case "type":
			if value != "file" && value != "env" {
				return nil, errors.Errorf("unsupported secret type %q", value)
			}
			typ = value
		case "id":
			src.ID = value
		case "source", "src":
			if !filepath.IsAbs(value) {
				value = filepath.Join(workingDir, value)
				value, err = filepath.Abs(value)
				if err != nil {
					return nil, fmt.Errorf("failed to get absolute path for %q: %w", value, err)
				}
			}
			src.FilePath = value
		case "env":
			src.Env = value
		default:
			return nil, errors.Errorf("unexpected key '%s' in '%s'", key, field)
		}
	}
	if typ == "env" && src.Env == "" {
		src.Env = src.FilePath
		src.FilePath = ""
	}
	return &src, nil
}
