package coglog

import "time"

type PushLogContext struct {
	started    time.Time
	fast       bool
	localImage bool
}
