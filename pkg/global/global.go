package global

import (
	"time"
)

var Version = "0.0.1"
var BuildTime = "none"

var (
	Verbose        = false
	StartupTimeout = 5 * time.Minute
)
