package coglog

import "time"

const StatusAccepted = "accepted"
const StatusPassed = "passed"
const StatusDeclined = "declined"
const StatusNone = "none"

type MigrateLogContext struct {
	started             time.Time
	accept              bool
	PythonPackageStatus string
	RunStatus           string
	PythonPredictStatus string
	PythonTrainStatus   string
}

func NewMigrateLogContext(accept bool) *MigrateLogContext {
	return &MigrateLogContext{
		started:             time.Now(),
		accept:              accept,
		PythonPackageStatus: StatusNone,
		RunStatus:           StatusNone,
		PythonPredictStatus: StatusNone,
		PythonTrainStatus:   StatusNone,
	}
}
