package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a2y-d5l/multiproc/engine"
	"github.com/a2y-d5l/multiproc/runner"
)

func printHelp() {
	fmt.Fprintf(os.Stderr, `multiproc - Concurrent Process Runner

DESCRIPTION:
  A tool for running multiple processes concurrently with real-time output
  rendering and graceful shutdown handling. Supports both interactive TTY
  mode with full-screen rendering and non-TTY mode for CI/log environments.

USAGE:
  multiproc [OPTIONS]

OPTIONS:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
EXAMPLES:
  # Run with default settings (full-screen mode in TTY)
  multiproc

  # Enable timestamps for debugging timing issues
  multiproc -timestamps

  # Use custom log prefix format
  multiproc -prefix="%%s:"

  # Limit output history to 500 lines per process
  multiproc -max-lines=500

  # Increase shutdown timeout for slow processes
  multiproc -shutdown-timeout=10

  # Disable full-screen mode (useful for logging)
  multiproc -fullscreen=false

ENVIRONMENT:
  The process specifications are currently hardcoded in main.go.
  Future versions may support configuration files or command-line arguments.

EXIT CODES:
  0  - All processes completed successfully
  1  - One or more processes failed

For more information, see: https://github.com/a2y-d5l/multiproc
`)
}

func run() int {
	fullScreen := flag.Bool("fullscreen", true, "Enable full-screen terminal rendering (TTY mode only)")
	showSummary := flag.Bool("summary", true, "Show summary of process results after execution")
	showTimestamps := flag.Bool("timestamps", false, "Prefix each output line with an RFC3339 timestamp")
	logPrefix := flag.String("prefix", "[%s]", "Format string for process name prefix (e.g., '[%s]', '%s:')")
	maxLines := flag.Int("max-lines", 1000, "Maximum number of output lines to keep per process")
	shutdownSec := flag.Int("shutdown-timeout", 5, "Seconds to wait for graceful shutdown before force-killing")
	help := flag.Bool("help", false, "Show this help message")

	flag.Parse()

	if *help {
		printHelp()
		os.Exit(0)
	}

	specs := []engine.ProcessSpec{
		{
			Name:    "Subprocess A",
			Command: "sh",
			Args: []string{
				"-c",
				"echo 'A: Starting...'; sleep 1; echo 'A: working...'; sleep 1; echo 'A: Done.'",
			},
		},
		{
			Name:    "Subprocess B",
			Command: "sh",
			Args: []string{
				"-c",
				"echo 'B: Starting...'; sleep 1; echo 'B: working...'; sleep 1; echo 'B: working...'; sleep 1; echo 'B: working...'; sleep 1; echo 'B: Done.'",
			},
		},
		{
			Name:    "Subprocess C",
			Command: "sh",
			Args: []string{
				"-c",
				"echo 'C: Starting...'; sleep 1; echo 'C: working...'; sleep 1; echo 'C: working...'; sleep 1; echo 'C: Done.'",
			},
		},
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		cancel(fmt.Errorf("received signal: %v", sig))
	}()

	cfg := runner.DefaultConfig()
	cfg.Specs = specs
	cfg.FullScreen = *fullScreen
	cfg.ShowSummary = *showSummary
	cfg.ShowTimestamps = *showTimestamps
	cfg.LogPrefix = *logPrefix
	cfg.MaxLinesPerProc = *maxLines
	cfg.ShutdownTimeout = time.Duration(*shutdownSec) * time.Second

	return runner.Run(ctx, cfg)
}

func main() {
	os.Exit(run())
}
