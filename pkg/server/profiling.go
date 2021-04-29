package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/profile"
)

// Profile responds with the pprof-formatted memory profile.
// Profiling lasts for duration specified in seconds GET parameter, or for 30 seconds if not specified.
// The package initialization registers it as /debug/pprof/profile.
func profileMemory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	sec, err := strconv.ParseInt(r.FormValue("seconds"), 10, 64)
	if sec <= 0 || err != nil {
		sec = 30
	}

	if durationExceedsWriteTimeout(r, float64(sec)) {
		pprofServeError(w, http.StatusBadRequest, "profile duration exceeds server's WriteTimeout")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="profile"`)
	profileDir, err := os.MkdirTemp("", "cog-mem-profile")
	if err != nil {
		pprofServeError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to create temp directory: %s", err))
		return
	}
	profiler := profile.Start(profile.ProfilePath(profileDir), profile.MemProfile)

	pprofSleep(r, time.Duration(sec)*time.Second)
	profiler.Stop()

	file, err := os.Open(filepath.Join(profileDir, "mem.pprof"))
	if err != nil {
		pprofServeError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to create temp directory: %s", err))
	}
	defer file.Close()
	if _, err := io.Copy(w, file); err != nil {
		pprofServeError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to copy memory profile contents: %s", err))
	}
}

func durationExceedsWriteTimeout(r *http.Request, seconds float64) bool {
	srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server)
	return ok && srv.WriteTimeout != 0 && seconds >= srv.WriteTimeout.Seconds()
}

func pprofServeError(w http.ResponseWriter, status int, txt string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Go-Pprof", "1")
	w.Header().Del("Content-Disposition")
	w.WriteHeader(status)
	fmt.Fprintln(w, txt)
}

func pprofSleep(r *http.Request, d time.Duration) {
	select {
	case <-time.After(d):
	case <-r.Context().Done():
	}
}
