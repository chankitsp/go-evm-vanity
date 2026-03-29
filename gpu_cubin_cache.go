//go:build windows || (linux && cgo)

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loadOrCompileCUDAImage(nvrtcAPI *nvrtcAPI, major, minor int, options []string) ([]byte, string, error) {
	cachePath, err := cudaImageCachePath(major, minor, options)
	if err != nil {
		return nil, "", err
	}

	if image, err := os.ReadFile(cachePath); err == nil && len(image) > 0 {
		return image, "", nil
	}

	image, logOutput, err := nvrtcAPI.compileCUDA(vanityKeccakKernelSource, "full_vanity.cu", options)
	if err != nil {
		return nil, logOutput, err
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		_ = os.WriteFile(cachePath, image, 0o644)
	}

	return image, logOutput, nil
}

func cudaImageCachePath(major, minor int, options []string) (string, error) {
	signature := strings.Join(options, "|") + "|" + vanityKeccakKernelSource
	digest := sha1.Sum([]byte(signature))
	fileName := fmt.Sprintf("full_vanity_sm%d%d_%s.cubin", major, minor, hex.EncodeToString(digest[:8]))
	return filepath.Join("cuda", "cache", fileName), nil
}
