package main

import (
	"fmt"
	"time"
)

func formatLargeUint64(value uint64) string {
	switch {
	case value >= 1_000_000_000_000:
		return fmt.Sprintf("%.2fT", float64(value)/1_000_000_000_000)
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.2fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatRate(attempts uint64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "0 addr/s"
	}

	return fmt.Sprintf("%.0f addr/s", float64(attempts)/elapsed.Seconds())
}

func estimateETA(target, attempts uint64, elapsed time.Duration) string {
	if target == 0 || attempts == 0 || elapsed <= 0 {
		return "unknown"
	}

	rate := float64(attempts) / elapsed.Seconds()
	if rate <= 0 {
		return "unknown"
	}

	if attempts >= target {
		return "soon"
	}

	remaining := float64(target - attempts)
	return time.Duration(remaining / rate * float64(time.Second)).Round(time.Second).String()
}
