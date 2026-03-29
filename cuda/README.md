# CUDA notes

This repository now uses a correctness-first full-GPU pubkey path for `-mode=cuda` / `-mode=auto` on both Windows and Linux.

## Current design

The CUDA path now does the heavy elliptic-curve and hashing work on the GPU:

- CPU generates sequential scalar batches
- GPU derives `secp256k1` pubkeys from those scalars
- GPU runs `Keccak-256 + suffix match`
- Go verifies any reported hit on the CPU before printing the address/private key

This keeps the final result aligned with `cpu` mode while moving the expensive pubkey generation onto the GPU.

## Full-GPU notes

The current full-GPU implementation focuses on correctness first:

- GPU `secp256k1` pubkey generation is validated against CPU-derived pubkeys
- GPU Keccak matching still runs in its own kernel after pubkey generation
- private keys are derived and confirmed on the CPU only for the winning index
- cached NVRTC output under `cuda/cache/` makes later runs start faster

## GPU pubkey validation

There is now a correctness-first GPU `secp256k1` pubkey self-test path that can
be compared directly against CPU-derived pubkeys before enabling a full-GPU
search path.

Examples:

```text
.\vanity.exe -gpu-pubkey-selftest 256 -mode cuda -suffix 99
./vanity -gpu-pubkey-selftest 256 -mode cuda -suffix 99
```

## Performance notes

Actual speed depends on the GPU, CPU, suffix length, and `-cuda-batch`.

Useful batch sizes to try on this machine were:

```text
65536
131072
262144
```

Short suffixes are highly luck-driven, so compare `rate=... addr/s` over longer runs instead of judging by which mode finds a hit first.

On the Windows RTX 3080 test machine, recent short-run full-GPU checks were around:

```text
CUDA full-GPU search: ~0.5M addr/s
```

## First run vs later runs

The first CUDA run after changing the kernel is slower to start because the app
compiles the CUDA source with NVRTC and stores a cached `.cubin` under:

```text
cuda/cache/
```

Later runs reuse that cached image and start much faster.

## Platform notes

Windows:

- toolkit discovery prefers `CUDA_PATH`
- the current loader looks for `nvcuda.dll`
- NVRTC is loaded from the CUDA Toolkit, for example `nvrtc64_130_0.dll`

Linux:

- build with `cgo` enabled
- the current loader looks for `libcuda.so.1` / `libcuda.so`
- NVRTC is loaded from `libnvrtc.so*`
- common search roots include `CUDA_PATH`, `CUDA_HOME`, `/usr/local/cuda`, `/usr/local/cuda-*`, and standard system library directories

## Why NVRTC is used

The project compiles the CUDA kernel at runtime with NVRTC and loads the
resulting image through the CUDA Driver API. This avoids depending on `nvcc`
for the normal run path.

## Important note about `999999999`

A 9-hex suffix has an average search cost of:

```text
16^9 = 68,719,476,736 attempts
```

So this target is still expensive and may need long runtimes even after full-GPU tuning.
