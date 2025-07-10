package core

// Op is implemented by all concrete build instructions.
type Op interface {
	Type() string
}

// Exec executes a shell or exec-form command.
type Exec struct {
	Shell bool     `json:"shell"` // if true, Args are concatenated into a shell string
	Args  []string `json:"args"`
	Env   []string `json:"env,omitempty"`
}

func (r Exec) Type() string { return "exec" }

// Copy copies files/directories into the image.
type Copy struct {
	From  string   `json:"from,omitempty"` // optional stage name / image ref
	Src   []string `json:"src"`
	Dest  string   `json:"dest"`
	Chown string   `json:"chown,omitempty"`
}

func (c Copy) Type() string { return "copy" }

// Add is like Copy but supports remote URLs and auto-extraction.
type Add struct {
	Src   []string `json:"src"`
	Dest  string   `json:"dest"`
	Chown string   `json:"chown,omitempty"`
}

func (a Add) Type() string { return "add" }

// Stage is the main unit of work produced by Providers and ultimately executed
// by the builder. A Stage may expand to one or more container layers depending
// on its LayerID.
type Stage struct {
	Name     string            `json:"name"`     // human readable identifier
	LayerID  string            `json:"layer_id"` // identical IDs are merged into one layer
	Inputs   []Input           `json:"inputs"`   // inputs to the step
	Commands []Op              `json:"commands"` // low-level build commands
	Requires []string          `json:"requires"` // artifacts needed
	Provides []string          `json:"provides"` // artifacts produced
	Meta     map[string]string `json:"meta"`     // optional free-form annotations
}

// Plan separates build-time steps (compile, install deps, etc.) from export-
// time steps (copy artifacts, set metadata). Additional export lists can be
// added later (e.g., OCI layout, tarball).
type Plan struct {
	Dependencies []Dependency `json:"dependencies"`
	BuildSteps   []Stage      `json:"build_steps"`
	ExportSteps  []Stage      `json:"export_steps"`
}

type Input struct {
	Image string `json:"image"`
	Stage string `json:"stage"`
}
