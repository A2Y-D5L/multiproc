# Usage Examples

This document provides comprehensive examples for using the multiproc library in various scenarios.

## Table of Contents

1. [Quick Start](#quick-start)
2. [Basic Usage](#basic-usage)
3. [Advanced Configuration](#advanced-configuration)
4. [Error Handling](#error-handling)
5. [Custom Rendering](#custom-rendering)
6. [Testing with Mocks](#testing-with-mocks)
7. [CI/CD Integration](#cicd-integration)
8. [Signal Handling](#signal-handling)
9. [Memory Management](#memory-management)
10. [Real-World Examples](#real-world-examples)

---

## Quick Start

The simplest way to use multiproc:

```go
package main

import (
 "context"
 "os"

 "github.com/a2y-d5l/multiproc/engine"
 "github.com/a2y-d5l/multiproc/runner"
)

func main() {
 specs := []engine.ProcessSpec{
  {Name: "build", Command: "go", Args: []string{"build", "./..."}},
  {Name: "test", Command: "go", Args: []string{"test", "./..."}},
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs

 os.Exit(runner.Run(context.Background(), cfg))
}
```

---

## Basic Usage

### Simple Process Execution

```go
package main

import (
 "context"
 "os"

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
   Args:    []string{"test", "-v", "./..."},
  },
  {
   Name:    "build",
   Command: "go",
   Args:    []string{"build", "-o", "app", "./cmd/app"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShowSummary = true

 exitCode := runner.Run(context.Background(), cfg)
 os.Exit(exitCode)
}
```

### With Timestamps (for debugging timing issues)

```go
func main() {
 specs := []engine.ProcessSpec{
  {Name: "slow-build", Command: "make", Args: []string{"build"}},
  {Name: "slow-test", Command: "make", Args: []string{"test"}},
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShowTimestamps = true  // Enable RFC3339 timestamps
 cfg.LogPrefix = "[%s]"

 os.Exit(runner.Run(context.Background(), cfg))
}
```

Output with timestamps:

```txt
[2024-11-20T15:30:45Z] [slow-build] Starting build...
[2024-11-20T15:30:46Z] [slow-test] Running tests...
[2024-11-20T15:30:50Z] [slow-build] Build complete
```

---

## Advanced Configuration

### Custom Memory Limits

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:     "verbose-process",
   Command:  "npm",
   Args:     []string{"run", "build"},
   MaxLines: 500,     // Keep only last 500 lines
   MaxBytes: 100000,  // Keep max 100KB of output
  },
  {
   Name:     "quiet-process",
   Command:  "go",
   Args:     []string{"build"},
   MaxLines: 100,     // Smaller limit for less verbose process
   MaxBytes: 10000,   // 10KB max
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.MaxLinesPerProc = 1000  // Default for processes without explicit limits

 os.Exit(runner.Run(context.Background(), cfg))
}
```

### Custom Log Prefix Format

```go
func main() {
 specs := []engine.ProcessSpec{
  {Name: "frontend", Command: "npm", Args: []string{"start"}},
  {Name: "backend", Command: "go", Args: []string{"run", "."}},
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.LogPrefix = ">>> %s >>>"  // Custom bracket style
 // Output: >>> frontend >>> line content

 os.Exit(runner.Run(context.Background(), cfg))
}
```

Other prefix examples:

- `"%s:"` → `frontend: line content`
- `"(%s)"` → `(frontend) line content`
- `"[%s] →"` → `[frontend] → line content`

### Longer Graceful Shutdown Timeout

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:    "database",
   Command: "postgres",
   Args:    []string{"-D", "/data"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShutdownTimeout = 30 * time.Second  // Wait 30s for graceful shutdown

 os.Exit(runner.Run(context.Background(), cfg))
}
```

---

## Error Handling

### Handling Process Failures

```go
func main() {
 specs := []engine.ProcessSpec{
  {Name: "task1", Command: "sh", Args: []string{"-c", "exit 1"}},
  {Name: "task2", Command: "sh", Args: []string{"-c", "echo ok"}},
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShowSummary = true  // Will show which failed

 exitCode := runner.Run(context.Background(), cfg)
 
 if exitCode != 0 {
  fmt.Fprintf(os.Stderr, "One or more processes failed\n")
  os.Exit(exitCode)
 }
 
 fmt.Println("All processes succeeded")
}
```

### Collecting Exit Codes with Direct Engine Usage

```go
func main() {
 specs := []engine.ProcessSpec{
  {Name: "task1", Command: "sh", Args: []string{"-c", "exit 0"}},
  {Name: "task2", Command: "sh", Args: []string{"-c", "exit 1"}},
 }

 eng := engine.New(specs, 5*time.Second)
 output := make(chan engine.ProcessLine, 128)

 ctx := context.Background()
 go eng.Run(ctx, output)

 exitCodes := make(map[int]error)
 for pl := range output {
  if pl.IsComplete {
   exitCodes[pl.Index] = pl.Err
   name := specs[pl.Index].Name
   if pl.Err != nil {
    fmt.Printf("%s failed: %v\n", name, pl.Err)
   } else {
    fmt.Printf("%s succeeded\n", name)
   }
  }
 }

 // Check results
 for idx, err := range exitCodes {
  if err != nil {
   fmt.Printf("Process %d (%s) failed\n", idx, specs[idx].Name)
   os.Exit(1)
  }
 }
}
```

---

## Custom Rendering

### JSON Logger

```go
package main

import (
 "context"
 "encoding/json"
 "os"
 "time"

 "github.com/a2y-d5l/multiproc/engine"
)

type LogEntry struct {
 Timestamp time.Time `json:"timestamp"`
 Process   string    `json:"process"`
 Line      string    `json:"line,omitempty"`
 Complete  bool      `json:"complete"`
 Error     string    `json:"error,omitempty"`
}

func main() {
 specs := []engine.ProcessSpec{
  {Name: "build", Command: "go", Args: []string{"build"}},
  {Name: "test", Command: "go", Args: []string{"test"}},
 }

 eng := engine.New(specs, 5*time.Second)
 output := make(chan engine.ProcessLine, 128)

 ctx := context.Background()
 go eng.Run(ctx, output)

 encoder := json.NewEncoder(os.Stdout)
 for pl := range output {
  entry := LogEntry{
   Timestamp: time.Now(),
   Process:   specs[pl.Index].Name,
   Line:      pl.Line,
   Complete:  pl.IsComplete,
  }
  if pl.Err != nil {
   entry.Error = pl.Err.Error()
  }
  encoder.Encode(entry)
 }
}
```

### Progress Tracking

```go
package main

import (
 "context"
 "fmt"
 "sync"
 "time"

 "github.com/a2y-d5l/multiproc/engine"
)

type ProgressTracker struct {
 mu         sync.Mutex
 startTimes map[int]time.Time
 lineCounts map[int]int
 completed  map[int]bool
}

func NewProgressTracker(numProcs int) *ProgressTracker {
 return &ProgressTracker{
  startTimes: make(map[int]time.Time),
  lineCounts: make(map[int]int),
  completed:  make(map[int]bool),
 }
}

func (pt *ProgressTracker) Track(pl engine.ProcessLine, specs []engine.ProcessSpec) {
 pt.mu.Lock()
 defer pt.mu.Unlock()

 if _, started := pt.startTimes[pl.Index]; !started {
  pt.startTimes[pl.Index] = time.Now()
 }

 if pl.IsComplete {
  pt.completed[pl.Index] = true
  duration := time.Since(pt.startTimes[pl.Index])
  name := specs[pl.Index].Name
  status := "✓"
  if pl.Err != nil {
   status = "✗"
  }
  fmt.Printf("%s %s completed in %v (%d lines)\n", 
   status, name, duration, pt.lineCounts[pl.Index])
 } else {
  pt.lineCounts[pl.Index]++
 }
}

func main() {
 specs := []engine.ProcessSpec{
  {Name: "build", Command: "go", Args: []string{"build"}},
  {Name: "test", Command: "go", Args: []string{"test"}},
 }

 eng := engine.New(specs, 5*time.Second)
 output := make(chan engine.ProcessLine, 128)

 ctx := context.Background()
 go eng.Run(ctx, output)

 tracker := NewProgressTracker(len(specs))
 for pl := range output {
  tracker.Track(pl, specs)
 }
}
```

---

## Testing with Mocks

### Basic Mock Example

```go
package myapp_test

import (
 "context"
 "io"
 "strings"
 "testing"

 "github.com/a2y-d5l/multiproc/engine"
)

type MockCommand struct {
 stdout []string
 stderr []string
 err    error
}

func (m *MockCommand) StdoutPipe() (io.ReadCloser, error) {
 return io.NopCloser(strings.NewReader(
  strings.Join(m.stdout, "\n") + "\n",
 )), nil
}

func (m *MockCommand) StderrPipe() (io.ReadCloser, error) {
 return io.NopCloser(strings.NewReader(
  strings.Join(m.stderr, "\n") + "\n",
 )), nil
}

func (m *MockCommand) Start() error { return nil }
func (m *MockCommand) Wait() error  { return m.err }
func (m *MockCommand) Process() engine.ProcessHandle { return nil }

func TestProcessExecution(t *testing.T) {
 specs := []engine.ProcessSpec{
  {Name: "test", Command: "mock"},
 }

 mockFactory := func(ctx context.Context, spec engine.ProcessSpec) (engine.Command, error) {
  return &MockCommand{
   stdout: []string{"line1", "line2", "line3"},
   stderr: []string{},
   err:    nil,
  }, nil
 }

 eng := engine.New(specs, 5*time.Second).WithCommandFactory(mockFactory)
 output := make(chan engine.ProcessLine, 128)

 ctx := context.Background()
 go eng.Run(ctx, output)

 lines := []string{}
 for pl := range output {
  if !pl.IsComplete {
   lines = append(lines, pl.Line)
  }
 }

 expected := []string{"line1", "line2", "line3"}
 if len(lines) != len(expected) {
  t.Errorf("Expected %d lines, got %d", len(expected), len(lines))
 }
}
```

---

## CI/CD Integration

### GitHub Actions

```yaml
# .github/workflows/test.yml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run tests with multiproc
        run: |
          go run ./cmd/ci-runner
```

```go
// cmd/ci-runner/main.go
package main

import (
 "context"
 "os"

 "github.com/a2y-d5l/multiproc/engine"
 "github.com/a2y-d5l/multiproc/runner"
)

func main() {
 specs := []engine.ProcessSpec{
  {Name: "lint", Command: "golangci-lint", Args: []string{"run"}},
  {Name: "test", Command: "go", Args: []string{"test", "-v", "./..."}},
  {Name: "build", Command: "go", Args: []string{"build", "./..."}},
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShowTimestamps = true  // Useful for CI logs
 cfg.LogPrefix = "[%s]"
 
 // Non-TTY is auto-detected in CI
 os.Exit(runner.Run(context.Background(), cfg))
}
```

### GitLab CI

```yaml
# .gitlab-ci.yml
test:
  image: golang:1.21
  script:
    - go run ./cmd/ci-runner
  artifacts:
    reports:
      junit: test-results.xml
```

---

## Signal Handling

### Graceful Shutdown on Ctrl+C

```go
package main

import (
 "context"
 "fmt"
 "os"
 "os/signal"
 "syscall"

 "github.com/a2y-d5l/multiproc/engine"
 "github.com/a2y-d5l/multiproc/runner"
)

func main() {
 specs := []engine.ProcessSpec{
  {Name: "server", Command: "go", Args: []string{"run", "server.go"}},
  {Name: "worker", Command: "go", Args: []string{"run", "worker.go"}},
 }

 // Create cancellable context
 ctx, cancel := context.WithCancelCause(context.Background())
 defer cancel(nil)

 // Set up signal handling
 sigCh := make(chan os.Signal, 1)
 signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
 
 go func() {
  sig := <-sigCh
  fmt.Fprintf(os.Stderr, "\nReceived %v, shutting down gracefully...\n", sig)
  cancel(fmt.Errorf("received signal: %v", sig))
 }()

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShutdownTimeout = 10 * time.Second  // Allow 10s for cleanup

 os.Exit(runner.Run(ctx, cfg))
}
```

### Timeout Context

```go
func main() {
 specs := []engine.ProcessSpec{
  {Name: "build", Command: "go", Args: []string{"build"}},
 }

 // Fail if processes take longer than 5 minutes
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
 defer cancel()

 cfg := runner.DefaultConfig()
 cfg.Specs = specs

 exitCode := runner.Run(ctx, cfg)
 
 if ctx.Err() == context.DeadlineExceeded {
  fmt.Fprintf(os.Stderr, "Processes timed out after 5 minutes\n")
  os.Exit(1)
 }
 
 os.Exit(exitCode)
}
```

---

## Memory Management

### Per-Process Limits

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:     "verbose-npm-build",
   Command:  "npm",
   Args:     []string{"run", "build"},
   MaxLines: 200,      // NPM is verbose, limit output
   MaxBytes: 50000,    // 50KB max
  },
  {
   Name:     "go-build",
   Command:  "go",
   Args:     []string{"build", "-v"},
   MaxLines: 1000,     // Go build less verbose
   MaxBytes: 0,        // No byte limit
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.MaxLinesPerProc = 500  // Default for others

 os.Exit(runner.Run(context.Background(), cfg))
}
```

---

## Real-World Examples

### Go Project CI Pipeline

```go
package main

import (
 "context"
 "os"

 "github.com/a2y-d5l/multiproc/engine"
 "github.com/a2y-d5l/multiproc/runner"
)

func main() {
 specs := []engine.ProcessSpec{
  {
   Name:    "gofmt",
   Command: "sh",
   Args:    []string{"-c", "gofmt -l . | tee /tmp/fmt.txt && [ ! -s /tmp/fmt.txt ]"},
  },
  {
   Name:    "golint",
   Command: "golangci-lint",
   Args:    []string{"run", "--timeout=5m"},
  },
  {
   Name:    "unit-tests",
   Command: "go",
   Args:    []string{"test", "-v", "-race", "-cover", "./..."},
  },
  {
   Name:    "integration-tests",
   Command: "go",
   Args:    []string{"test", "-v", "-tags=integration", "./..."},
  },
  {
   Name:    "build",
   Command: "go",
   Args:    []string{"build", "-o", "app", "./cmd/app"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShowTimestamps = true
 cfg.ShowSummary = true

 os.Exit(runner.Run(context.Background(), cfg))
}
```

### Multi-Language Monorepo

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:    "frontend-lint",
   Command: "npm",
   Args:    []string{"run", "lint"},
  },
  {
   Name:    "frontend-test",
   Command: "npm",
   Args:    []string{"run", "test"},
  },
  {
   Name:    "frontend-build",
   Command: "npm",
   Args:    []string{"run", "build"},
  },
  {
   Name:    "backend-lint",
   Command: "golangci-lint",
   Args:    []string{"run"},
  },
  {
   Name:    "backend-test",
   Command: "go",
   Args:    []string{"test", "./..."},
  },
  {
   Name:    "backend-build",
   Command: "go",
   Args:    []string{"build", "./cmd/server"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.MaxLinesPerProc = 500
 cfg.ShowTimestamps = true

 os.Exit(runner.Run(context.Background(), cfg))
}
```

### Development Watch Mode

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:    "frontend-dev",
   Command: "npm",
   Args:    []string{"run", "dev"},
  },
  {
   Name:    "backend-dev",
   Command: "air",  // Go hot-reload tool
   Args:    []string{},
  },
  {
   Name:    "tailwind-watch",
   Command: "npx",
   Args:    []string{"tailwindcss", "-i", "input.css", "-o", "output.css", "--watch"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.FullScreen = true
 cfg.ShowSummary = false  // No summary for long-running processes

 os.Exit(runner.Run(context.Background(), cfg))
}
```

### Docker Compose Alternative

```go
func main() {
 specs := []engine.ProcessSpec{
  {
   Name:    "postgres",
   Command: "docker",
   Args:    []string{"run", "--rm", "-p", "5432:5432", "postgres:15"},
  },
  {
   Name:    "redis",
   Command: "docker",
   Args:    []string{"run", "--rm", "-p", "6379:6379", "redis:7"},
  },
  {
   Name:    "app",
   Command: "go",
   Args:    []string{"run", "./cmd/app"},
  },
 }

 cfg := runner.DefaultConfig()
 cfg.Specs = specs
 cfg.ShutdownTimeout = 15 * time.Second  // Docker containers need time to stop

 os.Exit(runner.Run(context.Background(), cfg))
}
```

---

## Tips and Best Practices

1. **Buffer the output channel appropriately**: Use at least `128` for the channel buffer to prevent blocking during high-frequency output.

2. **Use timestamps in CI**: Enable `ShowTimestamps` in non-interactive environments to help debug timing issues.

3. **Set appropriate shutdown timeouts**: Databases and services need longer timeouts (10-30s) compared to simple scripts (1-5s).

4. **Leverage per-process memory limits**: Verbose processes (npm, webpack) should have stricter limits to prevent memory issues.

5. **Handle signals properly**: Always set up signal handling for graceful shutdown in production.

6. **Use the engine directly for custom needs**: The runner package is convenient but the engine package offers more control.

7. **Test with mocks**: Use the CommandFactory abstraction to test without spawning real processes.

8. **Force non-TTY mode in tests**: Set `cfg.IsTTY = &false` to get predictable output in tests.

---

## Additional Resources

- [Architecture Documentation](ARCHITECTURE.md) - Design patterns and package structure
- [Performance Guide](PERFORMANCE.md) - Benchmarks and optimization strategies
- [Quick Reference](QUICKREF.md) - Common patterns and troubleshooting
- [API Reference](https://pkg.go.dev/github.com/a2y-d5l/multiproc) - Complete API documentation
