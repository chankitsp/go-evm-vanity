//go:build windows || (linux && cgo)

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
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
	pubkeyFunction uintptr
	dScalars       uint64
	dPubKeys       uint64
	dStatuses      uint64
	dSuffixBytes   uint64
	dFoundIndex    uint64
	batchSize      int
	suffixLenBytes int32
	oddSuffix      int32
	leadingHalf    byte
}

type cudaScalarBatch struct {
	start   secp256k1.ModNScalar
	next    secp256k1.ModNScalar
	scalars []byte
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

	pubkeyFunction, err := driver.getFunction(module, "secp256k1_pubkey_kernel")
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

	dScalars, err := driver.alloc(uintptr(options.CUDABatchSize * 32))
	if err != nil {
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	dStatuses, err := driver.alloc(uintptr(options.CUDABatchSize))
	if err != nil {
		driver.free(dScalars)
		driver.free(dPubKeys)
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
		driver.free(dStatuses)
		driver.free(dScalars)
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	if err := driver.memcpyHtoD(dSuffixBytes, suffixBytes); err != nil {
		driver.free(dStatuses)
		driver.free(dScalars)
		driver.free(dSuffixBytes)
		driver.free(dPubKeys)
		driver.unloadModule(module)
		driver.destroyContext(context)
		runtime.UnlockOSThread()
		return nil, err
	}

	dFoundIndex, err := driver.alloc(4)
	if err != nil {
		driver.free(dStatuses)
		driver.free(dScalars)
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
		pubkeyFunction: pubkeyFunction,
		dScalars:       dScalars,
		dPubKeys:       dPubKeys,
		dStatuses:      dStatuses,
		dSuffixBytes:   dSuffixBytes,
		dFoundIndex:    dFoundIndex,
		batchSize:      options.CUDABatchSize,
		suffixLenBytes: int32(len(options.Matcher.fullBytes)),
		oddSuffix:      boolToInt32(options.Matcher.oddLength),
		leadingHalf:    options.Matcher.leadingHalf,
	}, nil
}

func (e *cudaEngine) generatePubkeysFromScalars(scalars [][32]byte) ([]byte, []byte, error) {
	count := len(scalars)
	if count == 0 {
		return nil, nil, nil
	}
	if count > e.batchSize {
		return nil, nil, fmt.Errorf("pubkey self-test count %d exceeds cuda-batch %d", count, e.batchSize)
	}

	scalarBytes := make([]byte, count*32)
	for i := range scalars {
		copy(scalarBytes[i*32:], scalars[i][:])
	}

	pubkeys := make([]byte, count*64)
	statuses := make([]byte, count)

	if err := e.driver.memcpyHtoD(e.dScalars, scalarBytes); err != nil {
		return nil, nil, err
	}
	if err := e.driver.memcpyHtoD(e.dStatuses, statuses); err != nil {
		return nil, nil, err
	}

	gridX := uint32((count + cudaBlockSize - 1) / cudaBlockSize)
	if err := e.driver.launchPubkey(e.pubkeyFunction, gridX, cudaBlockSize, e.dScalars, int32(count), e.dPubKeys, e.dStatuses); err != nil {
		return nil, nil, err
	}
	if err := e.driver.sync(); err != nil {
		return nil, nil, err
	}
	if err := e.driver.memcpyDtoH(pubkeys, e.dPubKeys); err != nil {
		return nil, nil, err
	}
	if err := e.driver.memcpyDtoH(statuses, e.dStatuses); err != nil {
		return nil, nil, err
	}

	return pubkeys, statuses, nil
}

func (e *cudaEngine) Name() string {
	return "cuda"
}

func (e *cudaEngine) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	defer e.close()

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

	scalarBuffers := [][]byte{
		make([]byte, e.batchSize*32),
		make([]byte, e.batchSize*32),
	}
	foundRaw := make([]byte, 4)
	currentBatch, err := fillScalarBatch(ctx, seed.key, scalarBuffers[0], e.batchSize)
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

		if err := e.beginFullGPUBatch(currentBatch.scalars, foundRaw); err != nil {
			return SearchResult{}, err
		}

		nextBuffer := 1 - activeBuffer
		nextBatch, err := fillScalarBatch(ctx, currentBatch.next, scalarBuffers[nextBuffer], e.batchSize)
		if err != nil {
			return SearchResult{}, err
		}

		matchIndex, err := e.finishMatchBatch(foundRaw)
		if err != nil {
			return SearchResult{}, err
		}

		totalAfterBatch := atomic.AddUint64(&attempts, uint64(e.batchSize))

		if matchIndex >= 0 {
			total := totalAfterBatch - uint64(e.batchSize) + uint64(matchIndex) + 1
			result, ok := confirmFullGPUCUDAHit(currentBatch.scalars, matchIndex, options.Matcher, total)
			if ok {
				return result, nil
			}
		}

		currentBatch = nextBatch
		activeBuffer = nextBuffer
	}
}

func (e *cudaEngine) beginFullGPUBatch(scalars []byte, foundRaw []byte) error {
	if err := e.driver.memcpyHtoD(e.dScalars, scalars[:e.batchSize*32]); err != nil {
		return err
	}

	count32 := int32(e.batchSize)
	gridX := uint32((e.batchSize + cudaBlockSize - 1) / cudaBlockSize)
	if err := e.driver.launchPubkey(e.pubkeyFunction, gridX, cudaBlockSize, e.dScalars, count32, e.dPubKeys, e.dStatuses); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(foundRaw, math.MaxUint32)
	if err := e.driver.memcpyHtoD(e.dFoundIndex, foundRaw); err != nil {
		return err
	}

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

	if err := e.driver.launch(e.function, gridX, cudaBlockSize, params); err != nil {
		return err
	}
	return nil
}

func confirmFullGPUCUDAHit(scalars []byte, matchIndex int, matcher hexSuffixMatcher, attempts uint64) (SearchResult, bool) {
	if matchIndex < 0 || len(scalars) < (matchIndex+1)*32 {
		return SearchResult{}, false
	}

	var scalar secp256k1.ModNScalar
	var scalarBytes [32]byte
	copy(scalarBytes[:], scalars[matchIndex*32:(matchIndex+1)*32])
	if overflow := scalar.SetBytes(&scalarBytes); overflow != 0 || scalar.IsZero() {
		return SearchResult{}, false
	}

	priv := secp256k1.NewPrivateKey(&scalar)
	pub := priv.PubKey().SerializeUncompressed()
	hash := crypto.Keccak256(pub[1:])
	if !matcher.Match(hash[12:]) {
		return SearchResult{}, false
	}

	addressHex := hex.EncodeToString(hash[12:])
	return SearchResult{
		Address:       "0x" + addressHex,
		PrivateKeyHex: hex.EncodeToString(priv.Serialize()),
		Attempts:      attempts,
	}, true
}

func fillScalarBatch(ctx context.Context, start secp256k1.ModNScalar, scalars []byte, batchSize int) (cudaScalarBatch, error) {
	next, err := fillSequentialScalarRange(ctx, start, scalars, batchSize)
	if err != nil {
		return cudaScalarBatch{}, err
	}
	return cudaScalarBatch{
		start:   start,
		next:    next,
		scalars: scalars,
	}, nil
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

func (e *cudaEngine) close() {
	if e.driver == nil {
		return
	}
	e.driver.free(e.dFoundIndex)
	e.driver.free(e.dSuffixBytes)
	e.driver.free(e.dStatuses)
	e.driver.free(e.dPubKeys)
	e.driver.free(e.dScalars)
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
