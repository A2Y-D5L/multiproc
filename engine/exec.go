package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// DefaultCommandFactory creates real os/exec commands for process execution.
// This is the production implementation of CommandFactory that actually spawns
// system processes.
//
// The factory:
//   - Creates an exec.Cmd using the spec's Command and Args
//   - Wraps it in execCommand to implement the Command interface
//   - Does not modify environment or working directory (uses parent process settings)
//   - Inherits stdin from parent (connected to /dev/null or equivalent)
//
// This factory is used automatically when Engine.CommandFactory is nil.
//
// Example (implicit usage):
//
//	eng := engine.New(specs, timeout)
//	// DefaultCommandFactory will be used automatically
//
// Example (explicit usage):
//
//	eng := &Engine{
//	    Specs: specs,
//	    CommandFactory: engine.DefaultCommandFactory,
//	}
func DefaultCommandFactory(ctx context.Context, spec ProcessSpec) (Command, error) {
	return &execCommand{
		spec: spec,
		cmd:  newExecCmdWrapper(ctx, spec.Command, spec.Args...),
	}, nil
}

// execCommand wraps exec.Cmd to implement the Command interface.
type execCommand struct {
	cmd  *execCmdWrapper
	spec ProcessSpec
}

func (e *execCommand) StdoutPipe() (io.ReadCloser, error) {
	return e.cmd.StdoutPipe()
}

func (e *execCommand) StderrPipe() (io.ReadCloser, error) {
	return e.cmd.StderrPipe()
}

func (e *execCommand) Start() error {
	return e.cmd.Start()
}

func (e *execCommand) Wait() error {
	if e.cmd == nil {
		return errors.New("command not started")
	}
	return e.cmd.Wait()
}

func (e *execCommand) Process() ProcessHandle {
	if e.cmd == nil {
		return nil
	}
	return e.cmd.Process()
}

// execCmdWrapper wraps os/exec.Cmd to provide the necessary interfaces.
type execCmdWrapper struct {
	*exec.Cmd
}

// newExecCmdWrapper creates a new wrapped exec.Cmd with context.
func newExecCmdWrapper(ctx context.Context, name string, args ...string) *execCmdWrapper {
	return &execCmdWrapper{
		Cmd: exec.CommandContext(ctx, name, args...),
	}
}

// StdoutPipe returns a pipe for stdout.
func (e *execCmdWrapper) StdoutPipe() (io.ReadCloser, error) {
	return e.Cmd.StdoutPipe()
}

// StderrPipe returns a pipe for stderr.
func (e *execCmdWrapper) StderrPipe() (io.ReadCloser, error) {
	return e.Cmd.StderrPipe()
}

// Process returns the process handle as a ProcessHandle interface.
func (e *execCmdWrapper) Process() ProcessHandle {
	if e.Cmd.Process == nil {
		return nil
	}
	return &processWrapper{Process: e.Cmd.Process}
}

// processWrapper wraps os.Process to implement ProcessHandle.
type processWrapper struct {
	*os.Process
}

// Signal sends a signal to the process.
func (p *processWrapper) Signal(sig syscall.Signal) error {
	// Use os.Process.Signal for cross-platform compatibility
	return p.Process.Signal(sig)
}

// Kill terminates the process.
func (p *processWrapper) Kill() error {
	return p.Process.Kill()
}
