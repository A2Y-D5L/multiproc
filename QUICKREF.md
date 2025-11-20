# Multiproc Quick Reference

Quick reference guide for the multiproc concurrent process runner library.

## Installation

```bash
go get github.com/a2y-d5l/multiproc
```

## Minimal Example

```go
import (
    "context"
    "os"
    "github.com/a2y-d5l/multiproc/engine"
    "github.com/a2y-d5l/multiproc/runner"
)

func main() {
    specs := []engine.ProcessSpec{
        {Name: "build", Command: "go", Args: []string{"build"}},
        {Name: "test", Command: "go", Args: []string{"test"}},
    }
    
    cfg := runner.DefaultConfig()
    cfg.Specs = specs
    
    os.Exit(runner.Run(context.Background(), cfg))
}
```

## ProcessSpec Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Name` | string | "" | Display name for the process |
| `Command` | string | required | Executable to run |
| `Args` | []string | nil | Command arguments |
| `MaxLines` | int | 1000 | Max lines to keep (0 = use global) |
| `MaxBytes` | int | 0 | Max bytes to keep (0 = unlimited) |

## Config Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Specs` | []ProcessSpec | required | Processes to run |
| `IsTTY` | *bool | auto | Force TTY mode (nil = auto-detect) |
| `MaxLinesPerProc` | int | 1000 | Default max lines per process |
| `ShutdownTimeout` | time.Duration | 5s | Graceful shutdown timeout |
| `FullScreen` | bool | true | Enable full-screen rendering |
| `ShowSummary` | bool | true | Show summary after completion |
| `ShowTimestamps` | bool | false | Prefix lines with timestamps |
| `LogPrefix` | string | "[%s]" | Process name prefix format |

## Common Patterns

### With Timestamps

```go
cfg := runner.DefaultConfig()
cfg.Specs = specs
cfg.ShowTimestamps = true
```

### Custom Prefix

```go
cfg.LogPrefix = "%s:"  // "ProcessName: line"
```

### Memory Limits

```go
specs := []engine.ProcessSpec{
    {
        Name: "verbose",
        Command: "npm",
        Args: []string{"run", "build"},
        MaxLines: 500,
        MaxBytes: 100000,  // 100KB
    },
}
```

### Signal Handling

```go
ctx, cancel := context.WithCancelCause(context.Background())
defer cancel(nil)

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
go func() {
    sig := <-sigCh
    cancel(fmt.Errorf("received signal: %v", sig))
}()

os.Exit(runner.Run(ctx, cfg))
```

### Direct Engine Usage

```go
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

### Testing with Mocks

```go
mockFactory := func(ctx context.Context, spec engine.ProcessSpec) (engine.Command, error) {
    return &MockCommand{
        stdout: []string{"line1", "line2"},
        err: nil,
    }, nil
}

eng := engine.New(specs, timeout).WithCommandFactory(mockFactory)
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All processes succeeded |
| 1 | One or more processes failed |

## Performance Guidelines

| Scenario | Recommendation |
|----------|----------------|
| 1-10 processes | Default settings |
| 11-100 processes | Consider `MaxLinesPerProc: 500` |
| 100+ processes | Not recommended, consider batching |
| High-frequency output | Use channel buffer of 128+ |
| Long-running (hours) | Set strict `MaxLines: 100-200` |
| CI/CD | Enable `ShowTimestamps: true` |

## CLI Flags

```bash
multiproc [OPTIONS]

-fullscreen          # Enable full-screen rendering (default: true)
-summary            # Show summary after execution (default: true)
-timestamps         # Prefix lines with timestamps (default: false)
-prefix string      # Process name prefix format (default: "[%s]")
-max-lines int      # Max output lines per process (default: 1000)
-shutdown-timeout int  # Graceful shutdown seconds (default: 5)
-help               # Show help message
```

## Package Layout

```txt
multiproc/
├── engine/         - Core execution (no UI dependencies)
├── renderer/       - Output formatting and display
├── runner/         - High-level orchestration
└── cmd/multiproc/  - CLI application
```

## Import Paths

```go
import (
    "github.com/a2y-d5l/multiproc/engine"   // Direct engine usage
    "github.com/a2y-d5l/multiproc/renderer" // Custom rendering
    "github.com/a2y-d5l/multiproc/runner"   // Simple usage
)
```

## Documentation

- Full examples: [EXAMPLES.md](EXAMPLES.md)
- Architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Performance: [PERFORMANCE.md](PERFORMANCE.md)
- API docs: `go doc github.com/a2y-d5l/multiproc/runner`

## Troubleshooting

### Process hangs on shutdown

- Increase `ShutdownTimeout`: `cfg.ShutdownTimeout = 30 * time.Second`
- Check if process handles SIGTERM correctly

### Memory grows unbounded

- Set `MaxLines` and `MaxBytes` on ProcessSpec
- Reduce global `MaxLinesPerProc`

### Output is truncated

- Increase `MaxLines` for verbose processes
- Set `MaxBytes: 0` to disable byte limit

### Tests are flaky

- Use mock CommandFactory
- Force non-TTY mode: `val := false; cfg.IsTTY = &val`
- Ensure deterministic output in mocks

### Can't see process output

- Check if process writes to stderr instead of stdout
- Both are captured and merged

### Performance is slow

- Enable render debouncing (already default)
- Use incremental mode: `cfg.FullScreen = false`
- Reduce `MaxLinesPerProc` for many processes

## Best Practices

✅ **DO**:

- Use `runner.DefaultConfig()` as starting point
- Set appropriate `ShutdownTimeout` for your processes
- Handle signals for graceful shutdown
- Use timestamps in CI/CD environments
- Set memory limits for long-running processes
- Use mocks for testing
- Buffer output channels (128+)

❌ **DON'T**:

- Run >100 processes concurrently
- Use unbounded memory (set MaxLines/MaxBytes)
- Forget to handle context cancellation
- Use full-screen mode in CI/CD (auto-detected)
- Block on channel sends (use buffered channels)
- Ignore exit codes

## Version Compatibility

- Go 1.20+ (uses `context.WithCancelCause`)
- Works on: macOS, Linux, Windows (with caveats)
- TTY detection: Unix-compatible terminals

## License

See LICENSE file in repository.

## Support

- Issues: <https://github.com/a2y-d5l/multiproc/issues>
- Docs: <https://pkg.go.dev/github.com/a2y-d5l/multiproc>
