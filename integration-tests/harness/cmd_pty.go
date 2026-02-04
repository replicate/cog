package harness

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/rogpeppe/go-internal/testscript"
)

// PtyRunCommand implements the 'pty-run' command for testscript.
type PtyRunCommand struct {
	harness *Harness
}

func (c *PtyRunCommand) Name() string { return "pty-run" }

// Run executes a command with a PTY, sending input from a file and capturing output.
//
// Usage: pty-run <input-file> <command> [args...]
//
// The input file contents are written to the PTY as terminal input.
// Use /dev/null or an empty file if no input is needed.
// The command's output is written to stdout for matching with 'stdout' command.
//
// This uses github.com/creack/pty which works on both Linux and macOS,
// unlike testscript's native ttyin/ttyout which hangs on macOS due to
// Go bug https://github.com/golang/go/issues/61779.
//
// TODO: Remove this implementation and use testscript's native ttyin/ttyout
// once the Go bug is fixed (check Go 1.26+).
func (c *PtyRunCommand) Run(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("pty-run: usage: pty-run <input-file> <command> [args...]")
	}

	inputFile := args[0]
	cmdName := args[1]
	cmdArgs := args[2:]

	// Read input file
	var input string
	if inputFile != "/dev/null" {
		input = ts.ReadFile(inputFile)
	}

	// Expand environment variables in command and args
	cmdName = os.Expand(cmdName, ts.Getenv)

	// Handle "cog" command specially - use the resolved binary
	if cmdName == "cog" {
		cmdName = c.harness.CogBinary
	}

	expandedArgs := make([]string, len(cmdArgs))
	for i, arg := range cmdArgs {
		expandedArgs[i] = os.Expand(arg, ts.Getenv)
	}

	// Create the command
	cmd := exec.Command(cmdName, expandedArgs...)
	cmd.Dir = ts.Getenv("WORK")

	// Build environment
	cmd.Env = []string{
		"HOME=" + ts.Getenv("HOME"),
		"PATH=" + ts.Getenv("PATH"),
		"COG_NO_UPDATE_CHECK=1",
	}
	if v := ts.Getenv("COG_WHEEL"); v != "" {
		cmd.Env = append(cmd.Env, "COG_WHEEL="+v)
	}
	if v := ts.Getenv("COGLET_RUST_WHEEL"); v != "" {
		cmd.Env = append(cmd.Env, "COGLET_RUST_WHEEL="+v)
	}

	// Start command with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		ts.Fatalf("pty-run: failed to start command with PTY: %v", err)
	}
	defer ptmx.Close()

	// Set terminal size
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		ts.Logf("pty-run: failed to set terminal size: %v", err)
	}

	// Use shared buffer pattern for reading (avoids race conditions)
	var buf bytes.Buffer
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		tmp := make([]byte, 1024)
		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(tmp)
				if n > 0 {
					mu.Lock()
					buf.Write(tmp[:n])
					mu.Unlock()
				}
				if err != nil {
					if err != io.EOF {
						// Log non-EOF errors but don't fail - PTY may close unexpectedly
						ts.Logf("pty-run: read error (may be normal): %v", err)
					}
					return
				}
			}
		}
	}()

	// Write input to PTY with small delays between lines for reliability
	if input != "" {
		lines := strings.Split(input, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			_, err := ptmx.Write([]byte(line + "\n"))
			if err != nil {
				ts.Logf("pty-run: failed to write input (may be normal if command exited): %v", err)
				break
			}
			// Small delay to let the shell process the line
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Wait for command to finish with timeout
	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	timeout := 60 * time.Second
	var cmdErr error

	select {
	case cmdErr = <-cmdDone:
		// Command finished
	case <-time.After(timeout):
		// Timeout - kill the process
		cmd.Process.Kill()
		mu.Lock()
		output := buf.String()
		mu.Unlock()
		ts.Logf("pty-run: timeout after %v, partial output: %q", timeout, output)
		ts.Fatalf("pty-run: command timed out after %v", timeout)
		return
	}

	// Give a moment for final output to be captured
	time.Sleep(100 * time.Millisecond)
	close(done)

	// Get final output
	mu.Lock()
	output := buf.String()
	mu.Unlock()

	// Handle negation
	if neg {
		if cmdErr == nil {
			ts.Fatalf("pty-run: command succeeded unexpectedly")
		}
		// Command failed as expected - write output for potential pattern matching
		ts.Stdout().Write([]byte(output))
		return
	}

	if cmdErr != nil {
		ts.Logf("pty-run: command output: %q", output)
		ts.Fatalf("pty-run: command failed: %v", cmdErr)
	}

	// Write output to stdout for pattern matching
	ts.Stdout().Write([]byte(output))
}
