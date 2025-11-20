package engine

import (
	"context"
	"time"
)

// CommandFactory creates Command instances from ProcessSpecs.
// This abstraction enables dependency injection for testing and alternative
// command implementations.
//
// The factory receives:
//   - ctx: Context for cancellation and deadlines
//   - spec: ProcessSpec describing the command to create
//
// And returns:
//   - Command: An instance implementing the Command interface
//   - error: Any error encountered during command creation
//
// Standard usage (production):
//
//	factory := engine.DefaultCommandFactory
//
// Testing with mocks:
//
//	factory := func(ctx context.Context, spec ProcessSpec) (Command, error) {
//	    return &MockCommand{
//	        stdout: []string{"line1", "line2"},
//	        exitErr: nil,
//	    }, nil
//	}
//
// Remote execution:
//
//	factory := func(ctx context.Context, spec ProcessSpec) (Command, error) {
//	    return &SSHCommand{
//	        host: "remote.example.com",
//	        spec: spec,
//	    }, nil
//	}
type CommandFactory func(ctx context.Context, spec ProcessSpec) (Command, error)

// Engine is the core execution engine that runs processes and emits ProcessLine events.
// It is decoupled from any specific rendering logic, making it reusable for
// different output formats (terminal UI, JSON logs, progress bars, metrics, etc.).
//
// The Engine coordinates:
//   - Concurrent process execution (one goroutine per process)
//   - Stream capture (stdout and stderr)
//   - Line normalization (cross-platform line endings)
//   - Graceful shutdown (SIGTERM → timeout → SIGKILL sequence)
//   - Error propagation and exit status handling
//
// Basic usage:
//
//	eng := engine.New(specs, 5*time.Second)
//	output := make(chan engine.ProcessLine, 128)
//	go eng.Run(ctx, output)
//
//	for pl := range output {
//	    // Handle ProcessLine events
//	}
//
// With custom command factory (for testing):
//
//	eng := engine.New(specs, timeout).WithCommandFactory(mockFactory)
type Engine struct {
	// CommandFactory creates commands from ProcessSpecs.
	// If nil, uses DefaultCommandFactory which creates real os/exec commands.
	//
	// Custom factories enable:
	//   - Mock commands for testing (no real processes)
	//   - Remote execution (SSH, Kubernetes, etc.)
	//   - Custom instrumentation and logging
	//   - Containerized execution (Docker, Podman)
	//
	// See CommandFactory type documentation for examples.
	CommandFactory CommandFactory

	// Specs defines the processes to run.
	// Each spec describes one subprocess (command, args, limits).
	// Processes are executed concurrently in the order specified.
	Specs []ProcessSpec

	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	// When the context is cancelled, the engine will:
	//   1. Send SIGTERM to all running processes
	//   2. Wait up to ShutdownTimeout for graceful termination
	//   3. Send SIGKILL to forcefully terminate remaining processes
	//
	// If zero or negative, defaults to 5 seconds.
	//
	// Example: 10*time.Second allows slow processes more time to clean up.
	ShutdownTimeout time.Duration
}

// New creates a new Engine with the given specs and optional shutdown timeout.
// This is the primary constructor for creating engines.
//
// Parameters:
//   - specs: Slice of ProcessSpec defining the processes to run
//   - shutdownTimeout: Maximum time to wait for graceful shutdown (0 = use default)
//
// Returns:
//   - *Engine: Configured engine ready to run
//
// The returned engine will use DefaultCommandFactory unless overridden with
// WithCommandFactory().
//
// Example:
//
//	specs := []engine.ProcessSpec{
//	    {Name: "build", Command: "go", Args: []string{"build"}},
//	    {Name: "test", Command: "go", Args: []string{"test"}},
//	}
//	eng := engine.New(specs, 5*time.Second)
func New(specs []ProcessSpec, shutdownTimeout time.Duration) *Engine {
	return &Engine{
		Specs:           specs,
		ShutdownTimeout: shutdownTimeout,
		CommandFactory:  nil, // Will use DefaultCommandFactory
	}
}

// WithCommandFactory returns a copy of the engine with a custom command factory.
// This is primarily useful for testing, but also enables custom implementations
// like remote execution or containerized commands.
//
// Parameters:
//   - factory: CommandFactory to use for creating commands
//
// Returns:
//   - *Engine: New engine instance with the specified factory
//
// This method returns a new Engine instance rather than modifying the receiver,
// making it safe to use concurrently and supporting functional-style configuration.
//
// Example (testing):
//
//	mockFactory := func(ctx, spec) (Command, error) {
//	    return &MockCommand{stdout: []string{"test"}}, nil
//	}
//	eng := engine.New(specs, timeout).WithCommandFactory(mockFactory)
//
// Example (remote execution):
//
//	sshFactory := func(ctx context.Context, spec ProcessSpec) (Command, error) {
//	    return NewSSHCommand("user@host", spec), nil
//	}
//	eng := engine.New(specs, timeout).WithCommandFactory(sshFactory)
func (eng *Engine) WithCommandFactory(factory CommandFactory) *Engine {
	return &Engine{
		Specs:           eng.Specs,
		ShutdownTimeout: eng.ShutdownTimeout,
		CommandFactory:  factory,
	}
}
