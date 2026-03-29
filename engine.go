package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type SearchOptions struct {
	Mode             string
	Suffix           string
	Workers          int
	ProgressInterval time.Duration
	CUDABatchSize    int
	ExpectedAttempts uint64
	Matcher          hexSuffixMatcher
}

type SearchResult struct {
	Address       string
	PrivateKeyHex string
	Attempts      uint64
}

type Engine interface {
	Name() string
	Search(ctx context.Context, options SearchOptions) (SearchResult, error)
}

func NormalizeOptions(options SearchOptions) (SearchOptions, error) {
	options.Mode = strings.ToLower(strings.TrimSpace(options.Mode))
	options.Suffix = normalizeSuffix(options.Suffix)

	switch options.Mode {
	case "auto", "cpu", "cuda":
	default:
		return SearchOptions{}, fmt.Errorf("unsupported mode %q", options.Mode)
	}

	if options.Suffix == "" {
		return SearchOptions{}, errors.New("suffix must not be empty")
	}

	if len(options.Suffix) > 40 {
		return SearchOptions{}, errors.New("suffix must be 40 hex characters or fewer")
	}

	if !isLowerHex(options.Suffix) {
		return SearchOptions{}, errors.New("suffix must contain only hex characters")
	}

	if options.Workers <= 0 {
		return SearchOptions{}, errors.New("workers must be greater than zero")
	}

	if options.ProgressInterval <= 0 {
		return SearchOptions{}, errors.New("progress interval must be greater than zero")
	}

	if options.CUDABatchSize <= 0 {
		return SearchOptions{}, errors.New("cuda-batch must be greater than zero")
	}

	options.ExpectedAttempts = expectedAttemptsForSuffix(len(options.Suffix))
	matcher, err := newHexSuffixMatcher(options.Suffix)
	if err != nil {
		return SearchOptions{}, err
	}
	options.Matcher = matcher
	return options, nil
}

func newEngine(options SearchOptions) (Engine, string, error) {
	switch options.Mode {
	case "cpu":
		return newCPUEngine(), "", nil
	case "cuda":
		engine, err := newCUDAEngine(options)
		return engine, "", err
	case "auto":
		engine, err := newCUDAEngine(options)
		if err == nil {
			return engine, "", nil
		}
		return newCPUEngine(), fmt.Sprintf("CUDA unavailable (%v), falling back to CPU", err), nil
	default:
		return nil, "", fmt.Errorf("unsupported mode %q", options.Mode)
	}
}

func normalizeSuffix(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	return strings.ToLower(value)
}

func isLowerHex(value string) bool {
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		default:
			return false
		}
	}
	return true
}

func expectedAttemptsForSuffix(length int) uint64 {
	if length <= 0 {
		return 1
	}

	if length >= 16 {
		return math.MaxUint64
	}

	return uint64(math.Pow(16, float64(length)))
}

type hexSuffixMatcher struct {
	fullBytes   []byte
	oddLength   bool
	leadingHalf byte
}

func newHexSuffixMatcher(suffix string) (hexSuffixMatcher, error) {
	if suffix == "" {
		return hexSuffixMatcher{}, errors.New("suffix must not be empty")
	}

	if len(suffix)%2 == 0 {
		decoded, err := hex.DecodeString(suffix)
		if err != nil {
			return hexSuffixMatcher{}, fmt.Errorf("decode suffix: %w", err)
		}
		return hexSuffixMatcher{fullBytes: decoded}, nil
	}

	decoded, err := hex.DecodeString(suffix[1:])
	if err != nil {
		return hexSuffixMatcher{}, fmt.Errorf("decode suffix: %w", err)
	}

	return hexSuffixMatcher{
		fullBytes:   decoded,
		oddLength:   true,
		leadingHalf: fromHexNibble(suffix[0]),
	}, nil
}

func (m hexSuffixMatcher) Match(address []byte) bool {
	if !m.oddLength {
		if len(address) < len(m.fullBytes) {
			return false
		}
		offset := len(address) - len(m.fullBytes)
		for i, want := range m.fullBytes {
			if address[offset+i] != want {
				return false
			}
		}
		return true
	}

	bytesNeeded := len(m.fullBytes) + 1
	if len(address) < bytesNeeded {
		return false
	}

	offset := len(address) - bytesNeeded
	if address[offset]&0x0f != m.leadingHalf {
		return false
	}

	for i, want := range m.fullBytes {
		if address[offset+1+i] != want {
			return false
		}
	}
	return true
}

func fromHexNibble(ch byte) byte {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0'
	case ch >= 'a' && ch <= 'f':
		return 10 + ch - 'a'
	default:
		return 0
	}
}
