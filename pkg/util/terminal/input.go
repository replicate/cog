package terminal

// Input is the configuration for an input.
type Input struct {
	// Prompt is a single-line prompt to give the user such as "Continue?"
	// The user will input their answer after this prompt.
	Prompt string

	// Style is the style to apply to the input. If this is blank,
	// the output won't be colorized in any way.
	Style string

	// True if this input is a secret. The input will be masked.
	Secret bool
}
