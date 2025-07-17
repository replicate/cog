package plan

import "errors"

var ErrDuplicateStageID = errors.New("stage ID already exists")
var ErrNoInputStage = errors.New("no previous stage available for input")
var ErrNoOutputStage = errors.New("no output stage available for input")
var ErrPhaseNotFound = errors.New("phase not found")
var ErrStageNotFound = errors.New("stage not found")
