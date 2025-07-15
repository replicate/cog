package plan

import "fmt"

type SourceOpt func() Input

func FromCurrentState() SourceOpt {
	return func() Input {
		return Input{Auto: true}
	}
}

func FromScratch() SourceOpt {
	return func() Input {
		return Input{Scratch: true}
	}
}

func FromImage(image string) SourceOpt {
	return func() Input {
		return Input{Image: image}
	}
}

func FromLocal(local string) SourceOpt {
	return func() Input {
		return Input{Local: local}
	}
}

func FromURL(url string) SourceOpt {
	return func() Input {
		return Input{URL: url}
	}
}

func FromStage(stage string) SourceOpt {
	return func() Input {
		return Input{Stage: stage}
	}
}

func FromPhase(phase PhaseKey) SourceOpt {
	return func() Input {
		return Input{Phase: phase}
	}
}

// Input represents the starting point for a stage or an additional input for an operation
type Input struct {
	Auto bool `json:"auto,omitempty,omitzero"` // automatically resolve input

	Scratch bool     `json:"scratch,omitempty,omitzero"` // scratch input
	Image   string   `json:"image,omitempty,omitzero"`   // external image reference
	Local   string   `json:"local,omitempty,omitzero"`   // build context name
	URL     string   `json:"url,omitempty,omitzero"`     // HTTP/HTTPS URL for files
	Stage   string   `json:"stage,omitempty,omitzero"`   // reference to another stage
	Phase   PhaseKey `json:"phase,omitempty,omitzero"`   // reference to a phase result
}

func (i Input) Validate() error {
	var activeCount int

	if i.Image != "" {
		activeCount++
	}
	if i.Stage != "" {
		activeCount++
	}
	if i.Local != "" {
		activeCount++
	}
	if i.URL != "" {
		activeCount++
	}
	if i.Phase != "" {
		activeCount++
	}

	if i.Auto {
		activeCount++
	}

	if i.Scratch {
		activeCount++
	}

	if activeCount != 1 {
		return fmt.Errorf("exactly 1 input source is required")
	}

	return nil
}

func (i Input) IsBuildStage() bool {
	return i.Auto || i.Phase != "" || i.Stage != ""
}

// IsEmpty returns true if the input has no source specified
func (i Input) IsEmpty() bool {
	return i.Image == "" && i.Stage == "" && i.Local == "" && i.URL == "" && i.Phase == "" && !i.Auto && !i.Scratch
}
