//go:build windows || (linux && cgo)

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/crypto"
)

const cudaBlockSize = 256

type cudaEngine struct {
	driver         *cudaDriver
	context        uintptr
	module         uintptr
	function       uintptr
	dPubKeys       uint64
	dSuffixBytes   uint64
	dFoundIndex    uint64
	batchSize      int
	suffixLenBytes int32
	oddSuffix      int32
	leadingHalf    byte
}

type cudaBatch struct {
	start   secp256k1.ModNScalar
	next    secp256k1.ModNScalar
	pubkeys []byte
}

type cudaFillResult struct {
	batch cudaBatch
	err   error
}

func newCUDAEngine(options SearchOptions) (Engine, error) {
	runtime.LockOSThread()

	driver, err := loadCUDADriver()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	nvrtcAPI, err := loadNVRTC()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	context, major, minor, err := driver.createContext()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	compileOptions := []string{
		fmt.Sprintf("--gpu-architecture=sm_%d%d", major, minor),
		"--std=c++17",
	}
	image, logOutput, err := loadOrCompileCUDAImage(nvrtcAPI, major, minor, compileOptions)
	if err != nil {
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		if logOutput != "" {
			return nil, fmt.Errorf("compile CUDA kernel: %w\n%s", err, logOutput)
		}
		return nil, fmt.Errorf("compile CUDA kernel: %w", err)
	}

	module, err := driver.loadModule(image)
	if err != nil {
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	function, err := driver.getFunction(module, "keccak_match_kernel")
	if err != nil {
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	dPubKeys, err := driver.alloc(uintptr(options.CUDABatchSize * 64))
	if err != nil {
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	suffixBytes := append([]byte(nil), options.Matcher.fullBytes...)
	if len(suffixBytes) == 0 {
		suffixBytes = []byte{0}
	}

	dSuffixBytes, err := driver.alloc(uintptr(len(suffixBytes)))
	if err != nil {
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	if err := driver.memcpyHtoD(dSuffixBytes, suffixBytes); err != nil {
		driver.free(dSuffixBytes)
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	dFoundIndex, err := driver.alloc(4)
	if err != nil {
		driver.free(dSuffixBytes)
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	return &cudaEngine{
		driver:         driver,
		context:        context,
		module:         module,
		function:       function,
		dPubKeys:       dPubKeys,
		dSuffixBytes:   dSuffixBytes,
		dFoundIndex:    dFoundIndex,
		batchSize:      options.CUDABatchSize,
		suffixLenBytes: int32(len(options.Matcher.fullBytes)),
		oddSuffix:      boolToInt32(options.Matcher.oddLength),
		leadingHalf:    options.Matcher.leadingHalf,
	}, nil
}

func (e *cudaEngine) Name() string {
	return "cuda"
}

func (e *cudaEngine) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	var fillWG sync.WaitGroup
	defer func() {
		cancel()
		fillWG.Wait()
		e.close()
	}()

	startTime := time.Now()
	var attempts uint64

	go func() {
		ticker := time.NewTicker(options.ProgressInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				total := atomic.LoadUint64(&attempts)
				elapsed := time.Since(startTime)
				eta := estimateETA(options.ExpectedAttempts, total, elapsed)
				fmt.Printf(
					"attempts=%s rate=%s eta=%s\n",
					formatLargeUint64(total),
					formatRate(total, elapsed),
					eta,
				)
			}
		}
	}()

	seed, err := newWorkerSeed()
	if err != nil {
		return SearchResult{}, err
	}

	buffers := [][]byte{
		make([]byte, e.batchSize*64),
		make([]byte, e.batchSize*64),
	}
	foundRaw := make([]byte, 4)
	currentBatch, err := e.fillBatch(ctx, seed.key, buffers[0], options.Workers)
	if err != nil {
		return SearchResult{}, err
	}
	activeBuffer := 0

	for {
		select {
		case <-ctx.Done():
			return SearchResult{}, ctx.Err()
		default:
		}

		if err := e.beginMatchBatch(currentBatch.pubkeys, foundRaw); err != nil {
			return SearchResult{}, err
		}

		nextBuffer := 1 - activeBuffer
		fillDone := make(chan cudaFillResult, 1)
		fillWG.Add(1)
		go func(start secp256k1.ModNScalar, pubkeys []byte) {
			defer fillWG.Done()
			nextBatch, fillErr := e.fillBatch(ctx, start, pubkeys, options.Workers)
			select {
			case fillDone <- cudaFillResult{batch: nextBatch, err: fillErr}:
			case <-ctx.Done():
			}
		}(currentBatch.next, buffers[nextBuffer])

		matchIndex, err := e.finishMatchBatch(foundRaw)
		if err != nil {
			return SearchResult{}, err
		}

		totalAfterBatch := atomic.AddUint64(&attempts, uint64(e.batchSize))

		if matchIndex >= 0 {
			total := totalAfterBatch - uint64(e.batchSize) + uint64(matchIndex) + 1
			result, ok := confirmCUDAHit(currentBatch.start, currentBatch.pubkeys, matchIndex, options.Matcher, total)
			if ok {
				cancel()
				return result, nil
			}
		}

		fillResult, err := waitForCUDAFill(ctx, fillDone)
		if err != nil {
			return SearchResult{}, err
		}

		currentBatch = fillResult.batch
		activeBuffer = nextBuffer
	}
}

func (e *cudaEngine) fillBatch(ctx context.Context, start secp256k1.ModNScalar, pubkeys []byte, workers int) (cudaBatch, error) {
	next, err := fillPubkeyBatchParallel(ctx, start, pubkeys, e.batchSize, workers)
	if err != nil {
		return cudaBatch{}, err
	}
	return cudaBatch{
		start:   start,
		next:    next,
		pubkeys: pubkeys,
	}, nil
}

func (e *cudaEngine) beginMatchBatch(pubkeys []byte, foundRaw []byte) error {
	if err := e.driver.memcpyHtoD(e.dPubKeys, pubkeys[:e.batchSize*64]); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(foundRaw, math.MaxUint32)
	if err := e.driver.memcpyHtoD(e.dFoundIndex, foundRaw); err != nil {
		return err
	}

	count32 := int32(e.batchSize)
	dPubKeys := e.dPubKeys
	dSuffixBytes := e.dSuffixBytes
	suffixLenBytes := e.suffixLenBytes
	oddSuffix := e.oddSuffix
	leadingHalf := e.leadingHalf
	dFoundIndex := e.dFoundIndex

	params := []unsafe.Pointer{
		unsafe.Pointer(&dPubKeys),
		unsafe.Pointer(&count32),
		unsafe.Pointer(&dSuffixBytes),
		unsafe.Pointer(&suffixLenBytes),
		unsafe.Pointer(&oddSuffix),
		unsafe.Pointer(&leadingHalf),
		unsafe.Pointer(&dFoundIndex),
	}

	gridX := uint32((e.batchSize + cudaBlockSize - 1) / cudaBlockSize)
	if err := e.driver.launch(e.function, gridX, cudaBlockSize, params); err != nil {
		return err
	}
	return nil
}

func (e *cudaEngine) finishMatchBatch(foundRaw []byte) (int, error) {
	if err := e.driver.sync(); err != nil {
		return -1, err
	}
	if err := e.driver.memcpyDtoH(foundRaw, e.dFoundIndex); err != nil {
		return -1, err
	}

	foundIndex := binary.LittleEndian.Uint32(foundRaw)
	if foundIndex == math.MaxUint32 {
		return -1, nil
	}
	return int(foundIndex), nil
}

func confirmCUDAHit(start secp256k1.ModNScalar, pubkeys []byte, matchIndex int, matcher hexSuffixMatcher, attempts uint64) (SearchResult, bool) {
	if matchIndex < 0 {
		return SearchResult{}, false
	}

	pubOffset := matchIndex * 64
	hash := crypto.Keccak256(pubkeys[pubOffset : pubOffset+64])
	if !matcher.Match(hash[12:]) {
		return SearchResult{}, false
	}

	scalar := scalarWithOffset(start, uint32(matchIndex))
	privBytes := scalar.Bytes()
	addressHex := hex.EncodeToString(hash[12:])
	return SearchResult{
		Address:       "0x" + addressHex,
		PrivateKeyHex: hex.EncodeToString(privBytes[:]),
		Attempts:      attempts,
	}, true
}

func waitForCUDAFill(ctx context.Context, fillDone <-chan cudaFillResult) (cudaFillResult, error) {
	select {
	case <-ctx.Done():
		return cudaFillResult{}, ctx.Err()
	case result := <-fillDone:
		if result.err != nil {
			if ctx.Err() != nil {
				return cudaFillResult{}, ctx.Err()
			}
			return cudaFillResult{}, result.err
		}
		return result, nil
	}
}

func (e *cudaEngine) close() {
	if e.driver == nil {
		return
	}
	e.driver.free(e.dFoundIndex)
	e.driver.free(e.dSuffixBytes)
	e.driver.free(e.dPubKeys)
	e.driver.unloadModule(e.module)
	e.driver.destroyContext(e.context)
	runtime.UnlockOSThread()
}

func boolToInt32(value bool) int32 {
	if value {
		return 1
	}
	return 0
}
