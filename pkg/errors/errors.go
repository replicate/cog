package errors

const (
	CodeConfigNotFound = "CONFIG_NOT_FOUND"
)

// Types ////////////////////////////////////////

type CodedError interface {
	Code() string
}

type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string {
	return e.msg
}

func (e *codedError) Code() string {
	return e.code
}

// Error Creators ///////////////////////////////

// The Cog config was not found
func ConfigNotFound(msg string) error {
	return &codedError{
		code: CodeConfigNotFound,
		msg:  msg + ``, // TODO: populate this
	}
}

// Helpers //////////////////////////////////////

func IsConfigNotFound(err error) bool {
	return Code(err) == CodeConfigNotFound
}

// Return the error code, or the empty string
func Code(err error) string {
	if cerr, ok := err.(CodedError); ok {
		return cerr.Code()
	}

	return ""
}
