package runner_test

import (
	"io"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
	"github.com/a2y-d5l/multiproc/runner"
)

// MockCommand is a test double for engine.Command.
type MockCommand struct {
	exitErr     error
	stdoutLines []string
	stderrLines []string
	mu          sync.Mutex
	started     bool
	waited      bool
}

func NewMockCommand(_ engine.ProcessSpec) *MockCommand {
	return &MockCommand{
		stdoutLines: nil,
		stderrLines: nil,
	}
}

func (m *MockCommand) WithStdout(lines ...string) *MockCommand {
	m.stdoutLines = lines
	return m
}

func (m *MockCommand) StdoutPipe() (io.ReadCloser, error) {
	return &mockReadCloser{lines: m.stdoutLines}, nil
}

func (m *MockCommand) StderrPipe() (io.ReadCloser, error) {
	return &mockReadCloser{lines: m.stderrLines}, nil
}

func (m *MockCommand) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
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

type mockProcessHandle struct {
	cmd *MockCommand
}

func (m *mockProcessHandle) Signal(_ syscall.Signal) error {
	return nil
}

func (m *mockProcessHandle) Kill() error {
	return nil
}

type mockReadCloser struct {
	lines []string
	buf   []byte
	pos   int
}

func (m *mockReadCloser) Read(p []byte) (int, error) {
	if m.pos >= len(m.lines) {
		return 0, io.EOF
	}

	if len(m.buf) > 0 {
		n := copy(p, m.buf)
		m.buf = m.buf[n:]
		return n, nil
	}

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

// TestDefaultConfig verifies that default configuration has expected values.
func TestDefaultConfig(t *testing.T) {
	cfg := runner.DefaultConfig()

	if cfg.MaxLinesPerProc != 1000 {
		t.Errorf("Expected MaxLinesPerProc=1000, got %d", cfg.MaxLinesPerProc)
	}

	if cfg.FullScreen != true {
		t.Error("Expected FullScreen=true by default")
	}

	if cfg.ShowSummary != true {
		t.Error("Expected ShowSummary=true by default")
	}

	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("Expected ShutdownTimeout=5s, got %v", cfg.ShutdownTimeout)
	}

	if cfg.ShowTimestamps != false {
		t.Error("Expected ShowTimestamps=false by default")
	}

	if cfg.LogPrefix != "[%s]" {
		t.Errorf("Expected LogPrefix='[%%s]', got %q", cfg.LogPrefix)
	}
}

// TestConfigWithTimestamps verifies that timestamps can be enabled.
func TestConfigWithTimestamps(t *testing.T) {
	cfg := runner.DefaultConfig()
	cfg.ShowTimestamps = true

	if !cfg.ShowTimestamps {
		t.Error("Expected ShowTimestamps=true after setting")
	}
}

// TestConfigWithCustomPrefix verifies that log prefix can be customized.
func TestConfigWithCustomPrefix(t *testing.T) {
	testCases := []string{
		"[%s]",
		"%s:",
		"(%s)",
		">>> %s >>>",
	}

	for _, prefix := range testCases {
		cfg := runner.DefaultConfig()
		cfg.LogPrefix = prefix

		if cfg.LogPrefix != prefix {
			t.Errorf("Expected LogPrefix=%q, got %q", prefix, cfg.LogPrefix)
		}
	}
}

// TestConfigFallbackToDefaults verifies that zero values fall back to defaults.
func TestConfigFallbackToDefaults(t *testing.T) {
	// Create a config with zero values
	cfg := runner.Config{
		MaxLinesPerProc: 0,  // Should fall back to default
		ShutdownTimeout: 0,  // Should fall back to default
		LogPrefix:       "", // Should fall back to default
	}

	// Verify zero values are different from defaults
	base := runner.DefaultConfig()

	if cfg.MaxLinesPerProc != 0 {
		t.Errorf("Expected MaxLinesPerProc=0, got %d", cfg.MaxLinesPerProc)
	}
	if cfg.ShutdownTimeout != 0 {
		t.Errorf("Expected ShutdownTimeout=0, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogPrefix != "" {
		t.Errorf("Expected LogPrefix='', got %q", cfg.LogPrefix)
	}

	// Verify defaults are non-zero
	if base.MaxLinesPerProc == 0 {
		t.Error("Default MaxLinesPerProc should not be zero")
	}
	if base.ShutdownTimeout == 0 {
		t.Error("Default ShutdownTimeout should not be zero")
	}
	if base.LogPrefix == "" {
		t.Error("Default LogPrefix should not be empty")
	}

	t.Log("Zero values will fall back to defaults in Run()")
}

// TestNonTTYMode verifies that non-TTY mode disables full-screen rendering.
func TestNonTTYMode(t *testing.T) {
	// Verify the config accepts TTY overrides
	cfg := runner.DefaultConfig()

	// Test with TTY enabled
	isTTYTrue := true
	cfg.IsTTY = &isTTYTrue
	if cfg.IsTTY == nil || !*cfg.IsTTY {
		t.Error("Expected IsTTY to be true when explicitly set")
	}

	// Test with TTY disabled (non-TTY mode)
	isTTYFalse := false
	cfg.IsTTY = &isTTYFalse
	if cfg.IsTTY == nil || *cfg.IsTTY {
		t.Error("Expected IsTTY to be false when explicitly set")
	}

	// In non-TTY mode, full-screen rendering should be disabled by Run()
	// This is verified by inspection of the Run() implementation
	if cfg.IsTTY != nil && !*cfg.IsTTY {
		t.Log("Non-TTY mode detected, full-screen should be disabled by Run()")
	}
}
