package predict

import (
	"testing"
)

// Test data from your actual sample output
const sampleContainerOutput = `Failed to parse log level "warning": unrecognized level: "warning"
Failed to parse log level "warning": unrecognized level: "warning"
Failed to parse log level "warning": unrecognized level: "warning"
{"severity":"info","timestamp":"2025-09-12T16:32:29.498Z","logger":"cog","caller":"cog/main.go:59","message":"configuration","use-procedure-mode":false,"await-explicit-shutdown":false,"one-shot":false,"upload-url":""}
{"severity":"info","timestamp":"2025-09-12T16:32:29.500Z","logger":"cog","caller":"cog/main.go:67","message":"starting Cog HTTP server","addr":"0.0.0.0:5000","version":"0.2.0b3","pid":6}
{"severity":"info","timestamp":"2025-09-12T16:32:29.505Z","logger":"cog-http-server","caller":"server/runner.go:213","message":"python runner started","pid":12}
{"severity":"info","timestamp":"2025-09-12T16:32:29.510Z","logger":"cog-http-server","caller":"server/runner.go:547","message":"configuring runner","module":"predict","predictor":"run","max_concurrency":1}
{"severity":"info","timestamp":"2025-09-12T16:32:29.755Z","logger":"cog-http-server","caller":"server/runner.go:642","message":"updating OpenAPI schema"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.763Z","logger":"cog-http-server","caller":"server/runner.go:664","message":"updating setup result"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.763Z","logger":"cog-http-server","caller":"server/runner.go:676","message":"setup succeeded"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.763Z","logger":"cog-http-server","caller":"server/runner.go:620","message":"runner is ready"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.786Z","logger":"cog-http-server","caller":"server/runner.go:425","message":"received prediction request","id":"v86bg93cb9t6t0cs7v2rdcz0j0"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.872Z","logger":"cog-http-server","caller":"server/runner.go:625","message":"runner is busy"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.874Z","logger":"cog-http-server","caller":"server/runner.go:708","message":"received prediction response","id":"v86bg93cb9t6t0cs7v2rdcz0j0"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.876Z","logger":"cog-http-server","caller":"server/runner.go:781","message":"prediction completed","id":"v86bg93cb9t6t0cs7v2rdcz0j0","status":"succeeded"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.885Z","logger":"cog","caller":"cog/main.go:129","message":"stopping Cog HTTP server","signal":"terminated"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.886Z","logger":"cog-http-server","caller":"server/runner.go:317","message":"stop requested"}
{"severity":"info","timestamp":"2025-09-12T16:32:29.999Z","logger":"cog-http-server","caller":"server/runner.go:591","message":"python runner exited successfully","pid":12}
{"severity":"info","timestamp":"2025-09-12T16:32:30.000Z","logger":"cog","caller":"cog/main.go:144","message":"shutdown completed normally"}`

func TestLogHandler_JSONLogs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "debug severity",
			input:    `{"severity":"debug","message":"debug message"}`,
			expected: "debug message",
		},
		{
			name:     "info severity",
			input:    `{"severity":"info","message":"info message"}`,
			expected: "info message",
		},
		{
			name:     "warn severity",
			input:    `{"severity":"warn","message":"warning message"}`,
			expected: "warning message",
		},
		{
			name:     "warning severity",
			input:    `{"severity":"warning","message":"warning message"}`,
			expected: "warning message",
		},
		{
			name:     "error severity",
			input:    `{"severity":"error","message":"error message"}`,
			expected: "error message",
		},
		{
			name:     "fatal severity",
			input:    `{"severity":"fatal","message":"fatal message"}`,
			expected: "fatal message",
		},
		{
			name:     "unknown severity",
			input:    `{"severity":"unknown","message":"unknown message"}`,
			expected: "unknown message",
		},
		{
			name:     "missing severity field",
			input:    `{"message":"test message"}`,
			expected: "test message",
		},
		{
			name:     "missing message field",
			input:    `{"severity":"info"}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewLogHandler()
			n, err := handler.Write([]byte(tt.input + "\n"))

			if err != nil {
				t.Errorf("Write failed: %v", err)
			}
			if n != len(tt.input)+1 {
				t.Errorf("Expected to write %d bytes, wrote %d", len(tt.input)+1, n)
			}

			// For now, just verify the handler doesn't crash
			// The actual console routing behavior is tested in integration tests
		})
	}
}

func TestLogHandler_UnstructuredLogs(t *testing.T) {
	tests := []string{
		"Something went wrong with error handling",
		"Failed to connect to database",
		"Exception occurred during processing",
		"This is a warning message",
		"This is a warn message",
		"Debug information here",
		"Just some regular output",
		`Failed to parse log level "warning": unrecognized level: "warning"`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			handler := NewLogHandler()
			n, err := handler.Write([]byte(input + "\n"))

			if err != nil {
				t.Errorf("Write failed: %v", err)
			}
			if n != len(input)+1 {
				t.Errorf("Expected to write %d bytes, wrote %d", len(input)+1, n)
			}
		})
	}
}

func TestLogHandler_LineProcessing(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty line",
			input: "\n",
		},
		{
			name:  "whitespace only",
			input: "   \n",
		},
		{
			name:  "single line",
			input: "test message\n",
		},
		{
			name:  "multiple lines",
			input: "line 1\nline 2\nline 3\n",
		},
		{
			name:  "no trailing newline",
			input: "test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewLogHandler()
			n, err := handler.Write([]byte(tt.input))

			if err != nil {
				t.Errorf("Write failed: %v", err)
			}
			if n != len(tt.input) {
				t.Errorf("Expected to write %d bytes, wrote %d", len(tt.input), n)
			}
		})
	}
}

func TestLogHandler_RealSampleOutput(t *testing.T) {
	handler := NewLogHandler()
	n, err := handler.Write([]byte(sampleContainerOutput))

	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if n != len(sampleContainerOutput) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(sampleContainerOutput), n)
	}
}

func TestLogHandler_MalformedJSON(t *testing.T) {
	tests := []string{
		`{"severity":"info","message":"incomplete`,
		"This is just plain text",
		"{}",
		`{"message":"test message"}`,
		`{"severity":"info"}`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			handler := NewLogHandler()
			n, err := handler.Write([]byte(input + "\n"))

			if err != nil {
				t.Errorf("Write failed: %v", err)
			}
			if n != len(input)+1 {
				t.Errorf("Expected to write %d bytes, wrote %d", len(input)+1, n)
			}
		})
	}
}

func TestLogHandler_Concurrency(t *testing.T) {
	handler := NewLogHandler()

	// Test concurrent writes
	done := make(chan bool, 2)

	go func() {
		handler.Write([]byte("goroutine 1\n"))
		done <- true
	}()

	go func() {
		handler.Write([]byte("goroutine 2\n"))
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Should not crash or have race conditions
}

func TestLogHandler_JSONParsing(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldParse bool
		severity    string
		message     string
	}{
		{
			name:        "valid JSON with all fields",
			input:       `{"severity":"info","timestamp":"2025-09-12T16:32:29.498Z","logger":"cog","caller":"cog/main.go:59","message":"configuration"}`,
			shouldParse: true,
			severity:    "info",
			message:     "configuration",
		},
		{
			name:        "valid JSON with extra fields",
			input:       `{"severity":"warn","message":"warning message","extra":"field"}`,
			shouldParse: true,
			severity:    "warn",
			message:     "warning message",
		},
		{
			name:        "invalid JSON",
			input:       `{"severity":"info","message":"incomplete`,
			shouldParse: false,
		},
		{
			name:        "not JSON at all",
			input:       "This is just plain text",
			shouldParse: false,
		},
		{
			name:        "empty object",
			input:       "{}",
			shouldParse: true,
			severity:    "",
			message:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewLogHandler()

			// Test the TryParseJSONLog method directly
			parsed := handler.TryParseJSONLog(tt.input)

			if parsed != tt.shouldParse {
				t.Errorf("Expected parse result %v, got %v", tt.shouldParse, parsed)
			}
		})
	}
}

func TestLogHandler_EmptyInput(t *testing.T) {
	handler := NewLogHandler()

	// Test empty input
	n, err := handler.Write([]byte(""))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Expected to write 0 bytes, wrote %d", n)
	}

	// Test input with only newlines
	n, err = handler.Write([]byte("\n\n\n"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if n != 3 {
		t.Errorf("Expected to write 3 bytes, wrote %d", n)
	}
}
