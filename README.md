# Multiproc - Concurrent Process Runner

A Go library for running multiple processes concurrently with real-time output rendering and graceful shutdown handling.

## Package Structure

The project is organized into clean, decoupled packages:

```txt
multiproc/
├── engine/              - Core execution engine (library)
│   ├── types.go         - ProcessSpec, ProcessLine, Command interface
│   ├── config.go        - Engine configuration
│   ├── engine.go        - Main execution logic
│   ├── exec.go          - Real os/exec implementation
│   └── engine_test.go   - Comprehensive unit tests
│
├── renderer/            - Rendering layer (library)
│   ├── state.go         - ProcessState and event handling
│   ├── terminal.go      - Full-screen TTY renderer
│   └── incremental.go   - Non-TTY incremental renderer
│
├── runner/              - High-level orchestration (library)
│   └── runner.go        - Ties engine and renderer together
│
└── cmd/                 - Executable binaries
    └── multiproc/
        └── main.go      - CLI application
```

## Architecture

The architecture follows clean separation of concerns:

```txt
┌─────────────┐
│  CLI/Client │  ← Your application code
└──────┬──────┘
       │ uses
       ▼
┌─────────────┐
│   Runner    │  ← High-level orchestration
└──────┬──────┘
       │ uses
    ┌──┴──────┐
    ▼         ▼
┌────────┐ ┌──────────┐
│ Engine │ │ Renderer │  ← Core libraries
└────────┘ └──────────┘
```

### Engine Package

The engine is the core execution layer:

- **Decoupled from UI**: Emits `ProcessLine` events on a channel
- **Testable**: Accepts `CommandFactory` for dependency injection
- **Graceful shutdown**: SIGTERM → timeout → SIGKILL sequence
- **Cross-platform**: Normalized line endings

**Key Types:**

- `ProcessSpec`: Describes a subprocess to run
- `ProcessLine`: Raw output event (line or completion)
- `Command`: Interface over `os/exec.Cmd` for testing
- `Engine`: Main execution engine

**Example:**

```go
import "github.com/a2y-d5l/multiproc/engine"

specs := []engine.ProcessSpec{
    {
        Name:    "build",
        Command: "go",
        Args:    []string{"build", "./..."},
    },
}

eng := engine.New(specs, 5*time.Second)
output := make(chan engine.ProcessLine, 128)

go eng.Run(ctx, output)

for pl := range output {
    if pl.IsComplete {
        fmt.Printf("Process %d done: %v\n", pl.Index, pl.Err)
    } else {
        fmt.Printf("[%d] %s\n", pl.Index, pl.Line)
    }
}
```

### Renderer Package

The renderer handles output formatting and state management:

- **State management**: `ProcessState` tracks per-process state
- **Event application**: Updates state from `ProcessLine` events
- **Terminal rendering**: Full-screen TTY mode with screen clearing
- **Incremental rendering**: Non-TTY mode for CI/logs
- **Memory limits**: Line and byte-based eviction

**Key Functions:**

- `ConvertProcessLineToEvent()`: Adapts engine events for rendering
- `ApplyEvent()`: Updates process state
- `RenderScreen()`: Full-screen terminal UI
- `RenderIncremental()`: Line-by-line output for logs

### Runner Package

The runner package ties everything together:

- **Configuration**: High-level `Config` struct
- **TTY detection**: Automatic or manual override
- **Render coordination**: Debouncing, event conversion
- **Exit code handling**: Aggregates process results

**Example:**

```go
import "github.com/a2y-d5l/multiproc/runner"

cfg := runner.DefaultConfig()
cfg.Specs = specs
cfg.FullScreen = true
cfg.ShowSummary = true

exitCode := runner.Run(ctx, cfg)
os.Exit(exitCode)
```

## Features

### ✅ Concurrent Execution

- Multiple processes run simultaneously
- Real-time output capture (stdout + stderr)
- Async event-driven architecture

### ✅ Graceful Shutdown

- Context cancellation triggers shutdown
- SIGTERM sent first for graceful exit
- Configurable timeout before SIGKILL
- Per-process shutdown status logging

### ✅ Memory Management

- Per-process line limits
- Per-process byte limits
- Automatic eviction of oldest lines
- Dual-constraint enforcement

### ✅ Cross-Platform

- Line ending normalization (CRLF/LF/CR)
- TTY detection and adaptation
- Configurable shell commands

### ✅ Testability

- Command interface for mocking
- Comprehensive unit tests
- No external dependencies for tests
- Fast, deterministic test execution

### ✅ Flexible Rendering

- Full-screen TTY mode
- Incremental non-TTY mode
- Easily extensible for new formats
- JSON logs, metrics, progress bars

## Usage

### As a Library

```go
package main

import (
    "context"
    "github.com/a2y-d5l/multiproc/engine"
    "github.com/a2y-d5l/multiproc/runner"
)

func main() {
    specs := []engine.ProcessSpec{
        {
            Name:    "lint",
            Command: "golangci-lint",
            Args:    []string{"run"},
        },
        {
            Name:    "test",
            Command: "go",
            Args:    []string{"test", "./..."},
        },
    }

    ctx := context.Background()
    cfg := runner.DefaultConfig()
    cfg.Specs = specs

    os.Exit(runner.Run(ctx, cfg))
}
```

### Custom Renderer

```go
// Implement a JSON logger
output := make(chan engine.ProcessLine, 128)
go eng.Run(ctx, output)

for pl := range output {
    event := map[string]interface{}{
        "index": pl.Index,
        "line": pl.Line,
        "complete": pl.IsComplete,
    }
    json.NewEncoder(os.Stdout).Encode(event)
}
```

### With Custom Command Factory (Testing)

```go
mockFactory := func(ctx context.Context, spec engine.ProcessSpec) (engine.Command, error) {
    return &MockCommand{
        stdout: []string{"line1", "line2"},
    }, nil
}

eng := engine.New(specs, 5*time.Second).
    WithCommandFactory(mockFactory)
```

## Configuration

### ProcessSpec Options

```go
type ProcessSpec struct {
    Name     string   // Display name
    Command  string   // Executable
    Args     []string // Arguments
    MaxLines int      // Max lines to keep (0 = use global default)
    MaxBytes int      // Max bytes to keep (0 = unlimited)
}
```

### Runner Config Options

```go
type Config struct {
    IsTTY           *bool         // Force TTY mode (nil = auto-detect)
    Specs           []ProcessSpec // Processes to run
    MaxLinesPerProc int           // Default max lines per process
    ShutdownTimeout time.Duration // Graceful shutdown timeout
    FullScreen      bool          // Enable full-screen rendering
    ShowSummary     bool          // Show summary on completion
}
```

## Testing

Run all tests:

```bash
go test ./multiproc/...
```

Run engine tests only:

```bash
go test ./multiproc/engine
```

Run with coverage:

```bash
go test -cover ./multiproc/...
```

## Building

Build the CLI:

```bash
go build -o multiproc ./multiproc/cmd/multiproc
```

Build all packages:

```bash
go build ./multiproc/...
```

## Design Principles

1. **Separation of Concerns**: Engine ↔ Renderer ↔ CLI
2. **Dependency Injection**: Command interface for testability
3. **Event-Driven**: Channel-based communication
4. **Context-Aware**: Graceful cancellation support
5. **Memory-Safe**: Bounded buffers and eviction
6. **Cross-Platform**: Platform-agnostic abstractions

## Future Extensions

The architecture supports easy extension:

- **JSON Renderer**: Machine-readable logs
- **Progress Bars**: Visual progress tracking
- **Metrics Collection**: Execution analytics
- **Remote Execution**: Run processes on remote machines
- **Alternative Shells**: PowerShell, cmd, etc.

## API Documentation

Complete API documentation is available via:

```bash
go doc github.com/a2y-d5l/multiproc/runner
go doc github.com/a2y-d5l/multiproc/engine
go doc github.com/a2y-d5l/multiproc/renderer
```

Or online at [pkg.go.dev](https://pkg.go.dev/github.com/a2y-d5l/multiproc).

## Additional Documentation

- [Architecture Guide](ARCHITECTURE.md) - Design patterns and structure
- [Usage Examples](EXAMPLES.md) - 20+ real-world examples
- [Performance Guide](PERFORMANCE.md) - Benchmarks and optimization
- [Quick Reference](QUICKREF.md) - Common patterns and troubleshooting
- [Changelog](CHANGELOG.md) - Version history

## Requirements

- Go 1.20 or later (requires `context.WithCancelCause`)
- Works on macOS, Linux, and Windows
- TTY detection works on Unix-compatible terminals

## Contributing

Contributions welcome! Please ensure:

- All tests pass: `go test ./multiproc/...`
- Code is formatted: `go fmt ./multiproc/...`
- Add tests for new features
- Update documentation as needed

## License

See LICENSE file in repository root.
