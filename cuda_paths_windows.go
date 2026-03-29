//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cudaToolkitRoot() (string, error) {
	candidates := []string{}
	if value := strings.TrimSpace(os.Getenv("CUDA_PATH")); value != "" {
		candidates = append(candidates, value)
	}
	candidates = append(candidates, `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.2`)

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("CUDA toolkit root not found")
}

func cudaBinDir() (string, error) {
	root, err := cudaToolkitRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}

func cudaBinX64Dir() (string, error) {
	root, err := cudaToolkitRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", "x64"), nil
}

func prepareCUDALibraryPath() error {
	binDir, err := cudaBinDir()
	if err != nil {
		return err
	}
	binX64Dir, err := cudaBinX64Dir()
	if err != nil {
		return err
	}

	current := os.Getenv("PATH")
	parts := []string{binX64Dir, binDir}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(current), strings.ToLower(part)) {
			continue
		}
		current = part + ";" + current
	}
	return os.Setenv("PATH", current)
}
