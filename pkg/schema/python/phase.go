package python

import (
	"fmt"

	"github.com/replicate/cog/pkg/schema"
)

type parserPhase struct {
	Name string
	From parsePhase
	To   parsePhase
	Run  func(*ParseState) error
}

func runPhases(state *ParseState, phases []parserPhase) error {
	for _, phase := range phases {
		if state.Phase != phase.From {
			return schema.WrapError(
				schema.ErrParse,
				fmt.Sprintf("phase %s expected parser state %q, got %q", phase.Name, phase.From, state.Phase),
				nil,
			)
		}
		if err := phase.Run(state); err != nil {
			return err
		}
		state.Phase = phase.To
	}
	return nil
}
