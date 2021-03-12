package global

import (
	"time"
)

const Version = "0.0.1"

var (
	Port           int
	Verbose        = false
	StartupTimeout = 5 * time.Minute
)
