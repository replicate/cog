package terminal

import (
	"os"
	"strings"
)

const (
	StatusOK      = "ok"
	StatusError   = "error"
	StatusWarn    = "warn"
	StatusTimeout = "timeout"
	StatusAbort   = "abort"
)

var emojiStatus = map[string]string{
	StatusOK:      "\u2713",
	StatusError:   "❌",
	StatusWarn:    "⚠️",
	StatusTimeout: "⌛",
}

var textStatus = map[string]string{
	StatusOK:      " +",
	StatusError:   " !",
	StatusWarn:    " *",
	StatusTimeout: "<>",
}

// Status is used to provide an updating status to the user. The status
// usually has some animated element along with it such as a spinner.
type Status interface {
	// Update writes a new status. This should be a single line.
	Update(msg string)

	// Indicate that a step has finished, confering an ok, error, or warn upon
	// it's finishing state. If the status is not StatusOK, StatusError, or StatusWarn
	// then the status text is written directly to the output, allowing for custom
	// statuses.
	Step(status, msg string)

	// Close should be called when the live updating is complete. The
	// status will be cleared from the line.
	Close() error
}

var statusIcons map[string]string

const envForceEmoji = "WAYPOINT_FORCE_EMOJI"

func init() {
	if os.Getenv(envForceEmoji) != "" || strings.Contains(os.Getenv("LANG"), "UTF-8") {
		statusIcons = emojiStatus
	} else {
		statusIcons = textStatus
	}
}
