//go:build !windows && !(linux && cgo)

package main

import "fmt"

func runGPUPubkeySelfTest(options SearchOptions, count int) error {
	return fmt.Errorf("GPU pubkey self-test requires Windows or Linux with cgo-enabled CUDA support")
}
