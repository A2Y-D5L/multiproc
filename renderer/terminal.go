package renderer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// clearScreen clears the terminal screen and moves the cursor to the top-left.
// This uses ANSI escape codes that are supported by most modern terminals.
//
// ANSI codes used:
//   - \x1b[H: Move cursor to home position (1,1)
//   - \x1b[2J: Clear entire screen
//
// Compatibility:
//   - Works on Unix/Linux/macOS terminals
//   - Works on Windows 10+ with VT100 emulation
//   - When piped to file, escape codes are preserved (still readable)
//
// This function is called by RenderScreen() before each re-render in TTY mode.
func clearScreen() {
	fmt.Print("\x1b[H\x1b[2J")
}

// RenderScreen performs a full-screen re-render of all process states.
// This is the primary renderer for interactive TTY mode.
//
// Behavior:
//  1. Check if any state is dirty (optimization)
//  2. Clear the entire screen with ANSI codes
//  3. Render each process in order:
//     - Header: "Running <Name>... [<status>]"
//     - Output lines (indented)
//     - Blank line separator
//  4. Display footer with instructions
//  5. Clear dirty flags on all states
//
// Status values:
//   - "running": Process is still executing
//   - "ok": Process exited successfully
//   - "exit code N": Process exited with error code N
//   - "killed by signal SIG": Process was terminated by signal
//
// Performance:
//   - Skips render if no states are dirty (fast path)
//   - Full re-render on each call (simple, predictable)
//   - Suitable for low-to-medium frequency updates
//
// Output format example:
//
//	Running build... [running]
//	    Starting build process
//	    Compiling...
//
//	Running test... [ok]
//	    Running tests
//	    All tests passed
//
//	Press Ctrl+C to cancel. Output updates in real time.
//
// Parameters:
//   - states: Slice of ProcessState to render
//
// This function writes directly to stdout and is intended for TTY environments.
func RenderScreen(states []ProcessState) {
	// Fast path: if nothing is dirty, skip the render entirely.
	hasDirty := false
	for _, ps := range states {
		if ps.Dirty {
			hasDirty = true
			break
		}
	}
	if !hasDirty {
		return
	}

	clearScreen()

	for i := range states {
		ps := &states[i]
		status := "running"
		if ps.Done {
			status = FormatExitError(ps.Err)
		}

		// Header: "Running Subprocess A… [running]"
		fmt.Printf("Running %s… [%s]\n", ps.Name, status)

		for _, line := range ps.Lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				fmt.Println()
				continue
			}
			fmt.Printf("    %s\n", line)
		}

		fmt.Println()
		ps.Dirty = false
	}

	fmt.Println("Press Ctrl+C to cancel. Output updates in real time.")
}

// FormatExitError formats a process exit error into a human-readable string.
// This function extracts detailed information from exec.ExitError to provide
// meaningful status messages.
//
// Return values:
//   - "ok": Process exited successfully (err == nil)
//   - "error: <msg>": Generic error (not an exec.ExitError)
//   - "exit code N": Process exited with code N
//   - "killed by signal SIG (exit code N)": Process was terminated by signal
//
// Exit code details:
//   - 0: Success (returns "ok")
//   - 1-255: Standard exit codes
//   - 128+N: Killed by signal N on Unix (e.g., 137 = SIGKILL)
//
// Signal handling:
//   - Extracts signal information from syscall.WaitStatus
//   - Includes both signal name and exit code
//   - Provides clear indication of forceful termination
//
// Parameters:
//   - err: Error from process Wait() (may be nil)
//
// Returns:
//   - string: Human-readable status message
//
// Example output:
//   - FormatExitError(nil) → "ok"
//   - FormatExitError(exit code 1) → "exit code 1"
//   - FormatExitError(SIGKILL) → "killed by signal killed (exit code 137)"
func FormatExitError(err error) string {
	if err == nil {
		return "ok"
	}

	// Check if it's an exec.ExitError
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// Not an ExitError, return the error string as-is
		return fmt.Sprintf("error: %v", err)
	}

	// Extract exit code
	exitCode := exitErr.ExitCode()

	// Check for signal-based termination
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			signal := status.Signal()
			return fmt.Sprintf("killed by signal %v (exit code %d)", signal, exitCode)
		}
	}

	// Standard exit code
	return fmt.Sprintf("exit code %d", exitCode)
}

// WriteFinalSummary prints a concise summary of all process results to stderr.
// This is useful after the real-time view completes, especially when:
//   - Scrollback history is long
//   - Output was redirected to a file
//   - User needs quick overview of success/failure
//
// Format:
//
//	Summary:
//	  - <ProcessName>: <status>
//	  - <ProcessName>: <status>
//	  ...
//
// Example output:
//
//	Summary:
//	  - build: ok
//	  - test: exit code 1
//	  - lint: ok
//
// Parameters:
//   - states: Slice of ProcessState to summarize
//
// This function writes to stderr to keep it separate from process output
// and ensure visibility even when stdout is redirected.
func WriteFinalSummary(states []ProcessState) {
	fmt.Fprintln(os.Stderr, "\nSummary:")
	for _, ps := range states {
		status := FormatExitError(ps.Err)
		fmt.Fprintf(os.Stderr, "  - %s: %s\n", ps.Name, status)
	}
}

// IsTTY reports whether the current stdout is a TTY (interactive terminal).
// This is used to choose between full-screen and incremental renderers.
//
// Detection logic:
//  1. Call os.Stdout.Stat() to get file info
//  2. Check if mode includes os.ModeCharDevice flag
//  3. Return true if it's a character device (TTY)
//
// Returns true for:
//   - Interactive terminal sessions
//   - SSH sessions with PTY
//   - Terminal emulators
//
// Returns false for:
//   - Piped output: command | othercommand
//   - Redirected output: command > file
//   - CI/CD environments without TTY
//   - Background processes
//
// This function is called automatically by runner.Run() when Config.IsTTY is nil.
//
// Example usage:
//
//	if renderer.IsTTY() {
//	    // Use full-screen rendering
//	    renderer.RenderScreen(states)
//	} else {
//	    // Use incremental rendering
//	    renderer.RenderIncremental(ev, specs, states, false, "[%s]")
//	}
func IsTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode&os.ModeCharDevice != 0
}
