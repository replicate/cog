package coglog

import "time"

type BuildLogContext struct {
	started    time.Time
	Fast       bool
	CogRuntime bool
	localImage bool
}
