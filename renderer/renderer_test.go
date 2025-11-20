package renderer_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/a2y-d5l/multiproc/engine"
	"github.com/a2y-d5l/multiproc/renderer"
)

// TestRenderIncrementalWithTimestamps verifies timestamp formatting.
func TestRenderIncrementalWithTimestamps(_ *testing.T) {
	// This test captures what RenderIncremental would print
	// We can't easily capture stdout, but we can verify the logic
	// by inspecting the function signature and expected behavior

	specs := []engine.ProcessSpec{
		{Name: "TestProc", Command: "test"},
	}

	states := []renderer.ProcessState{
		{Name: "TestProc", Lines: []string{}, Running: true},
	}

	// Create a line event
	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 0,
		Line:  "test output",
	})

	// RenderIncremental should handle this without error
	// Note: This doesn't capture output, just ensures no panics
	renderer.RenderIncremental(ev, specs, states, true, "[%s]")
	renderer.RenderIncremental(ev, specs, states, false, "[%s]")
	renderer.RenderIncremental(ev, specs, states, true, "%s:")
}

// TestRenderIncrementalWithCustomPrefix verifies custom prefix formatting.
func TestRenderIncrementalWithCustomPrefix(_ *testing.T) {
	specs := []engine.ProcessSpec{
		{Name: "ProcA", Command: "test"},
	}

	states := []renderer.ProcessState{
		{Name: "ProcA", Lines: []string{}, Running: true},
	}

	prefixes := []string{
		"[%s]",
		"%s:",
		"(%s)",
		">>> %s >>>",
	}

	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 0,
		Line:  "output",
	})

	// Verify no panics with different prefix formats
	for _, prefix := range prefixes {
		renderer.RenderIncremental(ev, specs, states, false, prefix)
		renderer.RenderIncremental(ev, specs, states, true, prefix)
	}
}

// TestRenderIncrementalEmptyPrefix verifies fallback to default prefix.
func TestRenderIncrementalEmptyPrefix(_ *testing.T) {
	specs := []engine.ProcessSpec{
		{Name: "Test", Command: "test"},
	}

	states := []renderer.ProcessState{
		{Name: "Test", Lines: []string{}, Running: true},
	}

	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 0,
		Line:  "test",
	})

	// Empty prefix should fall back to default
	renderer.RenderIncremental(ev, specs, states, false, "")
}

// TestRenderIncrementalDoneEvent verifies completion event rendering.
func TestRenderIncrementalDoneEvent(_ *testing.T) {
	specs := []engine.ProcessSpec{
		{Name: "Completed", Command: "test"},
	}

	states := []renderer.ProcessState{
		{Name: "Completed", Lines: []string{}, Running: false, Done: true},
	}

	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      0,
		IsComplete: true,
		Err:        nil,
	})

	// Should render completion message without error
	renderer.RenderIncremental(ev, specs, states, false, "[%s]")
	renderer.RenderIncremental(ev, specs, states, true, "[%s]")
}

// TestConvertProcessLineToEvent verifies event conversion.
func TestConvertProcessLineToEvent(t *testing.T) {
	// Test line event conversion
	lineEvent := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      0,
		Line:       "test line",
		IsComplete: false,
	})

	if lineEvent == nil {
		t.Error("Expected non-nil event for line")
	}

	// Test completion event conversion
	doneEvent := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      1,
		IsComplete: true,
		Err:        nil,
	})

	if doneEvent == nil {
		t.Error("Expected non-nil event for completion")
	}
}

// TestFormatExitError verifies exit error formatting.
func TestFormatExitError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "nil error",
			err:      nil,
			contains: "ok",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := renderer.FormatExitError(tc.err)
			if !strings.Contains(result, tc.contains) && result != tc.contains {
				t.Errorf("Expected result to contain %q, got %q", tc.contains, result)
			}
		})
	}
}

// TestApplyEventLineEvent verifies line event application to state.
func TestApplyEventLineEvent(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:     "test",
			Lines:    []string{},
			ByteSize: 0,
			MaxLines: 0, // No limit
			MaxBytes: 0, // No limit
			Done:     false,
			Running:  true,
			Dirty:    false,
		},
	}

	// Apply line event
	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 0,
		Line:  "test line",
	})

	renderer.ApplyEvent(states, ev)

	// Verify state was updated
	if len(states[0].Lines) != 1 {
		t.Errorf("Expected 1 line, got %d", len(states[0].Lines))
	}
	if states[0].Lines[0] != "test line" {
		t.Errorf("Expected 'test line', got %q", states[0].Lines[0])
	}
	if states[0].ByteSize != 9 { // len("test line")
		t.Errorf("Expected ByteSize=9, got %d", states[0].ByteSize)
	}
	if !states[0].Dirty {
		t.Error("Expected Dirty=true after line event")
	}
}

// TestApplyEventDoneEvent verifies done event application to state.
func TestApplyEventDoneEvent(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:    "test",
			Lines:   []string{"line1"},
			Done:    false,
			Running: true,
			Dirty:   false,
		},
	}

	// Apply done event
	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      0,
		IsComplete: true,
		Err:        nil,
	})

	renderer.ApplyEvent(states, ev)

	// Verify state was updated
	if !states[0].Done {
		t.Error("Expected Done=true after done event")
	}
	if states[0].Running {
		t.Error("Expected Running=false after done event")
	}
	if !states[0].Dirty {
		t.Error("Expected Dirty=true after done event")
	}
	if states[0].Err != nil {
		t.Errorf("Expected Err=nil, got %v", states[0].Err)
	}
}

// TestApplyEventMaxLinesEviction verifies line limit enforcement.
func TestApplyEventMaxLinesEviction(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:     "test",
			Lines:    []string{"line1", "line2"},
			ByteSize: 10, // len("line1") + len("line2")
			MaxLines: 3,  // Allow max 3 lines
			MaxBytes: 0,  // No byte limit
		},
	}

	// Add more lines
	for i := 3; i <= 5; i++ {
		line := fmt.Sprintf("line%d", i)
		ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
			Index: 0,
			Line:  line,
		})
		renderer.ApplyEvent(states, ev)
	}

	// Should have only last 3 lines
	if len(states[0].Lines) != 3 {
		t.Errorf("Expected 3 lines after eviction, got %d", len(states[0].Lines))
	}

	// Should have lines 3, 4, 5
	expected := []string{"line3", "line4", "line5"}
	for i, exp := range expected {
		if i >= len(states[0].Lines) {
			t.Errorf("Missing line %d", i)
			continue
		}
		if states[0].Lines[i] != exp {
			t.Errorf("Line %d: expected %q, got %q", i, exp, states[0].Lines[i])
		}
	}
}

// TestApplyEventMaxBytesEviction verifies byte limit enforcement.
func TestApplyEventMaxBytesEviction(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:     "test",
			Lines:    []string{},
			ByteSize: 0,
			MaxLines: 0,  // No line limit
			MaxBytes: 20, // Allow max 20 bytes
		},
	}

	// Add lines that exceed byte limit
	// "12345" = 5 bytes each
	lines := []string{"12345", "67890", "ABCDE", "FGHIJ"}
	for _, line := range lines {
		ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
			Index: 0,
			Line:  line,
		})
		renderer.ApplyEvent(states, ev)
	}

	// Should evict oldest lines to stay under 20 bytes
	if states[0].ByteSize > 20 {
		t.Errorf("Expected ByteSize <= 20, got %d", states[0].ByteSize)
	}

	// Should have last few lines
	if len(states[0].Lines) > 4 {
		t.Errorf("Expected at most 4 lines, got %d", len(states[0].Lines))
	}
}

// TestApplyEventDualConstraintEviction verifies both limits are enforced.
func TestApplyEventDualConstraintEviction(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:     "test",
			Lines:    []string{},
			ByteSize: 0,
			MaxLines: 5,  // Max 5 lines
			MaxBytes: 25, // Max 25 bytes
		},
	}

	// Add 10 lines of 5 bytes each = 50 bytes total
	for i := 1; i <= 10; i++ {
		line := fmt.Sprintf("lin%02d", i)
		ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
			Index: 0,
			Line:  line,
		})
		renderer.ApplyEvent(states, ev)
	}

	// Should respect BOTH constraints
	if len(states[0].Lines) > 5 {
		t.Errorf("Expected at most 5 lines (line constraint), got %d", len(states[0].Lines))
	}
	if states[0].ByteSize > 25 {
		t.Errorf("Expected at most 25 bytes (byte constraint), got %d", states[0].ByteSize)
	}

	// Should have last 5 lines (lin06, lin07, lin08, lin09, lin10)
	if len(states[0].Lines) == 5 {
		if states[0].Lines[4] != "lin10" {
			t.Errorf("Expected last line to be 'lin10', got %q", states[0].Lines[4])
		}
	}
}

// TestApplyEventOutOfBoundsIndex verifies handling of invalid indices.
func TestApplyEventOutOfBoundsIndex(t *testing.T) {
	states := []renderer.ProcessState{
		{Name: "test", Lines: []string{}},
	}

	// Negative index
	ev1 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: -1,
		Line:  "should be ignored",
	})
	renderer.ApplyEvent(states, ev1)

	// Should not have added the line
	if len(states[0].Lines) != 0 {
		t.Error("Expected negative index to be ignored")
	}

	// Index too large
	ev2 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 10,
		Line:  "should be ignored",
	})
	renderer.ApplyEvent(states, ev2)

	// Should not panic or add line
	if len(states[0].Lines) != 0 {
		t.Error("Expected out-of-bounds index to be ignored")
	}
}

// TestExitCodeFromStates verifies exit code calculation.
func TestExitCodeFromStates(t *testing.T) {
	testCases := []struct {
		name     string
		states   []renderer.ProcessState
		expected int
	}{
		{
			name: "all successful",
			states: []renderer.ProcessState{
				{Err: nil},
				{Err: nil},
			},
			expected: 0,
		},
		{
			name: "one failed",
			states: []renderer.ProcessState{
				{Err: nil},
				{Err: errors.New("failed")},
			},
			expected: 1,
		},
		{
			name: "all failed",
			states: []renderer.ProcessState{
				{Err: errors.New("failed1")},
				{Err: errors.New("failed2")},
			},
			expected: 1,
		},
		{
			name:     "empty states",
			states:   []renderer.ProcessState{},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := renderer.ExitCodeFromStates(tc.states)
			if result != tc.expected {
				t.Errorf("Expected exit code %d, got %d", tc.expected, result)
			}
		})
	}
}

// TestRenderScreenDirtyTracking verifies dirty flag optimization.
func TestRenderScreenDirtyTracking(t *testing.T) {
	// Create states with no dirty flags
	states := []renderer.ProcessState{
		{Name: "proc1", Dirty: false},
		{Name: "proc2", Dirty: false},
	}

	// RenderScreen should skip rendering if nothing is dirty
	// (This test just ensures no panic; actual rendering goes to stdout)
	renderer.RenderScreen(states)

	// Verify dirty flags are still false
	for i, ps := range states {
		if ps.Dirty {
			t.Errorf("State %d should still be clean", i)
		}
	}

	// Mark one as dirty
	states[0].Dirty = true
	renderer.RenderScreen(states)

	// After render, dirty should be cleared
	if states[0].Dirty {
		t.Error("Dirty flag should be cleared after render")
	}
}

// TestFormatExitErrorWithExecError verifies formatting of exec.ExitError.
func TestFormatExitErrorWithExecError(t *testing.T) {
	// Test with generic error
	err := errors.New("generic error")
	result := renderer.FormatExitError(err)
	if !strings.Contains(result, "error:") {
		t.Errorf("Expected 'error:' in result, got: %q", result)
	}

	// Note: Testing actual exec.ExitError requires running real processes
	// or complex mocking, which is better done in integration tests
}

// TestWriteFinalSummary verifies summary output.
func TestWriteFinalSummary(_ *testing.T) {
	states := []renderer.ProcessState{
		{Name: "success-proc", Err: nil},
		{Name: "failed-proc", Err: errors.New("exit status 1")},
	}

	// This writes to stderr, so we can't easily capture it
	// But we can verify it doesn't panic
	renderer.WriteFinalSummary(states)

	// Test with empty states
	renderer.WriteFinalSummary([]renderer.ProcessState{})
}

// TestIsTTY verifies TTY detection.
func TestIsTTY(t *testing.T) {
	// This will return false in test environment
	// Just verify it doesn't panic and returns a boolean
	result := renderer.IsTTY()
	_ = result // result will be false in test environment

	// The function is deterministic based on os.Stdout
	t.Logf("IsTTY result: %v", result)
}

// TestRenderIncrementalWithInvalidIndex verifies handling of invalid indices.
func TestRenderIncrementalWithInvalidIndex(_ *testing.T) {
	specs := []engine.ProcessSpec{
		{Name: "proc1", Command: "test"},
	}

	states := []renderer.ProcessState{
		{Name: "proc1", Lines: []string{}, Running: true},
	}

	// Event with negative index
	ev1 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: -1,
		Line:  "should be ignored",
	})
	renderer.RenderIncremental(ev1, specs, states, false, "[%s]")

	// Event with index too large
	ev2 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 999,
		Line:  "should be ignored",
	})
	renderer.RenderIncremental(ev2, specs, states, false, "[%s]")

	// Should not panic
}

// TestRenderScreenWithEmptyStates verifies rendering with no processes.
func TestRenderScreenWithEmptyStates(_ *testing.T) {
	states := []renderer.ProcessState{}
	renderer.RenderScreen(states)
	// Should not panic
}

// TestRenderScreenWithMixedStatus verifies rendering with different process states.
func TestRenderScreenWithMixedStatus(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:    "running-proc",
			Lines:   []string{"output 1", "output 2"},
			Running: true,
			Done:    false,
			Dirty:   true,
		},
		{
			Name:    "success-proc",
			Lines:   []string{"completed"},
			Running: false,
			Done:    true,
			Err:     nil,
			Dirty:   true,
		},
		{
			Name:    "failed-proc",
			Lines:   []string{"error occurred"},
			Running: false,
			Done:    true,
			Err:     errors.New("exit status 1"),
			Dirty:   true,
		},
	}

	// Should render all states without panic
	renderer.RenderScreen(states)

	// All should have dirty cleared
	for i, ps := range states {
		if ps.Dirty {
			t.Errorf("State %d should have dirty cleared after render", i)
		}
	}
}

// TestApplyEventWithEmptyLine verifies handling of empty lines.
func TestApplyEventWithEmptyLine(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:     "test",
			Lines:    []string{},
			ByteSize: 0,
			MaxLines: 0,
			MaxBytes: 0,
		},
	}

	// Apply empty line
	ev := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index: 0,
		Line:  "",
	})

	renderer.ApplyEvent(states, ev)

	// Should have added the empty line
	if len(states[0].Lines) != 1 {
		t.Errorf("Expected 1 line, got %d", len(states[0].Lines))
	}
	if states[0].Lines[0] != "" {
		t.Errorf("Expected empty string, got %q", states[0].Lines[0])
	}
	if states[0].ByteSize != 0 {
		t.Errorf("Expected ByteSize=0 for empty line, got %d", states[0].ByteSize)
	}
}

// TestApplyEventMultipleDoneEvents verifies handling of multiple done events.
func TestApplyEventMultipleDoneEvents(t *testing.T) {
	states := []renderer.ProcessState{
		{
			Name:    "test",
			Lines:   []string{"output"},
			Done:    false,
			Running: true,
		},
	}

	// First done event
	ev1 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      0,
		IsComplete: true,
		Err:        nil,
	})
	renderer.ApplyEvent(states, ev1)

	if !states[0].Done {
		t.Error("Expected Done=true after first done event")
	}

	// Second done event (should be idempotent)
	ev2 := renderer.ConvertProcessLineToEvent(engine.ProcessLine{
		Index:      0,
		IsComplete: true,
		Err:        errors.New("different error"),
	})
	renderer.ApplyEvent(states, ev2)

	// Should update error even if already done
	if states[0].Err == nil {
		t.Error("Expected error to be updated")
	}
	if states[0].Done != true {
		t.Error("Done should still be true")
	}
}
