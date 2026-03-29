package main

const vanityKeccakKernelSource = `
extern "C" __device__ __forceinline__ unsigned long long rotl64(unsigned long long x, int shift) {
    return (x << shift) | (x >> (64 - shift));
}

extern "C" __device__ __forceinline__ unsigned long long load64_le(const unsigned char* src) {
    unsigned long long value = 0;
    #pragma unroll
    for (int i = 0; i < 8; ++i) {
        value |= ((unsigned long long)src[i]) << (8 * i);
    }
    return value;
}

__device__ __constant__ unsigned long long keccakf_rndc[24] = {
    0x0000000000000001ULL, 0x0000000000008082ULL,
    0x800000000000808aULL, 0x8000000080008000ULL,
    0x000000000000808bULL, 0x0000000080000001ULL,
    0x8000000080008081ULL, 0x8000000000008009ULL,
    0x000000000000008aULL, 0x0000000000000088ULL,
    0x0000000080008009ULL, 0x000000008000000aULL,
    0x000000008000808bULL, 0x800000000000008bULL,
    0x8000000000008089ULL, 0x8000000000008003ULL,
    0x8000000000008002ULL, 0x8000000000000080ULL,
    0x000000000000800aULL, 0x800000008000000aULL,
    0x8000000080008081ULL, 0x8000000000008080ULL,
    0x0000000080000001ULL, 0x8000000080008008ULL
};

__device__ __constant__ int keccakf_rotc[24] = {
    1, 3, 6, 10, 15, 21, 28, 36,
    45, 55, 2, 14, 27, 41, 56, 8,
    25, 43, 62, 18, 39, 61, 20, 44
};

__device__ __constant__ int keccakf_piln[24] = {
    10, 7, 11, 17, 18, 3, 5, 16,
    8, 21, 24, 4, 15, 23, 19, 13,
    12, 2, 20, 14, 22, 9, 6, 1
};

extern "C" __device__ void keccakf(unsigned long long state[25]) {
    unsigned long long bc[5];
    unsigned long long t;

    #pragma unroll
    for (int round = 0; round < 24; ++round) {
        #pragma unroll
        for (int i = 0; i < 5; ++i) {
            bc[i] = state[i] ^ state[i + 5] ^ state[i + 10] ^ state[i + 15] ^ state[i + 20];
        }

        #pragma unroll
        for (int i = 0; i < 5; ++i) {
            t = bc[(i + 4) % 5] ^ rotl64(bc[(i + 1) % 5], 1);
            #pragma unroll
            for (int j = 0; j < 25; j += 5) {
                state[j + i] ^= t;
            }
        }

        t = state[1];
        #pragma unroll
        for (int i = 0; i < 24; ++i) {
            int j = keccakf_piln[i];
            bc[0] = state[j];
            state[j] = rotl64(t, keccakf_rotc[i]);
            t = bc[0];
        }

        for (int j = 0; j < 25; j += 5) {
            #pragma unroll
            for (int i = 0; i < 5; ++i) {
                bc[i] = state[j + i];
            }
            #pragma unroll
            for (int i = 0; i < 5; ++i) {
                state[j + i] ^= (~bc[(i + 1) % 5]) & bc[(i + 2) % 5];
            }
        }

        state[0] ^= keccakf_rndc[round];
    }
}

extern "C" __device__ __forceinline__ int suffix_match(
    const unsigned char* address,
    const unsigned char* suffixBytes,
    int suffixLenBytes,
    int oddSuffix,
    unsigned char leadingHalf
) {
    if (oddSuffix == 0) {
        int offset = 20 - suffixLenBytes;
        for (int i = 0; i < suffixLenBytes; ++i) {
            if (address[offset + i] != suffixBytes[i]) {
                return 0;
            }
        }
        return 1;
    }

    int offset = 20 - (suffixLenBytes + 1);
    if ((address[offset] & 0x0f) != leadingHalf) {
        return 0;
    }

    for (int i = 0; i < suffixLenBytes; ++i) {
        if (address[offset + 1 + i] != suffixBytes[i]) {
            return 0;
        }
    }
    return 1;
}

extern "C" __global__ void keccak_match_kernel(
    const unsigned char* pubkeys,
    int count,
    const unsigned char* suffixBytes,
    int suffixLenBytes,
    int oddSuffix,
    unsigned char leadingHalf,
    unsigned int* foundIndex
) {
    int idx = blockIdx.x * blockDim.x + threadIdx.x;
    if (idx >= count) {
        return;
    }

    const unsigned char* pubkey = pubkeys + ((unsigned long long)idx * 64ULL);
    unsigned long long state[25] = {0};

    #pragma unroll
    for (int lane = 0; lane < 8; ++lane) {
        state[lane] = load64_le(pubkey + lane * 8);
    }

    state[8] ^= 0x01ULL;
    state[16] ^= 0x8000000000000000ULL;
    keccakf(state);

    unsigned char digest[32];
    #pragma unroll
    for (int lane = 0; lane < 4; ++lane) {
        unsigned long long word = state[lane];
        #pragma unroll
        for (int b = 0; b < 8; ++b) {
            digest[lane * 8 + b] = (unsigned char)((word >> (8 * b)) & 0xffULL);
        }
    }

    if (suffix_match(digest + 12, suffixBytes, suffixLenBytes, oddSuffix, leadingHalf)) {
        atomicMin(foundIndex, (unsigned int)idx);
    }
}
`
