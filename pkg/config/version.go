package config

// ArgumentType represents the type of a run argument.
type ArgumentType string

const (
	ArgumentTypeString ArgumentType = "str"
	ArgumentTypeInt    ArgumentType = "int"
	ArgumentTypeFloat  ArgumentType = "float"
	ArgumentTypeBool   ArgumentType = "bool"
	ArgumentTypePath   ArgumentType = "Path"
)

// RunArgument describes a single argument for a prediction run.
type RunArgument struct {
	Type    ArgumentType `json:"type"`
	Default *string      `json:"default"`
	Min     *string      `json:"min"`
	Max     *string      `json:"max"`
	Options *[]string    `json:"options"`
	Help    *string      `json:"help"`
}
