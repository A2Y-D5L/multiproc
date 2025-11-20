# Changelog

All notable changes to the multiproc project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2024-11-20

### Added

#### Core Features

- Concurrent execution of multiple processes with real-time output capture
- Graceful shutdown with configurable SIGTERM → SIGKILL timeout
- Context-based cancellation with `context.WithCancelCause` support
- Exit code propagation and aggregation across all processes
- Cross-platform line ending normalization (CRLF/LF/CR)

#### Architecture

- Clean package separation:
  - `engine/` - Core execution engine (no UI dependencies)
  - `renderer/` - Output formatting and display layer
  - `runner/` - High-level orchestration
  - `cmd/multiproc/` - CLI application
- Event-driven architecture using channels
- Dependency injection via `CommandFactory` for testing
- `Command` and `ProcessHandle` interfaces for mockable process execution

#### Rendering

- Full-screen TTY mode with real-time updates and screen clearing
- Incremental non-TTY mode for CI/CD and log aggregation
- Automatic TTY detection with manual override support
- Render debouncing for high-frequency output
- Process name prefixing with configurable formats
- Optional RFC3339 timestamps for timing analysis
- Initial process status display before execution
- Summary output with detailed exit information

#### Memory Management

- Per-process line limits (`MaxLines` in `ProcessSpec`)
- Per-process byte limits (`MaxBytes` in `ProcessSpec`)
- Dual-constraint enforcement (lines AND bytes)
- Automatic eviction of oldest output
- Global default limits with per-process overrides
- Bounded memory usage: O(NumProcesses × MaxLines)

#### Configuration

- High-level `Config` struct with sensible defaults
- `ProcessSpec` for flexible process definition
- Configurable shutdown timeout (default: 5 seconds)
- Configurable log prefix format (default: `[%s]`)
- Optional timestamp display
- TTY mode override
- Full-screen vs incremental rendering toggle
- Summary display toggle

#### Testing

- Comprehensive test suite with 22+ unit tests
- Mock command infrastructure for deterministic testing
- `CommandFactory` abstraction for dependency injection
- No external test dependencies
- Fast test execution (< 1 second)

#### Performance

- Benchmark suite with 5 comprehensive benchmarks
- Optimized for 10-100 concurrent processes
- >10,000 lines/second throughput per process
- <1ms event emission latency
- <5% CPU overhead for coordination

#### Documentation

- Comprehensive inline godoc for all exported symbols
- `README.md` - Overview and quick start guide
- `ARCHITECTURE.md` - Design patterns and structure
- `EXAMPLES.md` - 20+ real-world usage examples
- `PERFORMANCE.md` - Benchmarks, optimization, and profiling
- `QUICKREF.md` - Quick reference guide
- API documentation via `go doc` and pkg.go.dev

#### CLI

- Command-line flags for all configuration options
- Comprehensive help text with examples
- Auto-detection of TTY vs non-TTY environments

### Design Patterns

- Layered Architecture
- Dependency Injection
- Strategy Pattern (multiple renderers)
- Observer Pattern (event-driven)
- Adapter Pattern (event conversion)
- Factory Pattern (constructors)
- Builder Pattern (fluent APIs)

### Platform Support

- Go 1.20+ required (uses `context.WithCancelCause`)
- Tested on: macOS, Linux
- Expected to work on: Windows
- TTY detection: Unix-compatible terminals

## [Unreleased]

### Planned Features

- Additional renderer implementations (JSON, metrics)
- Remote execution support via SSH
- Plugin system for custom renderers
- Progress bar renderer
- Compressed output storage
- Adaptive channel buffering

---

**0.1.0** - 2024-11-20: Initial public release
