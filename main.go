package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	options, err := parseCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	engine, fallbackMessage, err := newEngine(options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine error: %v\n", err)
		os.Exit(1)
	}

	if fallbackMessage != "" {
		fmt.Printf("info: %s\n", fallbackMessage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf(
		"mode=%s suffix=%s workers=%d expected=~%s attempts progress=%s\n",
		engine.Name(),
		options.Suffix,
		options.Workers,
		formatLargeUint64(options.ExpectedAttempts),
		options.ProgressInterval,
	)

	start := time.Now()
	result, err := engine.Search(ctx, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	fmt.Println("FOUND!")
	fmt.Printf("Address:      %s\n", result.Address)
	fmt.Printf("Private Key:  %s\n", result.PrivateKeyHex)
	fmt.Printf("Attempts:     %s\n", formatLargeUint64(result.Attempts))
	fmt.Printf("Elapsed:      %s\n", elapsed.Round(time.Millisecond))
	if elapsed > 0 {
		fmt.Printf("Average Rate: %s\n", formatRate(result.Attempts, elapsed))
	}
}

func parseCLI() (SearchOptions, error) {
	var mode string
	var suffix string
	var workers int
	var progress time.Duration
	var batchSize int

	flag.StringVar(&mode, "mode", "auto", "search mode: auto, cpu, cuda")
	flag.StringVar(&suffix, "suffix", "999999999", "hex suffix to match against the 40-hex-character EVM address")
	flag.IntVar(&workers, "workers", runtime.NumCPU(), "number of CPU workers")
	flag.DurationVar(&progress, "progress", 2*time.Second, "progress print interval")
	flag.IntVar(&batchSize, "cuda-batch", 1<<16, "GPU batch size for full CUDA vanity search")
	flag.Parse()

	return NormalizeOptions(SearchOptions{
		Mode:             mode,
		Suffix:           suffix,
		Workers:          workers,
		ProgressInterval: progress,
		CUDABatchSize:    batchSize,
	})
}
