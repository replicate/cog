package model

import "os"

// TODO(md): OCIIndexEnabled is a temporary gate for the OCI Image Index push path.
// When COG_OCI_INDEX=1, builds produce weight artifacts and pushes create an OCI
// Image Index instead of a single image manifest. Remove this gate (and always use
// the index path) once we've validated index compatibility with all registries.
func OCIIndexEnabled() bool {
	return os.Getenv("COG_OCI_INDEX") == "1"
}
