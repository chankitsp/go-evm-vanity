package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/crypto"
)

type cpuEngine struct{}

type workerSeed struct {
	key secp256k1.ModNScalar
}

var scalarOne = new(secp256k1.ModNScalar).SetInt(1)

func newCPUEngine() Engine {
	return cpuEngine{}
}

func (cpuEngine) Name() string {
	return "cpu"
}

func (cpuEngine) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	var attempts uint64
	results := make(chan SearchResult, 1)
	errs := make(chan error, 1)

	seeds := make([]workerSeed, options.Workers)
	for i := range seeds {
		seed, err := newWorkerSeed()
		if err != nil {
			return SearchResult{}, err
		}
		seeds[i] = seed
	}

	var wg sync.WaitGroup
	for i := 0; i < options.Workers; i++ {
		wg.Add(1)
		go func(seed workerSeed) {
			defer wg.Done()
			searchCPUWorker(ctx, seed, options.Matcher, &attempts, results)
		}(seeds[i])
	}

	go func() {
		ticker := time.NewTicker(options.ProgressInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				total := atomic.LoadUint64(&attempts)
				elapsed := time.Since(start)
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

	go func() {
		wg.Wait()
		close(errs)
	}()

	select {
	case <-ctx.Done():
		wg.Wait()
		return SearchResult{}, ctx.Err()
	case result := <-results:
		cancel()
		wg.Wait()
		result.Attempts = atomic.LoadUint64(&attempts)
		return result, nil
	case err := <-errs:
		if err == nil {
			return SearchResult{}, fmt.Errorf("search stopped without a result")
		}
		return SearchResult{}, err
	}
}

func newWorkerSeed() (workerSeed, error) {
	for {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return workerSeed{}, fmt.Errorf("generate seed: %w", err)
		}

		var scalar secp256k1.ModNScalar
		if overflow := scalar.SetBytes(&raw); overflow != 0 || scalar.IsZero() {
			continue
		}

		return workerSeed{key: scalar}, nil
	}
}

func searchCPUWorker(ctx context.Context, seed workerSeed, matcher hexSuffixMatcher, attempts *uint64, results chan<- SearchResult) {
	scalar := seed.key
	keccak := crypto.NewKeccakState()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		priv := secp256k1.NewPrivateKey(&scalar)
		pub := priv.PubKey().SerializeUncompressed()
		hash := crypto.HashData(keccak, pub[1:])
		total := atomic.AddUint64(attempts, 1)

		if matcher.Match(hash[12:]) {
			addressHex := hex.EncodeToString(hash[12:])
			result := SearchResult{
				Address:       "0x" + addressHex,
				PrivateKeyHex: hex.EncodeToString(priv.Serialize()),
				Attempts:      total,
			}

			select {
			case results <- result:
			case <-ctx.Done():
			}
			return
		}

		scalar.Add(scalarOne)
		if scalar.IsZero() {
			scalar.Add(scalarOne)
		}
	}
}
