// Package engine provides the core execution engine for running multiple
// processes concurrently. It is completely decoupled from any UI/rendering
// logic, making it reusable for different output formats (terminal UI,
// JSON logs, progress bars, metrics collection, etc.).
//
// The engine handles:
//   - Process creation and lifecycle management
//   - Stdout/stderr stream capture
//   - Line ending normalization (cross-platform)
//   - Graceful shutdown (SIGTERM → timeout → SIGKILL)
//   - Error propagation and exit code handling
//
// Basic usage:
//
//	specs := []engine.ProcessSpec{
//	    {Name: "build", Command: "go", Args: []string{"build", "./..."}},
//	    {Name: "test", Command: "go", Args: []string{"test", "./..."}},
//	}
//	eng := engine.New(specs, 5*time.Second)
//	output := make(chan engine.ProcessLine, 128)
//
//	go eng.Run(ctx, output)
//	for pl := range output {
//	    if pl.IsComplete {
//	        fmt.Printf("Process %d done: %v\n", pl.Index, pl.Err)
//	    } else {
//	        fmt.Printf("[%d] %s\n", pl.Index, pl.Line)
//	    }
//	}
package engine

import (
	"io"
	"syscall"
)

// ProcessLine represents a single line of output or completion event from a process.
// This is the raw event emitted by the Engine and consumed by renderers or
// custom output handlers.
//
// ProcessLines are emitted in the following sequence for each process:
//  1. Zero or more line events (IsComplete=false, Line contains output)
//  2. Exactly one completion event (IsComplete=true, Err contains exit status)
//
// Example handling:
//
//	for pl := range output {
//	    if pl.IsComplete {
//	        if pl.Err != nil {
//	            fmt.Printf("Process %d failed: %v\n", pl.Index, pl.Err)
//	        } else {
//	            fmt.Printf("Process %d succeeded\n", pl.Index)
//	        }
//	    } else {
//	        fmt.Printf("[%d] %s\n", pl.Index, pl.Line)
//	    }
//	}
type ProcessLine struct {
	// Err contains the process exit error, if any.
	// Only meaningful when IsComplete is true.
	// Will be nil if the process exited successfully (exit code 0).
	// May be an *exec.ExitError containing the exit code and signal information.
	Err error

	// Line contains the actual output text (already normalized for line endings).
	// Only meaningful when IsComplete is false.
	// Line endings (CRLF/LF/CR) are stripped for cross-platform consistency.
	Line string

	// Index identifies which process emitted this event.
	// It corresponds to the position in the ProcessSpec slice passed to Engine.
	Index int

	// IsComplete indicates whether this is the final event for this process.
	// When true, the process has exited and Err contains the exit status.
	// When false, this is a regular output line and Line contains the text.
	IsComplete bool
}

// ProcessSpec describes a subprocess to run.
// It contains the command, arguments, and per-process configuration.
//
// Example:
//
//	spec := ProcessSpec{
//	    Name:     "build",
//	    Command:  "go",
//	    Args:     []string{"build", "-v", "./..."},
//	    MaxLines: 500,   // Keep last 500 lines
//	    MaxBytes: 10240, // Keep max 10KB of output
//	}
type ProcessSpec struct {
	// Name is a logical label for the subprocess, used in output headers and logs.
	// If empty, a default name like "proc-0" will be generated.
	Name string

	// Command is the executable to run (e.g., "sh", "bash", "go", "npm").
	// This should be either an absolute path or a name that exists in PATH.
	Command string

	// Args are the command-line arguments to pass to the executable.
	// Do not include the command name itself in Args.
	//
	// Example: For "go build -v ./...", use:
	//   Command: "go"
	//   Args:    []string{"build", "-v", "./..."}
	Args []string

	// MaxLines is the maximum number of output lines to keep for this process.
	// If 0, uses the global Config.MaxLinesPerProc default.
	// When the limit is exceeded, the oldest lines are evicted (FIFO).
	//
	// This provides fine-grained control over memory usage per process.
	// Processes with heavy output can have stricter limits while lightweight
	// processes can retain more history.
	MaxLines int

	// MaxBytes is the maximum number of bytes to keep in the output history
	// for this process. If 0, no byte limit is enforced (only line limit applies).
	//
	// When both MaxLines and MaxBytes are set, lines are evicted when EITHER
	// limit is exceeded. This protects against both volume (many lines) and
	// size (few very long lines).
	//
	// Example: MaxLines=1000 and MaxBytes=100000 means keep at most 1000 lines
	// AND at most 100KB, whichever constraint is reached first.
	MaxBytes int
}

// Command is an abstraction over os/exec.Cmd to enable testing and alternative
// implementations. This interface represents a runnable command with capturable
// output streams.
//
// The standard implementation wraps os/exec.Cmd, but custom implementations
// can provide:
//   - Remote execution over SSH
//   - Mock commands for testing
//   - Containerized execution (Docker, Podman)
//   - Custom logging and instrumentation
//
// Example custom implementation:
//
//	type RemoteCommand struct {
//	    host string
//	    spec ProcessSpec
//	}
//
//	func (c *RemoteCommand) Start() error {
//	    // SSH to host and start command
//	}
type Command interface {
	// StdoutPipe returns a reader for the command's standard output.
	// This must be called before Start().
	// The pipe will be closed automatically when the command exits.
	StdoutPipe() (io.ReadCloser, error)

	// StderrPipe returns a reader for the command's standard error.
	// This must be called before Start().
	// The pipe will be closed automatically when the command exits.
	StderrPipe() (io.ReadCloser, error)

	// Start begins execution of the command without waiting for it to complete.
	// The caller must call Wait() to collect the exit status and release resources.
	//
	// Start will return an error if the command cannot be started (e.g., if the
	// executable is not found or cannot be executed).
	Start() error

	// Wait waits for the command to exit and returns any error.
	// Wait will return:
	//   - nil if the command exits with status 0
	//   - *exec.ExitError if the command exits with non-zero status
	//   - other errors for unexpected failures
	//
	// Wait must be called after Start() to properly clean up resources.
	// It will block until the command completes.
	Wait() error

	// Process returns the underlying process handle, if available.
	// This is used for signal handling (SIGTERM/SIGKILL) during graceful shutdown.
	// May return nil if the process has not been started or has already exited.
	Process() ProcessHandle
}

// ProcessHandle is an abstraction over os.Process for signal handling.
// This interface enables sending signals to running processes, which is
// essential for graceful shutdown (SIGTERM followed by SIGKILL).
//
// The standard implementation wraps os.Process, but custom implementations
// can handle signals for remote processes, containerized processes, etc.
type ProcessHandle interface {
	// Signal sends the specified signal to the process.
	// Common signals include:
	//   - syscall.SIGTERM: Request graceful termination
	//   - syscall.SIGINT:  Interrupt (Ctrl+C equivalent)
	//   - syscall.SIGKILL: Force immediate termination
	//
	// Returns an error if the signal cannot be sent (e.g., process already exited).
	Signal(sig syscall.Signal) error

	// Kill forcefully terminates the process (equivalent to SIGKILL on Unix).
	// Unlike Signal(SIGTERM), this cannot be caught or ignored by the process.
	// Should be used as a last resort after graceful shutdown timeout expires.
	//
	// Returns an error if the process cannot be killed (e.g., already exited).
	Kill() error
}
