// Package renderer provides output formatting, state management, and rendering
// for multiprocess execution. It consumes ProcessLine events from the engine
// and produces formatted output for different environments.
//
// The renderer supports two modes:
//   - Full-screen TTY mode: Interactive display with screen clearing
//   - Incremental non-TTY mode: Line-by-line output for CI/logs
//
// Architecture:
//   - ProcessState: Maintains renderable state for each process
//   - Event system: Adapts engine events to renderer events
//   - ApplyEvent: Pure function for state updates
//   - Renderers: Format and display state
//
// Basic usage with engine events:
//
//	states := make([]renderer.ProcessState, len(specs))
//	// ... initialize states ...
//
//	for pl := range processLines {
//	    ev := renderer.ConvertProcessLineToEvent(pl)
//	    renderer.ApplyEvent(states, ev)
//	    renderer.RenderIncremental(ev, specs, states, false, "[%s]")
//	}
package renderer

import (
	"github.com/a2y-d5l/multiproc/engine"
)

// ProcessState holds renderable state for a subprocess.
// This structure is maintained by the renderer to track the current status
// and output history of each process.
//
// The state is updated by ApplyEvent() as engine events arrive, and consumed
// by renderers (RenderScreen, RenderIncremental) to produce formatted output.
//
// Memory management:
//   - Lines are stored in a slice (FIFO queue)
//   - Oldest lines are evicted when MaxLines or MaxBytes is exceeded
//   - ByteSize tracks total bytes to enforce byte limit
//
// Example initialization:
//
//	state := ProcessState{
//	    Name:     "build",
//	    Lines:    nil,
//	    Running:  true,
//	    Done:     false,
//	    MaxLines: 1000,
//	    MaxBytes: 100000,
//	    Dirty:    true,
//	}
type ProcessState struct {
	// Err contains the process exit error, if any.
	// Only meaningful when Done is true.
	// nil indicates successful exit (exit code 0).
	Err error

	// Name is the display name for this process.
	// Typically copied from ProcessSpec.Name.
	Name string

	// Lines contains the captured output (stdout + stderr merged).
	// Lines are appended as they arrive and evicted when limits are exceeded.
	// This is a FIFO queue: oldest lines are removed first.
	Lines []string

	// ByteSize is the total number of bytes currently stored in Lines.
	// Used to enforce MaxBytes limit. Updated automatically by ApplyEvent.
	ByteSize int

	// MaxLines is the maximum number of lines to keep for this process.
	// When exceeded, oldest lines are evicted. 0 means no limit.
	MaxLines int

	// MaxBytes is the maximum number of bytes to keep for this process.
	// When exceeded, oldest lines are evicted. 0 means no limit.
	// When both MaxLines and MaxBytes are set, lines are evicted when
	// EITHER limit is exceeded.
	MaxBytes int

	// Done is true when the process has exited (successfully or with error).
	// Once true, no more line events will be received for this process.
	Done bool

	// Running is true from process start until exit.
	// Opposite of Done, provided for convenience.
	Running bool

	// Dirty indicates whether this process state has changed since last render.
	// Set to true by ApplyEvent, cleared by renderer after displaying.
	// Used for performance optimization in full-screen rendering.
	Dirty bool
}

// Event is a marker interface for renderer events.
// All renderer event types implement this interface.
//
// Event types:
//   - lineEvent: Output line from a process
//   - doneEvent: Process completion/exit
//
// Events are created by ConvertProcessLineToEvent() from engine.ProcessLine
// and consumed by ApplyEvent() to update ProcessState.
type Event interface{ isEvent() }

// lineEvent represents a single line of output for one process.
// This is an internal event type used by the renderer.
type lineEvent struct {
	// Line contains the output text (already normalized for line endings).
	Line string

	// Index identifies which process emitted this line.
	Index int
}

func (lineEvent) isEvent() {}

// doneEvent signals that a process has exited.
// This is an internal event type used by the renderer.
type doneEvent struct {
	// Err contains the exit error, if any (nil for successful exit).
	Err error

	// Index identifies which process has exited.
	Index int
}

func (doneEvent) isEvent() {}

// ConvertProcessLineToEvent converts a ProcessLine from the engine to an Event for the renderer.
// This adapter function bridges the engine and renderer layers.
//
// Conversion logic:
//   - ProcessLine with IsComplete=true → doneEvent
//   - ProcessLine with IsComplete=false → lineEvent
//
// Parameters:
//   - pl: ProcessLine from engine
//
// Returns:
//   - Event: Corresponding renderer event
//
// Example:
//
//	for pl := range engineOutput {
//	    ev := renderer.ConvertProcessLineToEvent(pl)
//	    renderer.ApplyEvent(states, ev)
//	}
func ConvertProcessLineToEvent(pl engine.ProcessLine) Event {
	if pl.IsComplete {
		return doneEvent{Index: pl.Index, Err: pl.Err}
	}
	return lineEvent{Index: pl.Index, Line: pl.Line}
}

// ApplyEvent updates process state based on a renderer event.
// This is a pure function that mutates the states slice in-place.
//
// Behavior:
//   - lineEvent: Appends line to state, enforces memory limits, marks dirty
//   - doneEvent: Sets Done=true, Running=false, stores exit error, marks dirty
//
// Memory limit enforcement (lineEvent only):
//  1. Append new line to Lines slice
//  2. Add line byte count to ByteSize
//  3. While (lines > MaxLines OR bytes > MaxBytes):
//     - Remove oldest line from Lines
//     - Subtract its byte count from ByteSize
//  4. Mark state as Dirty
//
// The eviction loop ensures both constraints are satisfied, protecting against:
//   - Volume (many short lines exceeding MaxLines)
//   - Size (few very long lines exceeding MaxBytes)
//
// Parameters:
//   - states: Slice of ProcessState to update (mutated in-place)
//   - ev: Event to apply (lineEvent or doneEvent)
//
// Example:
//
//	states := make([]ProcessState, len(specs))
//	for ev := range events {
//	    renderer.ApplyEvent(states, ev)
//	    if needsRender {
//	        renderer.RenderScreen(states)
//	    }
//	}
func ApplyEvent(states []ProcessState, ev Event) {
	switch e := ev.(type) {
	case lineEvent:
		if e.Index < 0 || e.Index >= len(states) {
			return
		}
		ps := &states[e.Index]

		// Append line and track byte size.
		lineBytes := len(e.Line)
		ps.Lines = append(ps.Lines, e.Line)
		ps.ByteSize += lineBytes

		// Enforce limits: evict oldest lines if either limit is exceeded.
		// We need to keep removing lines until both constraints are satisfied.
		for {
			exceedsLineLimit := ps.MaxLines > 0 && len(ps.Lines) > ps.MaxLines
			exceedsByteLimit := ps.MaxBytes > 0 && ps.ByteSize > ps.MaxBytes

			if !exceedsLineLimit && !exceedsByteLimit {
				break
			}

			if len(ps.Lines) == 0 {
				break
			}

			// Remove the oldest line.
			oldestLine := ps.Lines[0]
			ps.Lines = ps.Lines[1:]
			ps.ByteSize -= len(oldestLine)
		}

		ps.Dirty = true

	case doneEvent:
		if e.Index < 0 || e.Index >= len(states) {
			return
		}
		ps := &states[e.Index]
		ps.Done = true
		ps.Running = false
		ps.Err = e.Err
		ps.Dirty = true
	}
}

// ExitCodeFromStates determines the appropriate exit code based on process states.
// This function is used to compute the final exit code for the overall execution.
//
// Logic:
//   - If any process has a non-nil Err, return 1 (failure)
//   - If all processes succeeded (Err == nil), return 0 (success)
//
// This follows standard Unix conventions where:
//   - 0 indicates success
//   - Non-zero indicates failure
//
// Parameters:
//   - states: Slice of ProcessState to examine
//
// Returns:
//   - int: Exit code (0 for success, 1 for failure)
//
// Example:
//
//	exitCode := renderer.ExitCodeFromStates(states)
//	os.Exit(exitCode)
func ExitCodeFromStates(states []ProcessState) int {
	for _, ps := range states {
		if ps.Err != nil {
			return 1
		}
	}
	return 0
}
