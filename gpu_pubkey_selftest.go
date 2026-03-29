//go:build windows || (linux && cgo)

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func runGPUPubkeySelfTest(options SearchOptions, count int) error {
	if count <= 0 {
		return fmt.Errorf("gpu-pubkey-selftest must be greater than zero")
	}

	engine, err := newCUDAEngine(options)
	if err != nil {
		return err
	}

	cuda, ok := engine.(*cudaEngine)
	if !ok {
		return fmt.Errorf("CUDA engine does not expose pubkey self-test hooks")
	}
	defer cuda.close()

	scalars := makeDeterministicSelfTestScalars(count)
	expected := make([]byte, count*64)
	for i := range scalars {
		var scalar secp256k1.ModNScalar
		if overflow := scalar.SetBytes(&scalars[i]); overflow != 0 || scalar.IsZero() {
			return fmt.Errorf("self-test scalar %d is invalid", i)
		}
		priv := secp256k1.NewPrivateKey(&scalar)
		copy(expected[i*64:(i+1)*64], priv.PubKey().SerializeUncompressed()[1:])
	}

	start := time.Now()
	pubkeys, statuses, err := cuda.generatePubkeysFromScalars(scalars)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)

	for i := range scalars {
		if statuses[i] != 1 {
			return fmt.Errorf("GPU self-test failed at index %d: status=%d scalar=%x", i, statuses[i], scalars[i])
		}
		offset := i * 64
		if got, want := pubkeys[offset:offset+64], expected[offset:offset+64]; string(got) != string(want) {
			return fmt.Errorf(
				"GPU self-test mismatch at index %d\nscalar: %x\ngpu:    %s\ncpu:    %s",
				i,
				scalars[i],
				hex.EncodeToString(got),
				hex.EncodeToString(want),
			)
		}
	}

	fmt.Printf(
		"GPU pubkey self-test passed count=%d elapsed=%s rate=%s\n",
		count,
		elapsed.Round(time.Millisecond),
		formatRate(uint64(count), elapsed),
	)
	return nil
}

func makeDeterministicSelfTestScalars(count int) [][32]byte {
	scalars := make([][32]byte, count)
	for i := 0; i < count; i++ {
		scalars[i] = deterministicScalarForIndex(i)
	}
	return scalars
}

func deterministicScalarForIndex(index int) [32]byte {
	if index < 32 {
		var scalar secp256k1.ModNScalar
		scalar.SetInt(uint32(index + 1))
		return scalar.Bytes()
	}

	nonce := uint32(index)
	for {
		var seed [8]byte
		binary.LittleEndian.PutUint32(seed[:4], nonce)
		binary.LittleEndian.PutUint32(seed[4:], nonce^0x9e3779b9)
		sum := sha256.Sum256(seed[:])

		var scalar secp256k1.ModNScalar
		if overflow := scalar.SetBytes(&sum); overflow == 0 && !scalar.IsZero() {
			return scalar.Bytes()
		}
		nonce++
	}
}
