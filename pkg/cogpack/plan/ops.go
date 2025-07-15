package plan

// Op interface for build operations
type Op interface {
	Type() string
}

// Exec runs shell commands
type Exec struct {
	Command string  `json:"command"`          // command to execute (always uses shell)
	Mounts  []Mount `json:"mounts,omitempty"` // additional mounts needed
}

func (e Exec) Type() string { return "exec" }

// Copy copies files between stages/images
type Copy struct {
	From     Input       `json:"from"`              // source stage/image/url/local
	Src      []string    `json:"src"`               // source paths
	Dest     string      `json:"dest"`              // destination path
	Chown    string      `json:"chown,omitempty"`   // ownership
	Patterns FilePattern `json:"patterns,omitzero"` // include/exclude patterns
}

func (c Copy) Type() string { return "copy" }

// Add copies files with URL support and auto-extraction
type Add struct {
	From     Input       `json:"from,omitempty"`     // optional source stage/image/url/local
	Src      []string    `json:"src"`                // source paths/URLs
	Dest     string      `json:"dest"`               // destination path
	Chown    string      `json:"chown,omitempty"`    // ownership
	Patterns FilePattern `json:"patterns,omitempty"` // include/exclude patterns
}

func (a Add) Type() string { return "add" }

// SetEnv sets environment variables
type SetEnv struct {
	Vars map[string]string `json:"vars"` // environment variables to set
}

func (s SetEnv) Type() string { return "env" }

// MkFile creates a file at the specified path with given data and mode
type MkFile struct {
	Dest string `json:"dest"` // destination path
	Data []byte `json:"data"` // file contents
	Mode uint32 `json:"mode"` // file mode (e.g. 0644)
}

func (m MkFile) Type() string { return "mkfile" }

// FilePattern represents include/exclude patterns for file operations
type FilePattern struct {
	Include []string `json:"include,omitempty"` // glob patterns to include
	Exclude []string `json:"exclude,omitempty"` // glob patterns to exclude
}
