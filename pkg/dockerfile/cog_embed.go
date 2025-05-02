package dockerfile

import (
	"embed"
	"fmt"
	"path/filepath"
)

const EmbedDir = "embed"

//go:embed embed/*.whl
var CogEmbed embed.FS

func WheelFilename() (string, error) {
	files, err := CogEmbed.ReadDir(EmbedDir)
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", fmt.Errorf("should only have one cog wheel embedded")
	}
	return files[0].Name(), nil
}

func ReadWheelFile() ([]byte, string, error) {
	filename, err := WheelFilename()
	if err != nil {
		return nil, "", err
	}
	data, err := CogEmbed.ReadFile(filepath.Join(EmbedDir, filename))
	if err != nil {
		return nil, "", err
	}
	return data, filename, err
}
