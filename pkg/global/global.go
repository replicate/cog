package global

import (
	"golang.org/x/oauth2"
)

const Version = "0.0.1"

var (
	Port             int
	Verbose          = false
	TokenSource      oauth2.TokenSource
	GCSBucket        = "andreas-adhoc"
	GCPProject       = "replicate"
	CloudSQLRegion   = "us-central1"
	CloudSQLInstance = "replicate2"
	CloudSQLDB       = "models-test"
	CloudSQLPassword string
)
