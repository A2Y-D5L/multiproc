package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// defaultShutdownTimeout is the default time to wait for graceful process termination.
	defaultShutdownTimeout = 5 * time.Second

	// scannerInitialBufferSize is the initial buffer size for the line scanner.
	scannerInitialBufferSize = 64 * 1024 // 64KB

	// scannerMaxBufferSize is the maximum buffer size for the line scanner.
	scannerMaxBufferSize = 1024 * 1024 // 1MB

	// streamGoRoutines is the number of goroutines spawned per process (stdout + stderr).
	streamGoRoutines = 2
)

// Run executes all configured processes concurrently and emits ProcessLine events
// to the output channel. This is the main entry point for the Engine.
//
// Behavior:
//   - Spawns one goroutine per process in Specs
//   - Each goroutine captures stdout and stderr, emitting line events
//   - Handles graceful shutdown when context is cancelled
//   - Closes the output channel when all processes complete
//   - Blocks until all processes finish or are terminated
//
// Event sequence per process:
//  1. Zero or more line events (ProcessLine with IsComplete=false)
//  2. Exactly one completion event (ProcessLine with IsComplete=true)
//
// Graceful shutdown:
//   - When ctx is cancelled, sends SIGTERM to all running processes
//   - Waits up to ShutdownTimeout for graceful termination
//   - Sends SIGKILL to force termination of unresponsive processes
//
// Parameters:
//   - ctx: Context for cancellation and lifecycle management
//   - output: Channel to receive ProcessLine events (caller should buffer appropriately)
//
// The output channel is closed when Run() completes, allowing for:
//
//	for pl := range output {
//	    // Process events
//	}
//
// Example:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	output := make(chan engine.ProcessLine, 128)
//	go eng.Run(ctx, output)
//
//	for pl := range output {
//	    if pl.IsComplete {
//	        fmt.Printf("Process %d done: %v\n", pl.Index, pl.Err)
//	    } else {
//	        fmt.Printf("[%s] %s\n", eng.Specs[pl.Index].Name, pl.Line)
//	    }
//	}
func (eng *Engine) Run(ctx context.Context, output chan<- ProcessLine) {
	defer close(output)

	factory := eng.CommandFactory
	if factory == nil {
		factory = DefaultCommandFactory
	}

	var wg sync.WaitGroup
	for i, spec := range eng.Specs {
		wg.Add(1)
		go eng.runProcess(ctx, i, spec, factory, output, &wg)
	}

	wg.Wait()
}

// streamReader reads from a pipe line-by-line and emits ProcessLine events.
// This is a helper function for runProcess to reduce complexity.
func streamReader(scanner *bufio.Scanner, idx int, output chan<- ProcessLine, wg *sync.WaitGroup) {
	defer wg.Done()

	// Increase buffer size for long lines.
	buf := make([]byte, 0, scannerInitialBufferSize)
	scanner.Buffer(buf, scannerMaxBufferSize)

	for scanner.Scan() {
		line := scanner.Text()
		// Normalize line endings for cross-platform compatibility.
		line = strings.TrimRight(line, "\r\n")
		output <- ProcessLine{
			Index:      idx,
			Line:       line,
			IsComplete: false,
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		output <- ProcessLine{
			Index:      idx,
			Line:       fmt.Sprintf("[stream error: %v]", err),
			IsComplete: false,
		}
	}
}

// handleGracefulShutdown manages the graceful shutdown sequence for a process.
// Returns true if handled shutdown, false if process completed normally.
func (eng *Engine) handleGracefulShutdown(
	ctx context.Context,
	idx int,
	cmd Command,
	done <-chan error,
	output chan<- ProcessLine,
) bool {
	shutdownTimeout := eng.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	select {
	case waitErr := <-done:
		// Process completed normally before cancellation.
		output <- ProcessLine{
			Index:      idx,
			IsComplete: true,
			Err:        waitErr,
		}
		return false

	case <-ctx.Done():
		// Context cancelled - initiate graceful shutdown.
		cause := context.Cause(ctx)
		if cause != nil && !errors.Is(cause, context.Canceled) {
			output <- ProcessLine{
				Index: idx,
				Line:  fmt.Sprintf("[cancellation: %v]", cause),
			}
		}

		// Try graceful termination with SIGTERM first.
		proc := cmd.Process()
		if proc != nil {
			output <- ProcessLine{
				Index: idx,
				Line:  "[sending SIGTERM for graceful shutdown...]",
			}
			_ = proc.Signal(syscall.SIGTERM)

			// Wait for graceful shutdown with timeout.
			select {
			case waitErr := <-done:
				output <- ProcessLine{
					Index: idx,
					Line:  "[gracefully terminated]",
				}
				output <- ProcessLine{
					Index:      idx,
					IsComplete: true,
					Err:        waitErr,
				}

			case <-time.After(shutdownTimeout):
				// Timeout exceeded, force kill.
				output <- ProcessLine{
					Index: idx,
					Line:  fmt.Sprintf("[graceful shutdown timeout (%v), force killing...]", shutdownTimeout),
				}
				_ = proc.Kill()

				// Wait for kill to complete.
				waitErr := <-done
				output <- ProcessLine{
					Index: idx,
					Line:  "[force killed]",
				}
				output <- ProcessLine{
					Index:      idx,
					IsComplete: true,
					Err:        waitErr,
				}
			}
		} else {
			// Process already exited, just emit the done event.
			waitErr := <-done
			output <- ProcessLine{
				Index:      idx,
				IsComplete: true,
				Err:        waitErr,
			}
		}
		return true
	}
}

// runProcess executes a single process and emits its output as ProcessLine events.
// This function is called concurrently for each process in the Specs slice.
//
// Lifecycle:
//  1. Create command using CommandFactory
//  2. Set up stdout and stderr pipes
//  3. Start the process
//  4. Spawn goroutines to read from stdout and stderr
//  5. Monitor for process completion or context cancellation
//  6. Handle graceful shutdown on cancellation
//  7. Emit final completion event
//
// Error handling:
//   - Command creation errors: Emit completion event with error
//   - Pipe setup errors: Emit completion event with error
//   - Start errors: Emit completion event with error
//   - Stream read errors: Emit line event with error message
//   - Process exit errors: Included in completion event
//
// Graceful shutdown sequence:
//  1. Send SIGTERM to process
//  2. Wait up to ShutdownTimeout
//  3. If timeout expires, send SIGKILL
//  4. Emit status messages at each step
//
// This function always emits exactly one completion event, even if errors occur.
func (eng *Engine) runProcess(
	ctx context.Context,
	idx int,
	spec ProcessSpec,
	factory CommandFactory,
	output chan<- ProcessLine,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	cmd, err := factory(ctx, spec)
	if err != nil {
		output <- ProcessLine{
			Index:      idx,
			IsComplete: true,
			Err:        fmt.Errorf("create command: %w", err),
		}
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		output <- ProcessLine{
			Index:      idx,
			IsComplete: true,
			Err:        fmt.Errorf("stdout pipe: %w", err),
		}
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		output <- ProcessLine{
			Index:      idx,
			IsComplete: true,
			Err:        fmt.Errorf("stderr pipe: %w", err),
		}
		return
	}

	if startErr := cmd.Start(); startErr != nil {
		output <- ProcessLine{
			Index:      idx,
			IsComplete: true,
			Err:        fmt.Errorf("start: %w", startErr),
		}
		return
	}

	var streamsWG sync.WaitGroup
	streamsWG.Add(streamGoRoutines)

	go streamReader(bufio.NewScanner(stdout), idx, output, &streamsWG)
	go streamReader(bufio.NewScanner(stderr), idx, output, &streamsWG)

	// Monitor for process completion and context cancellation concurrently.
	done := make(chan error, 1)
	go func() {
		streamsWG.Wait()
		done <- cmd.Wait()
	}()

	eng.handleGracefulShutdown(ctx, idx, cmd, done, output)
}
