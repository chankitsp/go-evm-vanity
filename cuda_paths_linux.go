//go:build linux && cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cudaToolkitRoot() (string, error) {
	candidates := []string{}
	for _, envName := range []string{"CUDA_PATH", "CUDA_HOME", "CUDA_ROOT"} {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			candidates = append(candidates, value)
		}
	}
	candidates = append(candidates, "/usr/local/cuda")

	if matches, err := filepath.Glob("/usr/local/cuda-*"); err == nil {
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		candidates = append(candidates, matches...)
	}

	for _, candidate := range uniqueStrings(candidates) {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("CUDA toolkit root not found")
}

func prepareCUDALibraryPath() error {
	return nil
}

func cudaLibraryCandidates(names []string) []string {
	candidates := make([]string, 0, len(names)*4)
	candidates = append(candidates, names...)

	for _, dir := range cudaLibraryDirs() {
		for _, name := range names {
			if strings.ContainsAny(name, "*?[") {
				if matches, err := filepath.Glob(filepath.Join(dir, name)); err == nil {
					sort.Sort(sort.Reverse(sort.StringSlice(matches)))
					candidates = append(candidates, matches...)
				}
				continue
			}
			candidates = append(candidates, filepath.Join(dir, name))
		}
	}

	return uniqueStrings(candidates)
}

func cudaLibraryDirs() []string {
	dirs := []string{}
	if value := strings.TrimSpace(os.Getenv("LD_LIBRARY_PATH")); value != "" {
		dirs = append(dirs, filepath.SplitList(value)...)
	}

	if root, err := cudaToolkitRoot(); err == nil {
		dirs = append(dirs,
			filepath.Join(root, "lib64"),
			filepath.Join(root, "lib"),
			filepath.Join(root, "targets", "x86_64-linux", "lib"),
		)
	}

	if matches, err := filepath.Glob("/usr/local/cuda-*/lib64"); err == nil {
		dirs = append(dirs, matches...)
	}
	if matches, err := filepath.Glob("/usr/local/cuda-*/targets/x86_64-linux/lib"); err == nil {
		dirs = append(dirs, matches...)
	}

	dirs = append(dirs,
		"/usr/local/nvidia/lib64",
		"/usr/local/nvidia/lib",
		"/usr/lib/x86_64-linux-gnu",
		"/usr/lib64",
		"/usr/lib",
		"/lib/x86_64-linux-gnu",
		"/lib64",
		"/lib",
	)

	filtered := make([]string, 0, len(dirs))
	for _, dir := range uniqueStrings(dirs) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			filtered = append(filtered, dir)
		}
	}
	return filtered
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
