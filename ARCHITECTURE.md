# Architecture Overview

## Package Dependencies

```txt
┌──────────────────────────────────────────────────────────┐
│                      Client Code                         │
│                   (Your Application)                     │
└────────────────────┬─────────────────────────────────────┘
                     │
                     │ import "runner"
                     ▼
┌──────────────────────────────────────────────────────────┐
│                    runner.Run()                          │
│              High-Level Orchestration                    │
│  • Config management                                     │
│  • TTY detection                                         │
│  • Event conversion                                      │
│  • Render coordination                                   │
└─────────┬─────────────────────┬──────────────────────────┘
          │                     │
          │ import              │ import
          │ "engine"            │ "renderer"
          ▼                     ▼
┌───────────────────┐   ┌──────────────────────────────────┐
│  engine.Engine    │   │     renderer.Renderer            │
│                   │   │                                  │
│  Core Execution   │   │  Output Formatting               │
│  • Process mgmt   │   │  • ProcessState                  │
│  • Graceful       │   │  • RenderScreen()                │
│    shutdown       │   │  • RenderIncremental()           │
│  • Line capture   │   │  • FormatExitError()             │
│  • Normalization  │   │  • ApplyEvent()                  │
└───────────────────┘   └──────────────────────────────────┘
          │                     │
          │                     │ uses ProcessSpec/ProcessLine
          └─────────┬───────────┘
                    │
        ┌───────────▼────────────┐
        │   Shared Types         │
        │  • ProcessSpec         │
        │  • ProcessLine         │
        │  • Command interface   │
        └────────────────────────┘
```

## Data Flow

```txt
User Code
   │
   │ creates Config + ProcessSpecs
   ▼
runner.Run(ctx, config)
   │
   ├──> Creates engine.Engine
   │    │
   │    │ Creates processes
   │    │ Monitors stdout/stderr
   │    │ Handles cancellation
   │    │ Graceful shutdown
   │    │
   │    └──> Emits ProcessLine events
   │              │
   │              │ (channel)
   │              ▼
   ├──> Event Converter
   │    │
   │    └──> Converts to renderer.Event
   │              │
   │              ▼
   └──> Renderer
        │
        ├──> ApplyEvent(states, event)
        │    │
        │    └──> Updates ProcessState
        │              │
        │              ▼
        └──> RenderScreen(states) / RenderIncremental()
                     │
                     └──> Output to terminal
```

## Layer Responsibilities

### Layer 1: Engine (Core)

**Location**: `multiproc/engine/`

**Purpose**: Execute processes, capture output, manage lifecycle

**Key Responsibilities**:

- Create and start processes via `Command` interface
- Capture stdout/stderr streams
- Normalize line endings
- Handle graceful shutdown (SIGTERM → SIGKILL)
- Emit raw `ProcessLine` events
- No knowledge of rendering

**External Dependencies**: None (standard library only)

**Testability**: 100% mockable via `CommandFactory`

---

### Layer 2: Renderer (Presentation)

**Location**: `multiproc/renderer/`

**Purpose**: Format output, manage display state

**Key Responsibilities**:

- Maintain `ProcessState` for each process
- Apply events to update state
- Enforce memory limits (lines/bytes)
- Render full-screen TTY UI
- Render incremental non-TTY output
- Format exit errors
- TTY detection

**External Dependencies**: `engine` (for ProcessSpec/ProcessLine types)

**Testability**: Pure functions for state management

---

### Layer 3: Runner (Orchestration)

**Location**: `multiproc/runner/`

**Purpose**: Tie engine and renderer together

**Key Responsibilities**:

- Accept high-level `Config`
- Create and configure `Engine`
- Convert engine events to renderer events
- Coordinate rendering modes (TTY vs non-TTY)
- Manage render debouncing
- Aggregate exit codes
- Provide simple API for users

**External Dependencies**: Both `engine` and `renderer`

**Testability**: Integration point, can be tested end-to-end

---

### Layer 4: CLI (Application)

**Location**: `multiproc/cmd/multiproc/`

**Purpose**: Command-line interface

**Key Responsibilities**:

- Parse command-line args (future)
- Set up signal handling
- Create ProcessSpecs
- Call runner.Run()
- Handle os.Exit()

**External Dependencies**: `runner` and `engine`

**Testability**: End-to-end smoke tests

## Interface Boundaries

### Engine → Renderer

```go
// Engine emits
type ProcessLine struct {
    Index      int
    Line       string
    IsComplete bool
    Err        error
}

// Renderer consumes
func ConvertProcessLineToEvent(pl engine.ProcessLine) Event
func ApplyEvent(states []ProcessState, ev Event)
```

### Engine → User (Direct Usage)

```go
// User creates
specs := []engine.ProcessSpec{...}
eng := engine.New(specs, timeout)

// User receives
output := make(chan engine.ProcessLine, 128)
go eng.Run(ctx, output)

for pl := range output {
    // Custom handling
}
```

### Runner → User (Simple Usage)

```go
// User creates
cfg := runner.DefaultConfig()
cfg.Specs = specs

// User receives
exitCode := runner.Run(ctx, cfg)
```

## Extension Points

### 1. Custom Command Implementation

```go
type MyRemoteCommand struct { ... }

func (c *MyRemoteCommand) Start() error { 
    // SSH to remote host
}

factory := func(ctx, spec) (engine.Command, error) {
    return &MyRemoteCommand{spec: spec}, nil
}

eng := engine.New(specs, timeout).WithCommandFactory(factory)
```

### 2. Custom Renderer

```go
// JSON Logger
output := make(chan engine.ProcessLine, 128)
go eng.Run(ctx, output)

for pl := range output {
    json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
        "index": pl.Index,
        "line": pl.Line,
        "complete": pl.IsComplete,
    })
}
```

### 3. Metrics Collection

```go
type Metrics struct {
    StartTimes map[int]time.Time
    EndTimes   map[int]time.Time
    LineCounts map[int]int
}

func (m *Metrics) Collect(output <-chan engine.ProcessLine) {
    for pl := range output {
        if pl.IsComplete {
            m.EndTimes[pl.Index] = time.Now()
        } else {
            m.LineCounts[pl.Index]++
        }
    }
}
```

## Design Patterns Used

1. **Layered Architecture**: Clear separation of concerns
2. **Dependency Injection**: `CommandFactory` for testability
3. **Strategy Pattern**: Different renderers for different environments
4. **Observer Pattern**: Event-driven via channels
5. **Adapter Pattern**: `ConvertProcessLineToEvent` bridges layers
6. **Factory Pattern**: `New()` constructors, `CommandFactory`
7. **Builder Pattern**: Fluent configuration APIs

## Testability Strategy

### Unit Tests (Engine)

- Mock `Command` implementation
- Test individual process lifecycle
- Test error handling
- Test cancellation
- No real processes needed
- Fast execution

### Integration Tests (Runner)

- Real processes with simple commands
- End-to-end flow testing
- TTY vs non-TTY modes
- Configuration validation

### Smoke Tests (CLI)

- Build verification
- Basic execution test
- Signal handling

## Performance Characteristics

### Memory Usage

- **Per Process**: O(MaxLines + MaxBytes)
- **Total**: O(NumProcesses × MaxLines)
- **Bounded**: Yes, via eviction policy

### Concurrency

- **Processes**: N goroutines (one per process)
- **Streams**: 2N goroutines (stdout + stderr per process)
- **Coordination**: Channels with buffering
- **Scalability**: Tested up to 100 concurrent processes

### Latency

- **Event emission**: <1ms (channel send)
- **Render debouncing**: Configurable, default 100ms
- **Shutdown timeout**: Configurable, default 5s

## Security Considerations

1. **Command Injection**: ProcessSpec requires explicit command + args
2. **Resource Limits**: MaxLines and MaxBytes prevent memory exhaustion
3. **Timeout Enforcement**: ShutdownTimeout prevents hung processes
4. **Signal Handling**: Proper SIGTERM → SIGKILL sequence
5. **Error Isolation**: Process failures don't affect others

## Extensibility

The architecture is designed for extension. Common extension points:

### Custom Renderers

Implement custom output formats by consuming `ProcessLine` events directly:

```go
// JSON logger, metrics collector, progress tracker, etc.
output := make(chan engine.ProcessLine, 128)
go eng.Run(ctx, output)

for pl := range output {
    // Custom handling
}
```

### Remote Execution

Implement the `Command` interface for remote process execution:

```go
type SSHCommand struct { ... }

factory := func(ctx, spec) (engine.Command, error) {
    return &SSHCommand{spec: spec}, nil
}

eng := engine.New(specs, timeout).WithCommandFactory(factory)
```

### Plugin Systems

The clean package boundaries enable plugin architectures:

- Load renderer plugins dynamically
- Register custom command factories
- Extend configuration options
- Add middleware to event streams

See [EXAMPLES.md](EXAMPLES.md) for detailed implementation examples.
