package util

import "fmt"

// WrapError is just a shortcut for using fmt.Errorf
// to wrap an error with a message
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
