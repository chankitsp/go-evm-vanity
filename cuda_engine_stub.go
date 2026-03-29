//go:build !windows && !(linux && cgo)

package main

import "fmt"

func newCUDAEngine(options SearchOptions) (Engine, error) {
	return nil, fmt.Errorf("cuda mode is only available on Windows or on Linux with cgo enabled and NVIDIA CUDA libraries installed")
}
