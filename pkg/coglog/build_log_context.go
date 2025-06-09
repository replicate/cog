package coglog

import "time"

type BuildLogContext struct {
	started    time.Time
	fast       bool
	localImage bool
}
