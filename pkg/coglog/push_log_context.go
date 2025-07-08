package coglog

import "time"

type PushLogContext struct {
	started    time.Time
	Fast       bool
	CogRuntime bool
	localImage bool
}
