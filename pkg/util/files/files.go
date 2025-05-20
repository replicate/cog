package files

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func Exists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("Failed to determine if %s exists: %w", path, err)
	}
}

func IsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

func IsDir(path string) (bool, error) {
	file, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return file.Mode().IsDir(), nil
}

func IsExecutable(path string) bool {
	return unix.Access(path, unix.X_OK) == nil
}

func CopyFile(src string, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open %s while copying to %s: %w", src, dest, err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("Failed to create %s while copying %s: %w", dest, src, err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("Failed to copy %s to %s: %w", src, dest, err)
	}
	return out.Close()
}

func WriteIfDifferent(file, content string) error {
	if _, err := os.Stat(file); err == nil {
		bs, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if string(bs) == content {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Write out a new requirements file
	err := os.WriteFile(file, []byte(content), 0o644)
	if err != nil {
		return err
	}
	return nil
}
