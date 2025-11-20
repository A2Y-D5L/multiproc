package renderer

import (
	"fmt"
	"strings"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
)

// RenderIncremental renders events directly to standard output without
// clearing the screen or buffering. This is the primary renderer for
// non-TTY environments such as CI/CD pipelines, log files, and piped output.
//
// Behavior:
//   - Processes events as they arrive (no buffering)
//   - Prefixes each line with process name for stream identification
//   - Optional timestamp prefixing for timing analysis
//   - Configurable prefix format for different environments
//   - No screen clearing or cursor manipulation
//
// Event handling:
//   - lineEvent: Print line with prefix and optional timestamp
//   - doneEvent: Print completion status with prefix
//
// Output format (without timestamps):
//
//	[ProcessName] output line 1
//	[ProcessName] output line 2
//	[ProcessName] ok
//
// Output format (with timestamps):
//
//	[2024-11-20T15:30:45Z] [ProcessName] output line 1
//	[2024-11-20T15:30:46Z] [ProcessName] output line 2
//	[2024-11-20T15:30:47Z] [ProcessName] ok
//
// Prefix format examples:
//   - "[%s]": [ProcessName] line
//   - "%s:": ProcessName: line
//   - "(%s)": (ProcessName) line
//   - ">>> %s >>>": >>> ProcessName >>> line
//
// Parameters:
//   - ev: Event to render (lineEvent or doneEvent)
//   - specs: Process specifications (for name lookup)
//   - states: Process states (reserved for future use)
//   - showTimestamps: If true, prefix lines with RFC3339 timestamp
//   - logPrefix: Format string for process name (must include "%s")
//
// Advantages for CI/CD:
//   - Output immediately visible (no buffering delay)
//   - Easily parseable by log aggregators
//   - Works with grep, awk, and other text tools
//   - Timestamps enable timing analysis
//   - No ANSI escape codes (clean logs)
//
// Example usage:
//
//	for ev := range events {
//	    renderer.RenderIncremental(ev, specs, states, true, "[%s]")
//	}
func RenderIncremental(ev Event, specs []engine.ProcessSpec, _ []ProcessState, showTimestamps bool, logPrefix string) {
	// Default prefix format if not specified
	if logPrefix == "" {
		logPrefix = "[%s]"
	}

	switch e := ev.(type) {
	case lineEvent:
		if e.Index < 0 || e.Index >= len(specs) {
			return
		}
		name := specs[e.Index].Name
		if name == "" {
			name = fmt.Sprintf("proc-%d", e.Index)
		}
		line := strings.TrimRight(e.Line, "\r\n")

		// Build the output line with optional timestamp and configurable prefix
		var output string
		if showTimestamps {
			timestamp := time.Now().UTC().Format(time.RFC3339)
			prefix := fmt.Sprintf(logPrefix, name)
			output = fmt.Sprintf("[%s] %s %s", timestamp, prefix, line)
		} else {
			prefix := fmt.Sprintf(logPrefix, name)
			output = fmt.Sprintf("%s %s", prefix, line)
		}
		fmt.Println(output)

	case doneEvent:
		if e.Index < 0 || e.Index >= len(specs) {
			return
		}
		name := specs[e.Index].Name
		if name == "" {
			name = fmt.Sprintf("proc-%d", e.Index)
		}
		status := FormatExitError(e.Err)

		// Build the completion message with optional timestamp
		var output string
		if showTimestamps {
			timestamp := time.Now().UTC().Format(time.RFC3339)
			prefix := fmt.Sprintf(logPrefix, name)
			output = fmt.Sprintf("[%s] %s %s", timestamp, prefix, status)
		} else {
			prefix := fmt.Sprintf(logPrefix, name)
			output = fmt.Sprintf("%s %s", prefix, status)
		}
		fmt.Println(output)
	}
}

// RenderRequest is a signal type used to trigger rendering in full-screen mode.
// This empty struct is sent through a channel to request a screen re-render.
//
// Usage pattern (debouncing):
//
//	renderCh := make(chan RenderRequest, 1)
//	go func() {
//	    for range renderCh {
//	        renderer.RenderScreen(states)
//	    }
//	}()
//
//	// Non-blocking send (debounces renders)
//	select {
//	case renderCh <- RenderRequest{}:
//	default:
//	}
//
// The buffer size of 1 ensures:
//   - At most one pending render request
//   - Renders are debounced during high-frequency output
//   - No blocking on event processing
//
// This pattern prevents excessive re-renders when processes produce output
// faster than the terminal can display it.
type RenderRequest struct{}
