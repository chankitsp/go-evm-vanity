# CUDA notes

This repository currently uses a correctness-first hybrid `-mode=cuda` / `-mode=auto` path on both Windows and Linux.

## Current design

The CUDA path splits work across CPU and GPU:

- CPU generates sequential `secp256k1` pubkeys in batches
- GPU runs `Keccak-256 + suffix match` across that batch
- Go verifies any reported hit on the CPU before printing the address/private key

This keeps `cuda` mode aligned with `cpu` mode while still offloading the hashing pass to the GPU.

## Hybrid optimizations

The current hybrid implementation includes a few throughput-focused changes:

- pubkey-only batches on the host, instead of storing every private key
- double-buffered host memory so CPU can prepare the next batch while the GPU hashes the current one
- cached NVRTC output under `cuda/cache/` so later runs start faster

Private keys are derived only for the winning index after a confirmed hit.

## Performance notes

Actual speed depends on the GPU, CPU, suffix length, and `-cuda-batch`.

Useful batch sizes to try on this machine were:

```text
65536
131072
262144
```

Short suffixes are highly luck-driven, so compare `rate=... addr/s` over longer runs instead of judging by which mode finds a hit first.

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

So this target is still expensive and may need long runtimes even after hybrid-path tuning.
