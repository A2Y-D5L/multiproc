// Package runner provides high-level orchestration for running multiple processes
// concurrently with automatic rendering and output management.
//
// This package ties together the engine (process execution) and renderer
// (output formatting) layers to provide a simple, batteries-included API
// for most use cases.
//
// Architecture:
//   - Config: High-level configuration with sensible defaults
//   - Run(): Main entry point that coordinates everything
//   - Automatic TTY detection and renderer selection
//   - Render debouncing for performance
//   - Event conversion between layers
//
// Quick start:
//
//	cfg := runner.DefaultConfig()
//	cfg.Specs = []engine.ProcessSpec{
//	    {Name: "build", Command: "go", Args: []string{"build"}},
//	    {Name: "test", Command: "go", Args: []string{"test"}},
//	}
//	exitCode := runner.Run(ctx, cfg)
//	os.Exit(exitCode)
//
// For more control, use the engine and renderer packages directly.
package runner //nolint:cyclop // Package complexity is expected for high-level orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
	"github.com/a2y-d5l/multiproc/renderer"
)

const (
	// defaultMaxLinesPerProc is the default maximum number of output lines to keep per process.
	defaultMaxLinesPerProc = 1000

	// defaultShutdownTimeout is the default time to wait for graceful shutdown.
	defaultShutdownTimeout = 5 * time.Second

	// eventChannelBuffer is the buffer size for event channels.
	eventChannelBuffer = 128
)

// Config holds high-level configuration for running multiple processes.
// This provides a simplified interface that covers most common use cases.
//
// All fields are optional and will be populated with sensible defaults
// from DefaultConfig() if not specified.
//
// Example:
//
//	cfg := runner.DefaultConfig()
//	cfg.Specs = mySpecs
//	cfg.ShowTimestamps = true
//	cfg.MaxLinesPerProc = 500
type Config struct {
	// IsTTY indicates whether stdout is attached to a TTY (interactive terminal).
	// When nil, the value is auto-detected using renderer.IsTTY().
	//
	// Override this to force a particular render mode:
	//   - Set to &true to force full-screen TTY rendering
	//   - Set to &false to force incremental line-by-line rendering
	//
	// Auto-detection works well in most cases. Manual override is primarily
	// useful for testing or special environments.
	//
	// Example (force non-TTY mode):
	//   val := false
	//   cfg.IsTTY = &val
	IsTTY *bool

	// LogPrefix defines the format for prefixing process names in non-TTY mode.
	// Must be a format string containing exactly one "%s" placeholder.
	//
	// Common formats:
	//   - "[%s]"  → [ProcessName] line      (default)
	//   - "%s:"   → ProcessName: line
	//   - "(%s)"  → (ProcessName) line
	//   - "» %s »" → » ProcessName » line
	//
	// The prefix helps distinguish output from different processes when
	// their output is interleaved.
	//
	// If empty, defaults to "[%s]".
	LogPrefix string

	// Specs defines the processes to run.
	// Each ProcessSpec describes one subprocess to execute concurrently.
	//
	// Processes are started in the order specified, though execution is
	// concurrent so completion order may differ.
	//
	// Example:
	//   cfg.Specs = []engine.ProcessSpec{
	//       {Name: "lint", Command: "golangci-lint", Args: []string{"run"}},
	//       {Name: "test", Command: "go", Args: []string{"test", "./..."}},
	//   }
	Specs []engine.ProcessSpec

	// MaxLinesPerProc is the default maximum number of output lines to keep per process.
	// Individual processes can override this with ProcessSpec.MaxLines.
	//
	// When this limit is exceeded, the oldest lines are evicted (FIFO).
	// Set to 0 to use the package default (1000 lines).
	//
	// Memory usage is approximately: NumProcesses × MaxLinesPerProc × AvgLineLength
	//
	// Example: With 10 processes, 1000 lines, and 100 bytes/line = ~1MB
	MaxLinesPerProc int

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// before force-killing processes.
	//
	// When the context is cancelled (Ctrl+C, SIGTERM, etc.):
	//  1. Send SIGTERM to all running processes
	//  2. Wait up to ShutdownTimeout for graceful exit
	//  3. Send SIGKILL to forcefully terminate remaining processes
	//
	// If zero or negative, uses a default of 5 seconds.
	//
	// Adjust based on your processes:
	//   - Fast processes: 1-3 seconds
	//   - Database operations: 10-30 seconds
	//   - Complex cleanup: 30-60 seconds
	ShutdownTimeout time.Duration

	// FullScreen enables full-screen terminal rendering with screen clearing.
	// Only used when IsTTY is true.
	//
	// When true (default):
	//   - Clears screen and re-renders on each update
	//   - Process output is visually grouped
	//   - Good for interactive terminal use
	//
	// When false:
	//   - Uses incremental line-by-line rendering
	//   - Output appears immediately
	//   - Good for logging and debugging
	//
	// This is automatically forced to false in non-TTY environments.
	FullScreen bool

	// ShowSummary enables printing a summary to stderr after execution completes.
	//
	// The summary shows the final status of each process:
	//   Summary:
	//     - build: ok
	//     - test: exit code 1
	//
	// Useful when:
	//   - Output is long and you need quick overview
	//   - Running in background or via automation
	//   - Debugging failures across multiple processes
	ShowSummary bool

	// ShowTimestamps prefixes each output line with an RFC3339 timestamp.
	// Only applies to incremental (non-TTY) rendering mode.
	//
	// Format: [2024-11-20T15:30:45Z] [ProcessName] line content
	//
	// Useful for:
	//   - Debugging timing issues
	//   - Analyzing slow commands
	//   - Correlating logs across processes
	//   - Performance profiling
	//
	// Timestamps are in UTC for consistency across time zones.
	ShowTimestamps bool
}

// DefaultConfig returns sensible defaults for Config.
// Callers can override individual fields as needed.
//
// Defaults:
//   - IsTTY: nil (auto-detect)
//   - MaxLinesPerProc: 1000
//   - ShutdownTimeout: 5 seconds
//   - FullScreen: true
//   - ShowSummary: true
//   - ShowTimestamps: false
//   - LogPrefix: "[%s]"
//
// Example:
//
//	cfg := runner.DefaultConfig()
//	cfg.Specs = mySpecs           // Required: set your processes
//	cfg.ShowTimestamps = true     // Optional: enable timestamps
//	cfg.MaxLinesPerProc = 500     // Optional: reduce memory usage
func DefaultConfig() Config {
	return Config{
		Specs:           nil,
		MaxLinesPerProc: defaultMaxLinesPerProc,
		FullScreen:      true,
		ShowSummary:     true,
		IsTTY:           nil,
		ShutdownTimeout: defaultShutdownTimeout,
		ShowTimestamps:  false,
		LogPrefix:       "[%s]",
	}
}

// Run executes the configured processes and manages rendering.
// This is the main entry point for the runner package.
//
// Orchestration:
//  1. Apply configuration defaults
//  2. Initialize process states
//  3. Create and start engine
//  4. Set up appropriate renderer (TTY or non-TTY)
//  5. Process events and update state
//  6. Render updates in real-time
//  7. Print summary (if enabled)
//  8. Return aggregate exit code
//
// Rendering modes:
//   - TTY + FullScreen: Full-screen with debouncing
//   - TTY + !FullScreen: Incremental line-by-line
//   - Non-TTY: Always incremental
//
// Lifecycle:
//   - Blocks until all processes complete or context is cancelled
//   - Handles graceful shutdown (SIGTERM → timeout → SIGKILL)
//   - Closes resources automatically
//
// Exit codes:
//   - 0: All processes succeeded
//   - 1: One or more processes failed
//
// Parameters:
//   - ctx: Context for cancellation (typically from signal handling)
//   - cfg: Configuration (use DefaultConfig() as starting point)
//
// Returns:
//   - int: Exit code suitable for os.Exit()
//
// Example (simple):
//
//	cfg := runner.DefaultConfig()
//	cfg.Specs = specs
//	os.Exit(runner.Run(ctx, cfg))
//
// Example (with signal handling):
//
//	ctx, cancel := context.WithCancelCause(context.Background())
//	defer cancel(nil)
//
//	sigCh := make(chan os.Signal, 1)
//	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
//	go func() {
//	    sig := <-sigCh
//	    cancel(fmt.Errorf("received signal: %v", sig))
//	}()
//
//	os.Exit(runner.Run(ctx, cfg))
//
// Example (custom configuration):
//
//	cfg := runner.DefaultConfig()
//	cfg.Specs = specs
//	cfg.ShowTimestamps = true        // Enable timing analysis
//	cfg.MaxLinesPerProc = 500        // Reduce memory usage
//	cfg.ShutdownTimeout = 10 * time.Second  // Slow cleanup
//	cfg.LogPrefix = "%s:"            // Custom prefix format
//
//	exitCode := runner.Run(ctx, cfg)
//	if exitCode != 0 {
//	    log.Printf("Execution failed with code %d", exitCode)
//	}
//
//nolint:gocognit,funlen // High-level orchestration requires conditional logic and length
func Run(ctx context.Context, cfg Config) int {
	// Derive effective configuration, falling back to defaults.
	base := DefaultConfig()
	if cfg.MaxLinesPerProc <= 0 {
		cfg.MaxLinesPerProc = base.MaxLinesPerProc
	}
	if cfg.Specs == nil {
		cfg.Specs = base.Specs
	}
	if cfg.IsTTY == nil {
		val := renderer.IsTTY()
		cfg.IsTTY = &val
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = base.ShutdownTimeout
	}
	if cfg.LogPrefix == "" {
		cfg.LogPrefix = base.LogPrefix
	}

	// In non-TTY environments, full-screen rendering is not useful, so
	// force it off. Incremental renderer will still run.
	if cfg.IsTTY != nil && !*cfg.IsTTY {
		cfg.FullScreen = false
	}

	specs := cfg.Specs

	// Build initial render state.
	states := make([]renderer.ProcessState, len(specs))
	for i, spec := range specs {
		// Determine effective limits for this process.
		maxLines := spec.MaxLines
		if maxLines <= 0 {
			maxLines = cfg.MaxLinesPerProc
		}
		maxBytes := spec.MaxBytes
		// Note: if maxBytes is 0, no byte limit is enforced.

		states[i] = renderer.ProcessState{
			Name:     spec.Name,
			Lines:    nil,
			Done:     false,
			Err:      nil,
			Running:  true,
			Dirty:    true, // initial state should be rendered
			ByteSize: 0,
			MaxLines: maxLines,
			MaxBytes: maxBytes,
		}
	}

	events := make(chan renderer.Event, eventChannelBuffer)

	// Use the Engine to run processes.
	eng := engine.New(specs, cfg.ShutdownTimeout)

	// Convert ProcessLine events from engine to Event for rendering.
	processLines := make(chan engine.ProcessLine, eventChannelBuffer)
	var engineWG sync.WaitGroup
	engineWG.Go(func() {
		eng.Run(ctx, processLines)
	})

	// Convert engine events to renderer events.
	go func() {
		for pl := range processLines {
			events <- renderer.ConvertProcessLineToEvent(pl)
		}
		close(events)
	}()

	var renderCh chan renderer.RenderRequest
	if cfg.FullScreen && cfg.IsTTY != nil && *cfg.IsTTY {
		renderCh = make(chan renderer.RenderRequest, 1)
		// Dedicated render loop with debouncing.
		go func() {
			for range renderCh {
				renderer.RenderScreen(states)
			}
		}()

		// Queue initial render to show "starting" status for all processes.
		renderCh <- renderer.RenderRequest{}
	} else if cfg.IsTTY != nil && !*cfg.IsTTY {
		// In non-TTY mode, print initial status for all processes
		for i, spec := range specs {
			name := spec.Name
			if name == "" {
				name = fmt.Sprintf("proc-%d", i)
			}
			prefix := fmt.Sprintf(cfg.LogPrefix, name)
			if cfg.ShowTimestamps {
				timestamp := time.Now().UTC().Format(time.RFC3339)
				fmt.Printf("[%s] %s starting...\n", timestamp, prefix)
			} else {
				fmt.Printf("%s starting...\n", prefix)
			}
		}
	}

	// Main event loop: update state and re-render in real time.
	for ev := range events {
		renderer.ApplyEvent(states, ev)
		if cfg.IsTTY != nil && *cfg.IsTTY && cfg.FullScreen {
			// Non-blocking send to debounce renders.
			select {
			case renderCh <- renderer.RenderRequest{}:
			default:
			}
		} else {
			// Non-TTY incremental renderer.
			renderer.RenderIncremental(ev, specs, states, cfg.ShowTimestamps, cfg.LogPrefix)
		}
	}

	// Final render (in case we exited without drawing the last frame).
	if renderCh != nil {
		// Ensure the last state is rendered, then close the loop.
		renderCh <- renderer.RenderRequest{}
		close(renderCh)
	}

	// Print a short summary to stderr.
	if cfg.ShowSummary {
		renderer.WriteFinalSummary(states)
	}

	// Return exit code for caller to handle.
	return renderer.ExitCodeFromStates(states)
}
