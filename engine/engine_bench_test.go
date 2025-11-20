package engine_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
)

// BenchmarkEngineWithManyProcesses measures performance with many concurrent processes.
func BenchmarkEngineWithManyProcesses(b *testing.B) {
	numProcs := 50
	linesPerProc := 100

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		lines := make([]string, linesPerProc)
		for i := range linesPerProc {
			lines[i] = "benchmark output line " + string(rune('0'+i%10))
		}
		return &BenchCommand{stdout: lines}, nil
	}

	for b.Loop() {
		specs := make([]engine.ProcessSpec, numProcs)
		for j := range numProcs {
			specs[j] = engine.ProcessSpec{
				Name:    "bench-proc",
				Command: "mock",
			}
		}

		eng := engine.New(specs, 5*time.Second).WithCommandFactory(factory)
		output := make(chan engine.ProcessLine, 1024)

		ctx := context.Background()
		go eng.Run(ctx, output)

		// Consume all output
		for range output { //nolint:revive // Empty loop intentionally consumes all output
		}
	}
}

// BenchmarkEngineWithHighFrequencyOutput measures performance with rapid output.
func BenchmarkEngineWithHighFrequencyOutput(b *testing.B) {
	numLines := 10000

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		lines := make([]string, numLines)
		for i := range numLines {
			lines[i] = "high frequency output line"
		}
		return &BenchCommand{stdout: lines}, nil
	}

	for b.Loop() {
		specs := []engine.ProcessSpec{
			{Name: "high-freq", Command: "mock"},
		}

		eng := engine.New(specs, 5*time.Second).WithCommandFactory(factory)
		output := make(chan engine.ProcessLine, 1024)

		ctx := context.Background()
		go eng.Run(ctx, output)

		lineCount := 0
		for pl := range output {
			if !pl.IsComplete {
				lineCount++
			}
		}

		if lineCount != numLines {
			b.Fatalf("Expected %d lines, got %d", numLines, lineCount)
		}
	}
}

// BenchmarkEngineWithLargeLines measures performance with very long lines.
func BenchmarkEngineWithLargeLines(b *testing.B) {
	lineSize := 10000 // 10KB per line
	numLines := 100

	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		lines := make([]string, numLines)
		longLine := strings.Repeat("x", lineSize)
		for i := range numLines {
			lines[i] = longLine
		}
		return &BenchCommand{stdout: lines}, nil
	}

	for b.Loop() {
		specs := []engine.ProcessSpec{
			{Name: "large-lines", Command: "mock"},
		}

		eng := engine.New(specs, 5*time.Second).WithCommandFactory(factory)
		output := make(chan engine.ProcessLine, 128)

		ctx := context.Background()
		go eng.Run(ctx, output)

		//nolint:revive // Empty loop intentionally consumes all output
		for range output {
			// Consume all output
		}
	}
}

// BenchmarkEngineCancellation measures graceful shutdown performance.
func BenchmarkEngineCancellation(b *testing.B) {
	factory := func(_ context.Context, _ engine.ProcessSpec) (engine.Command, error) {
		// Simulate long-running process
		cmd := &BenchCommand{
			stdout:    make([]string, 0),
			sleepTime: 100 * time.Millisecond,
		}
		return cmd, nil
	}

	for b.Loop() {
		specs := []engine.ProcessSpec{
			{Name: "long-running", Command: "mock"},
		}

		eng := engine.New(specs, 100*time.Millisecond).WithCommandFactory(factory)
		output := make(chan engine.ProcessLine, 128)

		ctx, cancel := context.WithCancel(context.Background())
		go eng.Run(ctx, output)

		// Let it start
		time.Sleep(10 * time.Millisecond)

		// Cancel and measure shutdown time
		cancel()

		//nolint:revive // Empty loop intentionally consumes all output
		for range output {
			// Consume all output
		}
	}
}

// BenchmarkChannelThroughput measures raw channel throughput.
func BenchmarkChannelThroughput(b *testing.B) {
	numMessages := 10000

	for b.Loop() {
		ch := make(chan engine.ProcessLine, 128)

		go func() {
			for range numMessages {
				ch <- engine.ProcessLine{
					Index:      0,
					Line:       "benchmark line",
					IsComplete: false,
				}
			}
			ch <- engine.ProcessLine{
				Index:      0,
				IsComplete: true,
			}
			close(ch)
		}()

		count := 0
		for range ch {
			count++
		}
	}
}

// BenchCommand is a minimal mock for benchmarking.
type BenchCommand struct {
	stdout    []string
	sleepTime time.Duration
	mu        sync.Mutex
	started   bool
}

func (b *BenchCommand) StdoutPipe() (io.ReadCloser, error) {
	return &benchReadCloser{lines: b.stdout}, nil
}

func (b *BenchCommand) StderrPipe() (io.ReadCloser, error) {
	return &benchReadCloser{lines: []string{}}, nil
}

func (b *BenchCommand) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = true
	if b.sleepTime > 0 {
		time.Sleep(b.sleepTime)
	}
	return nil
}

func (b *BenchCommand) Wait() error {
	return nil
}

func (b *BenchCommand) Process() engine.ProcessHandle {
	return &benchProcessHandle{}
}

type benchProcessHandle struct{}

func (b *benchProcessHandle) Signal(_ syscall.Signal) error { return nil }
func (b *benchProcessHandle) Kill() error                   { return nil }

type benchReadCloser struct {
	lines []string
	buf   []byte
	pos   int
}

func (b *benchReadCloser) Read(p []byte) (int, error) {
	if b.pos >= len(b.lines) {
		return 0, io.EOF
	}

	if len(b.buf) > 0 {
		n := copy(p, b.buf)
		b.buf = b.buf[n:]
		return n, nil
	}

	line := b.lines[b.pos] + "\n"
	b.pos++

	n := copy(p, line)
	if n < len(line) {
		b.buf = []byte(line[n:])
	}

	return n, nil
}

func (b *benchReadCloser) Close() error {
	return nil
}
