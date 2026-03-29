# go-evm-vanity

Fast EVM vanity address search in Go with CPU and NVIDIA CUDA modes.

This tool brute-forces private keys until the derived EVM address ends with a requested hexadecimal suffix such as `999999999`, `dead`, or `c0ffee`.

## Features

- Searches for hexadecimal address suffixes up to 40 hex characters.
- Supports `cpu`, `cuda`, and `auto` execution modes.
- Falls back from CUDA to CPU automatically in `auto` mode.
- Prints live progress with attempts, rate, and ETA.
- Can run a deterministic GPU pubkey self-test before searching.
- Supports odd-length suffixes like `999` as well as even-length suffixes like `9999`.

## Requirements

- Go `1.25+`
- For CPU mode: no extra runtime dependencies
- For CUDA mode:
  - NVIDIA GPU with CUDA driver installed
  - CUDA toolkit available on the machine
  - Windows: the program looks for `CUDA_PATH` first, then `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.2`
  - Linux: build with `CGO_ENABLED=1` and make CUDA libraries available through the standard loader paths or `LD_LIBRARY_PATH`

## Build

### Windows

```powershell
go build -o vanity.exe .
```

### Linux

CPU-only build:

```bash
CGO_ENABLED=0 go build -o vanity .
```

CUDA-capable build:

```bash
CGO_ENABLED=1 go build -o vanity .
```

## Quick Start

Run with automatic engine selection:

```bash
go run . -suffix 999999999
```

Force CPU mode:

```bash
go run . -mode cpu -suffix deadbeef -workers 32
```

Force CUDA mode:

```bash
go run . -mode cuda -suffix c0ffee -cuda-batch 131072
```

If CUDA is unavailable and `-mode auto` is used, the program prints an info message and continues on CPU.

## CLI Flags

```text
-cuda-batch int
    GPU batch size for full CUDA vanity search (default 65536)
-gpu-pubkey-selftest int
    validate GPU secp256k1 pubkey generation against CPU for N deterministic scalars and exit
-mode string
    search mode: auto, cpu, cuda (default "auto")
-progress duration
    progress print interval (default 2s)
-suffix string
    hex suffix to match against the 40-hex-character EVM address (default "999999999")
-workers int
    number of CPU workers (default: runtime.NumCPU on the current machine)
```

## Matching Rules

- The search target is the final hex characters of the 20-byte EVM address.
- `0x` prefixes are accepted and removed automatically.
- Hex letters are normalized to lowercase automatically.
- The suffix must contain only hex characters and must be at most 40 characters long.
- Expected work grows exponentially with suffix length: approximately `16^n` attempts for `n` hex characters.

Examples:

- `999`
- `0x999999999`
- `DEADBEEF`

## Example Output

```text
mode=cpu suffix=999999999 workers=16 expected=~68.72B attempts progress=2s
attempts=1.20M rate=802331 addr/s eta=23h46m12s
attempts=2.01M rate=811204 addr/s eta=23h29m41s
FOUND!
Address:      0x0123456789abcdef0123456789abcde999999999
Private Key:  0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
Attempts:     68.72B
Elapsed:      23h31m2s
Average Rate: 811204 addr/s
```

## GPU Self-Test

To verify that GPU secp256k1 public key generation matches the CPU implementation:

```bash
go run . -mode cuda -gpu-pubkey-selftest 4096
```

This runs the self-test and exits without starting a vanity search.

## Testing

```bash
go test ./...
```

## Security Notes

- A successful match prints the private key to standard output.
- Treat the terminal output, logs, shell history, and screenshots as sensitive.
- Do not use a found key for real funds unless you fully trust the machine and workflow used to generate it.
