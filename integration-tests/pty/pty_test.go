package pty_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/replicate/cog/integration-tests/harness"
)

// TestInteractiveTTY tests that `cog run /bin/bash` works with an interactive TTY.
//
// This test verifies:
// 1. cog run can spawn an interactive shell
// 2. The PTY is properly connected for input/output
// 3. Commands can be sent and output received
//
// This test is written in Go (not txtar) because it requires bidirectional PTY
// interaction that doesn't fit txtar's sequential execution model.
func TestInteractiveTTY(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	// PTY tests only work reliably on Unix-like systems
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	// Create a temp directory for our test project
	tmpDir, err := os.MkdirTemp("", "cog-pty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write minimal cog.yaml
	cogYAML := `build:
  python_version: "3.13"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(cogYAML), 0644); err != nil {
		t.Fatalf("failed to write cog.yaml: %v", err)
	}

	// Get the cog binary
	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	// Create the command
	cmd := exec.Command(cogBinary, "run", "/bin/bash")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	// Start with a PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start PTY: %v", err)
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Set a reasonable terminal size
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Logf("failed to set terminal size: %v", err)
	}

	// Helper to read output with timeout
	readWithTimeout := func(timeout time.Duration) (string, error) {
		done := make(chan struct{})
		var buf bytes.Buffer

		go func() {
			tmp := make([]byte, 1024)
			for {
				select {
				case <-done:
					return
				default:
					ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
					n, err := ptmx.Read(tmp)
					if n > 0 {
						buf.Write(tmp[:n])
					}
					if err != nil {
						if !os.IsTimeout(err) && err != io.EOF {
							return
						}
					}
				}
			}
		}()

		time.Sleep(timeout)
		close(done)
		return buf.String(), nil
	}

	// Helper to wait for specific output pattern with timeout
	// Polls the PTY output and returns early when the pattern is found
	waitForOutput := func(pattern string, timeout time.Duration) (string, error) {
		deadline := time.Now().Add(timeout)
		var buf bytes.Buffer
		tmp := make([]byte, 1024)

		for time.Now().Before(deadline) {
			ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, err := ptmx.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				// Check if we've received the pattern
				if strings.Contains(buf.String(), pattern) {
					return buf.String(), nil
				}
			}
			if err != nil && !os.IsTimeout(err) && err != io.EOF {
				return buf.String(), fmt.Errorf("read error: %w", err)
			}
		}
		return buf.String(), fmt.Errorf("timeout waiting for pattern %q", pattern)
	}

	// Wait for bash to start and show a prompt
	// In CI, the Docker image build can take a while, so we need a longer timeout
	// Use waitForOutput which polls and returns early when the prompt appears
	t.Log("Waiting for bash prompt...")
	output, err := waitForOutput(":/src#", 60*time.Second)
	if err != nil {
		t.Fatalf("failed waiting for bash prompt: %v", err)
	}
	t.Logf("Initial output: %q", output)

	// Send a command
	t.Log("Sending 'echo OK' command...")
	_, err = ptmx.Write([]byte("echo OK\n"))
	if err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	// Read the response
	output, err = readWithTimeout(3 * time.Second)
	if err != nil {
		t.Fatalf("failed to read command output: %v", err)
	}
	t.Logf("Command output: %q", output)

	// Verify we got "OK" in the output
	if !strings.Contains(output, "OK") {
		t.Errorf("expected output to contain 'OK', got: %q", output)
	}

	// Send exit command
	t.Log("Sending 'exit' command...")
	_, err = ptmx.Write([]byte("exit\n"))
	if err != nil {
		t.Logf("failed to write exit command (may be expected): %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			// Exit code from bash is expected
			t.Logf("Process exited: %v", err)
		} else {
			t.Log("Process exited cleanly")
		}
	case <-time.After(5 * time.Second):
		t.Log("Process did not exit within timeout, killing...")
		cmd.Process.Kill()
	}
}

// TestInteractiveTTYEchoCommand is a simpler variant that just tests echo works
func TestInteractiveTTYEchoCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "cog-pty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cogYAML := `build:
  python_version: "3.13"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(cogYAML), 0644); err != nil {
		t.Fatalf("failed to write cog.yaml: %v", err)
	}

	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	// Use cog run with a simple echo command instead of interactive bash
	// This is simpler and tests the basic PTY functionality
	cmd := exec.Command(cogBinary, "run", "echo", "hello from cog run")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start PTY: %v", err)
	}
	defer ptmx.Close()

	// Read all output
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		io.Copy(&buf, ptmx)
		done <- nil
	}()

	// Wait for command to complete
	err = cmd.Wait()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for output")
	}

	output := buf.String()
	t.Logf("Output: %q", output)

	if !strings.Contains(output, "hello from cog run") {
		t.Errorf("expected output to contain 'hello from cog run', got: %q", output)
	}
}
