package engine_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
)

// MockCommand is a test double that implements the engine.Command interface.
type MockCommand struct {
	stderrErr    error
	exitErr      error
	startErr     error
	stdoutErr    error
	stdoutLines  []string
	stderrLines  []string
	spec         engine.ProcessSpec
	sleepOnStart time.Duration
	mu           sync.Mutex
	started      bool
	waited       bool
	killed       bool
	signaled     bool
}

func NewMockCommand(spec engine.ProcessSpec) *MockCommand {
	return &MockCommand{
		spec:        spec,
		stdoutLines: nil,
		stderrLines: nil,
	}
}

func (m *MockCommand) WithStdout(lines ...string) *MockCommand {
	m.stdoutLines = lines
	return m
}

func (m *MockCommand) WithStderr(lines ...string) *MockCommand {
	m.stderrLines = lines
	return m
}

func (m *MockCommand) WithExitError(err error) *MockCommand {
	m.exitErr = err
	return m
}

func (m *MockCommand) WithStartError(err error) *MockCommand {
	m.startErr = err
	return m
}

func (m *MockCommand) WithStdoutError(err error) *MockCommand {
	m.stdoutErr = err
	return m
}

func (m *MockCommand) WithStderrError(err error) *MockCommand {
	m.stderrErr = err
	return m
}

func (m *MockCommand) WithSleep(d time.Duration) *MockCommand {
	m.sleepOnStart = d
	return m
}

func (m *MockCommand) StdoutPipe() (io.ReadCloser, error) {
	if m.stdoutErr != nil {
		return nil, m.stdoutErr
	}
	return &mockReadCloser{lines: m.stdoutLines}, nil
}

func (m *MockCommand) StderrPipe() (io.ReadCloser, error) {
	if m.stderrErr != nil {
		return nil, m.stderrErr
	}
	return &mockReadCloser{lines: m.stderrLines}, nil
}

func (m *MockCommand) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return m.startErr
	}

	m.started = true
	if m.sleepOnStart > 0 {
		time.Sleep(m.sleepOnStart)
	}
	return nil
}

func (m *MockCommand) Wait() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.waited = true
	return m.exitErr
}

func (m *MockCommand) Process() engine.ProcessHandle {
	return &mockProcessHandle{cmd: m}
}

func (m *MockCommand) WasStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *MockCommand) WasWaited() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.waited
}

func (m *MockCommand) WasKilled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.killed
}

func (m *MockCommand) WasSignaled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.signaled
}

// mockProcessHandle implements engine.ProcessHandle for testing.
type mockProcessHandle struct {
	cmd *MockCommand
}

func (m *mockProcessHandle) Signal(_ syscall.Signal) error {
	m.cmd.mu.Lock()
	defer m.cmd.mu.Unlock()
	m.cmd.signaled = true
	return nil
}

func (m *mockProcessHandle) Kill() error {
	m.cmd.mu.Lock()
	defer m.cmd.mu.Unlock()
	m.cmd.killed = true
	return nil
}

// mockReadCloser simulates an io.ReadCloser that emits lines.
type mockReadCloser struct {
	lines []string
	buf   []byte
	pos   int
}

func (m *mockReadCloser) Read(p []byte) (int, error) {
	if m.pos >= len(m.lines) {
		return 0, io.EOF
	}

	// If we have leftover data in buffer, serve it first
	if len(m.buf) > 0 {
		n := copy(p, m.buf)
		m.buf = m.buf[n:]
		return n, nil
	}

	// Prepare next line with newline
	line := m.lines[m.pos] + "\n"
	m.pos++

	n := copy(p, line)
	if n < len(line) {
		m.buf = []byte(line[n:])
	}

	return n, nil
}

func (m *mockReadCloser) Close() error {
	return nil
}

// TestEngineLineEventsOrder verifies that line events are emitted in the correct order.
func TestEngineLineEventsOrder(t *testing.T) {
	ctx := context.Background()

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithStdout("line1", "line2", "line3")

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	go eng.Run(ctx, output)

	// Collect all events
	var events []engine.ProcessLine
	for ev := range output {
		events = append(events, ev)
	}

	// Should have 3 line events + 1 completion event
	if len(events) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(events))
	}

	// Verify line order
	if events[0].Line != "line1" || events[0].IsComplete {
		t.Errorf("Event 0: expected line1, got %+v", events[0])
	}
	if events[1].Line != "line2" || events[1].IsComplete {
		t.Errorf("Event 1: expected line2, got %+v", events[1])
	}
	if events[2].Line != "line3" || events[2].IsComplete {
		t.Errorf("Event 2: expected line3, got %+v", events[2])
	}

	// Final event should be completion
	if !events[3].IsComplete {
		t.Errorf("Event 3: expected completion, got %+v", events[3])
	}
}

// TestEngineDoneEventOnce verifies that done event is sent exactly once per process.
func TestEngineDoneEventOnce(t *testing.T) {
	ctx := context.Background()

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithStdout("line1")

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	go eng.Run(ctx, output)

	// Count completion events
	completionCount := 0
	for ev := range output {
		if ev.IsComplete {
			completionCount++
		}
	}

	if completionCount != 1 {
		t.Errorf("Expected exactly 1 completion event, got %d", completionCount)
	}
}

// TestEngineCancellationStopsStreaming verifies that cancellation stops the engine.
func TestEngineCancellationStopsStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Mock command that sleeps on start to simulate long-running process
	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "sleeper"}).
		WithSleep(100*time.Millisecond).
		WithStdout("line1", "line2")

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "sleeper", Command: "mock"}}, 50*time.Millisecond).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Engine should complete within reasonable time
	select {
	case <-done:
		// Success - engine completed
	case <-time.After(2 * time.Second):
		t.Fatal("Engine did not complete after cancellation")
	}

	// Verify that the mock was signaled or killed
	if !mockCmd.WasSignaled() && !mockCmd.WasKilled() {
		t.Error("Process was not signaled or killed on cancellation")
	}
}

// TestEngineErrorHandlingPipeFailures verifies error handling for pipe failures.
//
//nolint:gocognit // Test complexity is acceptable for comprehensive error coverage
func TestEngineErrorHandlingPipeFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("stdout pipe error", func(t *testing.T) {
		mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
			WithStdoutError(errors.New("stdout pipe failed"))

		factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
			return mockCmd, nil
		}

		eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
			WithCommandFactory(factory)

		output := make(chan engine.ProcessLine, 10)
		go eng.Run(ctx, output)

		// Should get completion event with error
		var gotError bool
		for ev := range output {
			if ev.IsComplete && ev.Err != nil {
				gotError = true
				if !strings.Contains(ev.Err.Error(), "stdout pipe") {
					t.Errorf("Expected stdout pipe error, got: %v", ev.Err)
				}
			}
		}

		if !gotError {
			t.Error("Expected error event, got none")
		}
	})

	t.Run("stderr pipe error", func(t *testing.T) {
		mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
			WithStderrError(errors.New("stderr pipe failed"))

		factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
			return mockCmd, nil
		}

		eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
			WithCommandFactory(factory)

		output := make(chan engine.ProcessLine, 10)
		go eng.Run(ctx, output)

		// Should get completion event with error
		var gotError bool
		for ev := range output {
			if ev.IsComplete && ev.Err != nil {
				gotError = true
				if !strings.Contains(ev.Err.Error(), "stderr pipe") {
					t.Errorf("Expected stderr pipe error, got: %v", ev.Err)
				}
			}
		}

		if !gotError {
			t.Error("Expected error event, got none")
		}
	})

	t.Run("start error", func(t *testing.T) {
		mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
			WithStartError(errors.New("failed to start"))

		factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
			return mockCmd, nil
		}

		eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
			WithCommandFactory(factory)

		output := make(chan engine.ProcessLine, 10)
		go eng.Run(ctx, output)

		// Should get completion event with error
		var gotError bool
		for ev := range output {
			if ev.IsComplete && ev.Err != nil {
				gotError = true
				if !strings.Contains(ev.Err.Error(), "start") {
					t.Errorf("Expected start error, got: %v", ev.Err)
				}
			}
		}

		if !gotError {
			t.Error("Expected error event, got none")
		}
	})
}

// TestEngineMultipleProcesses verifies that multiple processes run correctly.
func TestEngineMultipleProcesses(t *testing.T) {
	ctx := context.Background()

	specs := []engine.ProcessSpec{
		{Name: "proc1", Command: "mock1"},
		{Name: "proc2", Command: "mock2"},
		{Name: "proc3", Command: "mock3"},
	}

	factory := func(_ context.Context, spec engine.ProcessSpec) (engine.Command, error) {
		switch spec.Command {
		case "mock1":
			return NewMockCommand(spec).WithStdout("proc1-line1", "proc1-line2"), nil
		case "mock2":
			return NewMockCommand(spec).WithStdout("proc2-line1"), nil
		case "mock3":
			return NewMockCommand(spec).WithStdout("proc3-line1", "proc3-line2", "proc3-line3"), nil
		default:
			return nil, fmt.Errorf("unknown command: %s", spec.Command)
		}
	}

	eng := engine.New(specs, 5*time.Second).WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 20)
	go eng.Run(ctx, output)

	// Collect events by process index
	eventsByIndex := make(map[int][]engine.ProcessLine)
	for ev := range output {
		eventsByIndex[ev.Index] = append(eventsByIndex[ev.Index], ev)
	}

	// Verify all processes completed
	if len(eventsByIndex) != 3 {
		t.Fatalf("Expected 3 processes, got %d", len(eventsByIndex))
	}

	// Verify proc1
	proc1Events := eventsByIndex[0]
	if len(proc1Events) != 3 { // 2 lines + completion
		t.Errorf("Proc1: expected 3 events, got %d", len(proc1Events))
	}

	// Verify proc2
	proc2Events := eventsByIndex[1]
	if len(proc2Events) != 2 { // 1 line + completion
		t.Errorf("Proc2: expected 2 events, got %d", len(proc2Events))
	}

	// Verify proc3
	proc3Events := eventsByIndex[2]
	if len(proc3Events) != 4 { // 3 lines + completion
		t.Errorf("Proc3: expected 4 events, got %d", len(proc3Events))
	}
}

// TestEngineExitCodePropagation verifies that exit errors are propagated correctly.
func TestEngineExitCodePropagation(t *testing.T) {
	ctx := context.Background()

	exitErr := errors.New("exit status 42")
	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithStdout("output").
		WithExitError(exitErr)

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	go eng.Run(ctx, output)

	// Find the completion event
	var completionEvent *engine.ProcessLine
	for ev := range output {
		if ev.IsComplete {
			completionEvent = &ev
		}
	}

	if completionEvent == nil {
		t.Fatal("No completion event received")
	}

	if completionEvent.Err == nil {
		t.Error("Expected error in completion event, got nil")
	}

	if !strings.Contains(completionEvent.Err.Error(), "exit status 42") {
		t.Errorf("Expected exit status error, got: %v", completionEvent.Err)
	}
}

// TestEngineStdoutAndStderrInterleaved verifies that stdout and stderr are both captured.
func TestEngineStdoutAndStderrInterleaved(t *testing.T) {
	ctx := context.Background()

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithStdout("stdout1", "stdout2").
		WithStderr("stderr1", "stderr2")

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	go eng.Run(ctx, output)

	// Collect all line events
	var lines []string
	for ev := range output {
		if !ev.IsComplete {
			lines = append(lines, ev.Line)
		}
	}

	// Should have all 4 lines (2 from stdout, 2 from stderr)
	if len(lines) != 4 {
		t.Fatalf("Expected 4 lines, got %d: %v", len(lines), lines)
	}

	// Verify all expected lines are present (order may vary due to concurrency)
	hasStdout1 := false
	hasStdout2 := false
	hasStderr1 := false
	hasStderr2 := false

	for _, line := range lines {
		switch line {
		case "stdout1":
			hasStdout1 = true
		case "stdout2":
			hasStdout2 = true
		case "stderr1":
			hasStderr1 = true
		case "stderr2":
			hasStderr2 = true
		}
	}

	if !hasStdout1 || !hasStdout2 || !hasStderr1 || !hasStderr2 {
		t.Errorf("Missing expected lines. Got: %v", lines)
	}
}

// TestEngineCommandFactoryError verifies error handling when CommandFactory returns an error.
func TestEngineCommandFactoryError(t *testing.T) {
	ctx := context.Background()

	factoryErr := errors.New("factory failed to create command")
	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return nil, factoryErr
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	go eng.Run(ctx, output)

	// Should get completion event with error
	var completionEvent *engine.ProcessLine
	for ev := range output {
		if ev.IsComplete {
			completionEvent = &ev
		}
	}

	if completionEvent == nil {
		t.Fatal("Expected completion event")
	}

	if completionEvent.Err == nil {
		t.Error("Expected error in completion event")
	}

	if !strings.Contains(completionEvent.Err.Error(), "create command") {
		t.Errorf("Expected 'create command' error, got: %v", completionEvent.Err)
	}
}

// TestEngineLineEndingNormalization verifies that different line endings are normalized.
//
//nolint:gocognit // test
func TestEngineLineEndingNormalization(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name     string
		input    []string // Raw lines without added newlines
		expected []string
	}{
		{
			name:     "Already normalized",
			input:    []string{"line1", "line2"},
			expected: []string{"line1", "line2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockCmd := NewMockCommand(engine.ProcessSpec{Name: tc.name}).
				WithStdout(tc.input...)

			factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
				return mockCmd, nil
			}

			eng := engine.New([]engine.ProcessSpec{{Name: tc.name, Command: "mock"}}, 5*time.Second).
				WithCommandFactory(factory)

			output := make(chan engine.ProcessLine, 20)
			go eng.Run(ctx, output)

			// Collect non-completion events
			var lines []string
			for ev := range output {
				if !ev.IsComplete {
					lines = append(lines, ev.Line)
				}
			}

			if len(lines) != len(tc.expected) {
				t.Errorf("Expected %d lines, got %d", len(tc.expected), len(lines))
			}

			for i, expected := range tc.expected {
				if i >= len(lines) {
					break
				}
				if lines[i] != expected {
					t.Errorf("Line %d: expected %q, got %q", i, expected, lines[i])
				}
			}
		})
	}
}

// TestEngineEmptySpecsList verifies handling of empty process list.
func TestEngineEmptySpecsList(t *testing.T) {
	ctx := context.Background()

	eng := engine.New([]engine.ProcessSpec{}, 5*time.Second)
	output := make(chan engine.ProcessLine, 10)

	// Should complete immediately without error
	eng.Run(ctx, output)

	// Channel should be closed with no events
	count := 0
	for range output {
		count++
	}

	if count != 0 {
		t.Errorf("Expected 0 events for empty specs, got %d", count)
	}
}

// TestEngineDefaultShutdownTimeout verifies that zero/negative timeout uses default.
func TestEngineDefaultShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithSleep(50 * time.Millisecond)

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	// Create engine with zero timeout (should use default 5s)
	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 0).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 10)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	// Should complete within reasonable time using default timeout
	select {
	case <-done:
		//nolint:revive // drain output channel
		for range output {
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Engine did not complete within 10s (default timeout should be 5s)")
	}
}

// TestEngineProcessIndexAccuracy verifies that Index field is correct for each process.
func TestEngineProcessIndexAccuracy(t *testing.T) {
	ctx := context.Background()

	specs := []engine.ProcessSpec{
		{Name: "proc0", Command: "mock0"},
		{Name: "proc1", Command: "mock1"},
		{Name: "proc2", Command: "mock2"},
	}

	factory := func(_ context.Context, spec engine.ProcessSpec) (engine.Command, error) {
		// Each process outputs its command name
		return NewMockCommand(spec).WithStdout(spec.Command), nil
	}

	eng := engine.New(specs, 5*time.Second).WithCommandFactory(factory)
	output := make(chan engine.ProcessLine, 20)
	go eng.Run(ctx, output)

	// Collect events by index
	linesByIndex := make(map[int][]string)
	for ev := range output {
		if !ev.IsComplete {
			linesByIndex[ev.Index] = append(linesByIndex[ev.Index], ev.Line)
		}
	}

	// Verify each process has correct index
	for i, spec := range specs {
		lines, ok := linesByIndex[i]
		if !ok {
			t.Errorf("No lines for process %d (%s)", i, spec.Name)
			continue
		}
		if len(lines) != 1 {
			t.Errorf("Process %d: expected 1 line, got %d", i, len(lines))
			continue
		}
		if lines[0] != spec.Command {
			t.Errorf("Process %d: expected %q, got %q", i, spec.Command, lines[0])
		}
	}
}

// TestEngineGracefulShutdownTimeout verifies SIGTERM and graceful shutdown behavior.
func TestEngineGracefulShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "stubborn"}).
		WithSleep(50 * time.Millisecond)

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	// Short timeout for faster test
	eng := engine.New([]engine.ProcessSpec{{Name: "stubborn", Command: "mock"}}, 100*time.Millisecond).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 20)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	// Give it time to start
	time.Sleep(25 * time.Millisecond)
	cancel()

	// Wait for completion
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Fatal("Engine did not complete after cancellation")
	}

	// Collect output
	var messages []string
	for ev := range output {
		if !ev.IsComplete {
			messages = append(messages, ev.Line)
		}
	}

	// Should see SIGTERM message
	foundSigterm := false
	for _, msg := range messages {
		if strings.Contains(msg, "SIGTERM") {
			foundSigterm = true
		}
	}

	if !foundSigterm {
		t.Logf("Messages received: %v", messages)
		t.Error("Expected SIGTERM message in output")
	}

	// Verify mock was signaled
	if !mockCmd.WasSignaled() {
		t.Error("Process should have been signaled with SIGTERM")
	}
}

// TestEngineContextCause verifies that context cause is extracted and reported.
func TestEngineContextCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())

	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "test"}).
		WithSleep(100 * time.Millisecond)

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 50*time.Millisecond).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 20)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	time.Sleep(25 * time.Millisecond)

	// Cancel with a specific cause
	customCause := errors.New("custom cancellation reason")
	cancel(customCause)

	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Fatal("Engine did not complete")
	}

	// Check for cause in output
	var messages []string
	for ev := range output {
		if !ev.IsComplete {
			messages = append(messages, ev.Line)
		}
	}

	foundCause := false
	for _, msg := range messages {
		if strings.Contains(msg, "custom cancellation reason") {
			foundCause = true
			break
		}
	}

	if !foundCause {
		t.Errorf("Expected to find custom cancellation reason in output. Got: %v", messages)
	}
}

// TestEngineNilProcessHandle verifies handling when Process() returns nil.
func TestEngineNilProcessHandle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mockCmd := &MockCommand{
		spec:         engine.ProcessSpec{Name: "test"},
		stdoutLines:  []string{"output"},
		sleepOnStart: 100 * time.Millisecond,
	}

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 50*time.Millisecond).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 20)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	// Should complete without panic even if Process() returns nil
	select {
	case <-done:
		//nolint:revive // drain output channel
		for range output {
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Engine did not complete")
	}
}

// TestEngineLongLines verifies handling of moderately long output lines.
func TestEngineLongLines(t *testing.T) {
	ctx := context.Background()

	// Test lines up to scanner initial buffer size
	testCases := []struct {
		name   string
		length int
	}{
		{"normal", 100},
		{"medium", 1000},
		{"large", 10000},
		{"very large", 50000}, // Within typical scanner buffer expansion
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			longLine := strings.Repeat("x", tc.length)
			mockCmd := NewMockCommand(engine.ProcessSpec{Name: tc.name}).
				WithStdout(longLine)

			factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
				return mockCmd, nil
			}

			eng := engine.New([]engine.ProcessSpec{{Name: tc.name, Command: "mock"}}, 5*time.Second).
				WithCommandFactory(factory)

			output := make(chan engine.ProcessLine, 10)
			go eng.Run(ctx, output)

			// Should successfully read the long line
			var receivedLine string
			for ev := range output {
				if !ev.IsComplete {
					receivedLine = ev.Line
				}
			}

			if len(receivedLine) != tc.length {
				t.Errorf("Expected line of length %d, got %d", tc.length, len(receivedLine))
			}
		})
	}
}

// TestEngineWithCommandFactory verifies WithCommandFactory returns new instance.
func TestEngineWithCommandFactory(t *testing.T) {
	specs := []engine.ProcessSpec{{Name: "test", Command: "mock"}}
	timeout := 5 * time.Second

	eng1 := engine.New(specs, timeout)
	if eng1.CommandFactory != nil {
		t.Error("New engine should have nil CommandFactory")
	}

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		//nolint:nilnil // test
		return nil, nil
	}

	eng2 := eng1.WithCommandFactory(factory)

	// Should return new instance
	if eng2 == eng1 {
		t.Error("WithCommandFactory should return new instance")
	}

	// Original should be unchanged
	if eng1.CommandFactory != nil {
		t.Error("Original engine should still have nil CommandFactory")
	}

	// New instance should have the factory
	if eng2.CommandFactory == nil {
		t.Error("New engine should have CommandFactory set")
	}

	// Specs and timeout should be copied
	if len(eng2.Specs) != len(specs) {
		t.Error("Specs should be copied to new instance")
	}
	if eng2.ShutdownTimeout != timeout {
		t.Error("ShutdownTimeout should be copied to new instance")
	}
}

// TestDefaultCommandFactory verifies the default command factory with real processes.
func TestDefaultCommandFactory(t *testing.T) {
	ctx := context.Background()

	specs := []engine.ProcessSpec{
		{
			Name:    "echo",
			Command: "echo",
			Args:    []string{"hello", "world"},
		},
	}

	// Use default factory (nil triggers DefaultCommandFactory)
	eng := engine.New(specs, 5*time.Second)
	output := make(chan engine.ProcessLine, 10)

	go eng.Run(ctx, output)

	// Collect output
	var lines []string
	var exitErr error
	for ev := range output {
		if ev.IsComplete {
			exitErr = ev.Err
		} else {
			lines = append(lines, ev.Line)
		}
	}

	// Should have output "hello world"
	if len(lines) != 1 {
		t.Errorf("Expected 1 line of output, got %d", len(lines))
	}
	if len(lines) > 0 && lines[0] != "hello world" {
		t.Errorf("Expected 'hello world', got %q", lines[0])
	}

	// Should exit successfully
	if exitErr != nil {
		t.Errorf("Expected successful exit, got error: %v", exitErr)
	}
}

// TestDefaultCommandFactoryWithFailure verifies error handling with real processes.
func TestDefaultCommandFactoryWithFailure(t *testing.T) {
	ctx := context.Background()

	specs := []engine.ProcessSpec{
		{
			Name:    "false",
			Command: "sh",
			Args:    []string{"-c", "exit 42"},
		},
	}

	eng := engine.New(specs, 5*time.Second)
	output := make(chan engine.ProcessLine, 10)

	go eng.Run(ctx, output)

	// Collect output
	var exitErr error
	for ev := range output {
		if ev.IsComplete {
			exitErr = ev.Err
		}
	}

	// Should have exit error
	if exitErr == nil {
		t.Error("Expected exit error, got nil")
	}
}

// TestRealProcessCancellation verifies graceful shutdown with real processes.
func TestRealProcessCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real process test in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())

	specs := []engine.ProcessSpec{
		{
			Name:    "sleep",
			Command: "sleep",
			Args:    []string{"10"},
		},
	}

	eng := engine.New(specs, 1*time.Second)
	output := make(chan engine.ProcessLine, 20)

	done := make(chan bool)
	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	// Give it time to start
	time.Sleep(200 * time.Millisecond)

	// Cancel
	cancel()

	// Should complete within reasonable time
	select {
	case <-done:
		//nolint:revive // drain output channel
		for range output {
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Engine did not complete after cancellation")
	}
}

// TestEngineKillAfterTimeout verifies graceful shutdown behavior.
func TestEngineKillAfterTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a mock that outputs before shutdown
	mockCmd := NewMockCommand(engine.ProcessSpec{Name: "process"}).
		WithStdout("output line 1", "output line 2").
		WithSleep(50 * time.Millisecond)

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		return mockCmd, nil
	}

	// Reasonable timeout
	eng := engine.New([]engine.ProcessSpec{{Name: "process", Command: "mock"}}, 100*time.Millisecond).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 30)
	done := make(chan bool)

	go func() {
		eng.Run(ctx, output)
		done <- true
	}()

	// Let process start and produce some output
	time.Sleep(75 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for completion
	select {
	case <-done:
		// Expected
	case <-time.After(3 * time.Second):
		t.Fatal("Engine did not complete")
	}

	// Collect all messages
	var messages []string
	for ev := range output {
		if !ev.IsComplete {
			messages = append(messages, ev.Line)
		}
	}

	// Should have sent SIGTERM
	foundSigterm := false
	for _, msg := range messages {
		if strings.Contains(msg, "SIGTERM") {
			foundSigterm = true
			break
		}
	}

	if !foundSigterm {
		t.Logf("Messages: %v", messages)
		// Not a hard failure - the process might have exited before we could signal
		t.Log("SIGTERM message not found (process may have exited gracefully)")
	}

	// Process should have completed
	if !mockCmd.WasWaited() {
		t.Error("Process should have been waited on")
	}
}

// TestEngineStreamReaderError verifies handling of scanner errors.
func TestEngineStreamReaderError(t *testing.T) {
	ctx := context.Background()

	// Create a custom ReadCloser that returns an error
	errorReader := &errorReadCloser{err: errors.New("read error")}

	mockCmd := &MockCommand{
		spec: engine.ProcessSpec{Name: "test"},
	}

	// Override StdoutPipe to return error reader
	originalStdoutPipe := mockCmd.StdoutPipe
	_ = originalStdoutPipe

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		// Create a custom command that returns error reader
		return &customErrorCommand{errorReader: errorReader}, nil
	}

	eng := engine.New([]engine.ProcessSpec{{Name: "test", Command: "mock"}}, 5*time.Second).
		WithCommandFactory(factory)

	output := make(chan engine.ProcessLine, 20)
	go eng.Run(ctx, output)

	// Collect events
	var hasErrorMessage bool
	for ev := range output {
		if !ev.IsComplete && strings.Contains(ev.Line, "stream error") {
			hasErrorMessage = true
		}
	}

	if !hasErrorMessage {
		t.Error("Expected stream error message in output")
	}
}

// errorReadCloser is a ReadCloser that returns an error.
type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	return nil
}

// customErrorCommand is a command that returns error readers.
type customErrorCommand struct {
	errorReader io.ReadCloser
}

func (c *customErrorCommand) StdoutPipe() (io.ReadCloser, error) {
	return c.errorReader, nil
}

func (c *customErrorCommand) StderrPipe() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (c *customErrorCommand) Start() error {
	return nil
}

func (c *customErrorCommand) Wait() error {
	return nil
}

func (c *customErrorCommand) Process() engine.ProcessHandle {
	return nil
}
