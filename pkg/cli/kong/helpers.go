package kong

import (
	"context"
	"os"
)

// contextFromGlobals returns a context for command execution.
func contextFromGlobals(_ *Globals) context.Context {
	return context.Background()
}

// propagateRustLog appends RUST_LOG from the host env for coglet debugging.
func propagateRustLog(env []string) []string {
	if v := os.Getenv("RUST_LOG"); v != "" {
		return append(env, "RUST_LOG="+v)
	}
	return env
}
