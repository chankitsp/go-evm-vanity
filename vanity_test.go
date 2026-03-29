package main

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestNormalizeOptions(t *testing.T) {
	options, err := NormalizeOptions(SearchOptions{
		Mode:             "CPU",
		Suffix:           "0x999999999",
		Workers:          4,
		ProgressInterval: 2 * time.Second,
		CUDABatchSize:    1024,
	})
	if err != nil {
		t.Fatalf("NormalizeOptions returned error: %v", err)
	}

	if options.Mode != "cpu" {
		t.Fatalf("unexpected mode: %s", options.Mode)
	}

	if options.Suffix != "999999999" {
		t.Fatalf("unexpected suffix: %s", options.Suffix)
	}

	if options.ExpectedAttempts != 68_719_476_736 {
		t.Fatalf("unexpected expected attempts: %d", options.ExpectedAttempts)
	}
}

func TestIsLowerHex(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "999999999", want: true},
		{value: "abcdef", want: true},
		{value: "ABCDEF", want: false},
		{value: "xyz", want: false},
	}

	for _, tc := range tests {
		if got := isLowerHex(tc.value); got != tc.want {
			t.Fatalf("isLowerHex(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestHexSuffixMatcher(t *testing.T) {
	even, err := newHexSuffixMatcher("9999")
	if err != nil {
		t.Fatalf("newHexSuffixMatcher returned error: %v", err)
	}
	if !even.Match([]byte{0x12, 0x99, 0x99}) {
		t.Fatal("expected even matcher to match")
	}
	if even.Match([]byte{0x12, 0x99, 0x98}) {
		t.Fatal("expected even matcher not to match")
	}

	odd, err := newHexSuffixMatcher("999")
	if err != nil {
		t.Fatalf("newHexSuffixMatcher returned error: %v", err)
	}
	if !odd.Match([]byte{0xab, 0xc9, 0x99}) {
		t.Fatal("expected odd matcher to match")
	}
	if odd.Match([]byte{0xab, 0xc9, 0x98}) {
		t.Fatal("expected odd matcher not to match")
	}
}

func TestConfirmFullGPUCUDAHitDerivesMatchingPrivateKey(t *testing.T) {
	start := *new(secp256k1.ModNScalar).SetInt(1)
	scalars := make([]byte, 3*32)
	if _, err := fillSequentialScalarRange(context.Background(), start, scalars, 3); err != nil {
		t.Fatalf("fillSequentialScalarRange returned error: %v", err)
	}

	var scalar secp256k1.ModNScalar
	var scalarBytes [32]byte
	copy(scalarBytes[:], scalars[32:64])
	if overflow := scalar.SetBytes(&scalarBytes); overflow != 0 || scalar.IsZero() {
		t.Fatal("expected deterministic scalar to be valid")
	}
	pub := secp256k1.NewPrivateKey(&scalar).PubKey().SerializeUncompressed()
	hash := crypto.Keccak256(pub[1:])
	matcher, err := newHexSuffixMatcher(hex.EncodeToString(hash[30:32]))
	if err != nil {
		t.Fatalf("newHexSuffixMatcher returned error: %v", err)
	}

	result, ok := confirmFullGPUCUDAHit(scalars, 1, matcher, 2)
	if !ok {
		t.Fatal("expected confirmFullGPUCUDAHit to verify the hit")
	}

	expectedScalar := scalarWithOffset(start, 1)
	expectedPriv := expectedScalar.Bytes()
	if result.PrivateKeyHex != hex.EncodeToString(expectedPriv[:]) {
		t.Fatalf("unexpected private key: %s", result.PrivateKeyHex)
	}
	if result.Attempts != 2 {
		t.Fatalf("unexpected attempts: %d", result.Attempts)
	}
}
