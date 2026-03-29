package main

const vanityKeccakKernelSource = `
#define FIELD_LIMBS 8

typedef struct {
    unsigned int v[FIELD_LIMBS];
} fe_t;

typedef struct {
    fe_t x;
    fe_t y;
    fe_t z;
    int infinity;
} jacobian_t;

typedef struct {
    fe_t x;
    fe_t y;
    int infinity;
} affine_t;

__device__ __constant__ unsigned int secp_field_p[FIELD_LIMBS] = {
    0xFFFFFC2FU, 0xFFFFFFFEU, 0xFFFFFFFFU, 0xFFFFFFFFU,
    0xFFFFFFFFU, 0xFFFFFFFFU, 0xFFFFFFFFU, 0xFFFFFFFFU
};

__device__ __constant__ unsigned int secp_generator_x[FIELD_LIMBS] = {
    0x16F81798U, 0x59F2815BU, 0x2DCE28D9U, 0x029BFCDBU,
    0xCE870B07U, 0x55A06295U, 0xF9DCBBACU, 0x79BE667EU
};

__device__ __constant__ unsigned int secp_generator_y[FIELD_LIMBS] = {
    0xFB10D4B8U, 0x9C47D08FU, 0xA6855419U, 0xFD17B448U,
    0x0E1108A8U, 0x5DA4FBFCU, 0x26A3C465U, 0x483ADA77U
};

__device__ __constant__ unsigned int secp_field_p_minus_two_be[FIELD_LIMBS] = {
    0xFFFFFFFFU, 0xFFFFFFFFU, 0xFFFFFFFFU, 0xFFFFFFFFU,
    0xFFFFFFFFU, 0xFFFFFFFFU, 0xFFFFFFFEU, 0xFFFFFC2DU
};

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

__device__ __forceinline__ void fe_clear(fe_t* value) {
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        value->v[i] = 0;
    }
}

__device__ __forceinline__ void fe_copy(fe_t* dst, const fe_t* src) {
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        dst->v[i] = src->v[i];
    }
}

__device__ __forceinline__ void fe_set_one(fe_t* value) {
    fe_clear(value);
    value->v[0] = 1;
}

__device__ __forceinline__ int fe_is_zero(const fe_t* value) {
    unsigned int orValue = 0;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        orValue |= value->v[i];
    }
    return orValue == 0;
}

__device__ __forceinline__ int fe_cmp_words(const unsigned int* a, const unsigned int* b) {
    for (int i = FIELD_LIMBS - 1; i >= 0; --i) {
        if (a[i] > b[i]) {
            return 1;
        }
        if (a[i] < b[i]) {
            return -1;
        }
    }
    return 0;
}

__device__ __forceinline__ int fe_equal(const fe_t* a, const fe_t* b) {
    unsigned int diff = 0;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        diff |= (a->v[i] ^ b->v[i]);
    }
    return diff == 0;
}

__device__ __forceinline__ unsigned int fe_add_raw(const fe_t* a, const fe_t* b, fe_t* out) {
    unsigned long long carry = 0;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        unsigned long long sum = (unsigned long long)a->v[i] + b->v[i] + carry;
        out->v[i] = (unsigned int)sum;
        carry = sum >> 32;
    }
    return (unsigned int)carry;
}

__device__ __forceinline__ unsigned int fe_sub_raw(const fe_t* a, const fe_t* b, fe_t* out) {
    unsigned long long borrow = 0;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        unsigned long long subtrahend = (unsigned long long)b->v[i] + borrow;
        unsigned long long ai = (unsigned long long)a->v[i];
        if (ai >= subtrahend) {
            out->v[i] = (unsigned int)(ai - subtrahend);
            borrow = 0;
        } else {
            out->v[i] = (unsigned int)((1ULL << 32) + ai - subtrahend);
            borrow = 1;
        }
    }
    return (unsigned int)borrow;
}

__device__ __forceinline__ void fe_sub_prime(fe_t* value) {
    fe_t prime;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        prime.v[i] = secp_field_p[i];
    }
    fe_t tmp;
    (void)fe_sub_raw(value, &prime, &tmp);
    fe_copy(value, &tmp);
}

__device__ __forceinline__ void fe_add_prime(fe_t* value) {
    fe_t prime;
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        prime.v[i] = secp_field_p[i];
    }
    fe_t tmp;
    (void)fe_add_raw(value, &prime, &tmp);
    fe_copy(value, &tmp);
}

__device__ __forceinline__ void fe_normalize(fe_t* value) {
    while (fe_cmp_words(value->v, secp_field_p) >= 0) {
        fe_sub_prime(value);
    }
}

__device__ __forceinline__ void fe_add(const fe_t* a, const fe_t* b, fe_t* out) {
    unsigned int carry = fe_add_raw(a, b, out);
    if (carry != 0 || fe_cmp_words(out->v, secp_field_p) >= 0) {
        fe_sub_prime(out);
    }
}

__device__ __forceinline__ void fe_sub(const fe_t* a, const fe_t* b, fe_t* out) {
    unsigned int borrow = fe_sub_raw(a, b, out);
    if (borrow != 0) {
        fe_add_prime(out);
    }
}

__device__ __forceinline__ void acc_add_u32(unsigned int* acc, int index, unsigned int value) {
    unsigned long long sum = (unsigned long long)acc[index] + value;
    acc[index] = (unsigned int)sum;
    unsigned long long carry = sum >> 32;
    index++;
    while (carry != 0) {
        sum = (unsigned long long)acc[index] + carry;
        acc[index] = (unsigned int)sum;
        carry = sum >> 32;
        index++;
    }
}

__device__ __forceinline__ void acc_add_mul_u32(unsigned int* acc, int index, unsigned int value, unsigned int mul) {
    unsigned long long product = (unsigned long long)value * mul;
    acc_add_u32(acc, index, (unsigned int)product);
    acc_add_u32(acc, index + 1, (unsigned int)(product >> 32));
}

__device__ __forceinline__ void fe_reduce_product(const unsigned int product[16], fe_t* out) {
    unsigned int acc[11] = {0};

    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        acc[i] = product[i];
    }

    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        acc_add_mul_u32(acc, i, product[FIELD_LIMBS + i], 977U);
        acc_add_u32(acc, i + 1, product[FIELD_LIMBS + i]);
    }

    while (acc[8] != 0 || acc[9] != 0 || acc[10] != 0) {
        unsigned int overflow8 = acc[8];
        unsigned int overflow9 = acc[9];
        unsigned int overflow10 = acc[10];
        acc[8] = 0;
        acc[9] = 0;
        acc[10] = 0;

        if (overflow8 != 0) {
            acc_add_mul_u32(acc, 0, overflow8, 977U);
            acc_add_u32(acc, 1, overflow8);
        }
        if (overflow9 != 0) {
            acc_add_mul_u32(acc, 1, overflow9, 977U);
            acc_add_u32(acc, 2, overflow9);
        }
        if (overflow10 != 0) {
            acc_add_mul_u32(acc, 2, overflow10, 977U);
            acc_add_u32(acc, 3, overflow10);
        }
    }

    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        out->v[i] = acc[i];
    }
    fe_normalize(out);
}

__device__ __forceinline__ void fe_mul(const fe_t* a, const fe_t* b, fe_t* out) {
    unsigned int product[16] = {0};

    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        unsigned long long carry = 0;
        #pragma unroll
        for (int j = 0; j < FIELD_LIMBS; ++j) {
            unsigned long long uv = (unsigned long long)product[i + j] +
                (unsigned long long)a->v[i] * b->v[j] + carry;
            product[i + j] = (unsigned int)uv;
            carry = uv >> 32;
        }

        int k = i + FIELD_LIMBS;
        while (carry != 0) {
            unsigned long long uv = (unsigned long long)product[k] + carry;
            product[k] = (unsigned int)uv;
            carry = uv >> 32;
            k++;
        }
    }

    fe_reduce_product(product, out);
}

__device__ __forceinline__ void fe_square(const fe_t* a, fe_t* out) {
    fe_mul(a, a, out);
}

__device__ void fe_inverse(const fe_t* a, fe_t* out) {
    fe_t result;
    fe_t base;
    fe_set_one(&result);
    fe_copy(&base, a);

    #pragma unroll
    for (int wordIndex = 0; wordIndex < FIELD_LIMBS; ++wordIndex) {
        unsigned int word = secp_field_p_minus_two_be[wordIndex];
        for (int bit = 31; bit >= 0; --bit) {
            fe_square(&result, &result);
            if (((word >> bit) & 1U) != 0) {
                fe_mul(&result, &base, &result);
            }
        }
    }

    fe_copy(out, &result);
}

__device__ __forceinline__ void fe_from_constant(fe_t* out, const unsigned int* src) {
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        out->v[i] = src[i];
    }
}

__device__ __forceinline__ void fe_to_big_endian(const fe_t* value, unsigned char* out) {
    #pragma unroll
    for (int i = 0; i < FIELD_LIMBS; ++i) {
        unsigned int limb = value->v[FIELD_LIMBS - 1 - i];
        out[i * 4 + 0] = (unsigned char)((limb >> 24) & 0xffU);
        out[i * 4 + 1] = (unsigned char)((limb >> 16) & 0xffU);
        out[i * 4 + 2] = (unsigned char)((limb >> 8) & 0xffU);
        out[i * 4 + 3] = (unsigned char)(limb & 0xffU);
    }
}

__device__ __forceinline__ void jacobian_set_infinity(jacobian_t* point) {
    fe_clear(&point->x);
    fe_clear(&point->y);
    fe_clear(&point->z);
    point->infinity = 1;
}

__device__ __forceinline__ int jacobian_is_infinity(const jacobian_t* point) {
    return point->infinity != 0 || fe_is_zero(&point->z);
}

__device__ __forceinline__ void jacobian_set_generator(jacobian_t* point) {
    fe_from_constant(&point->x, secp_generator_x);
    fe_from_constant(&point->y, secp_generator_y);
    fe_set_one(&point->z);
    point->infinity = 0;
}

__device__ void jacobian_double(const jacobian_t* point, jacobian_t* out) {
    if (jacobian_is_infinity(point) || fe_is_zero(&point->y)) {
        jacobian_set_infinity(out);
        return;
    }

    fe_t A;
    fe_t B;
    fe_t C;
    fe_t D;
    fe_t E;
    fe_t F;
    fe_t tmp1;
    fe_t tmp2;
    fe_t tmp3;

    fe_square(&point->x, &A);
    fe_square(&point->y, &B);
    fe_square(&B, &C);

    fe_add(&point->x, &B, &tmp1);
    fe_square(&tmp1, &tmp2);
    fe_sub(&tmp2, &A, &tmp2);
    fe_sub(&tmp2, &C, &tmp2);
    fe_add(&tmp2, &tmp2, &D);

    fe_add(&A, &A, &tmp1);
    fe_add(&tmp1, &A, &E);
    fe_square(&E, &F);

    fe_add(&D, &D, &tmp1);
    fe_sub(&F, &tmp1, &out->x);

    fe_sub(&D, &out->x, &tmp1);
    fe_mul(&E, &tmp1, &tmp2);

    fe_add(&C, &C, &tmp3);
    fe_add(&tmp3, &tmp3, &tmp3);
    fe_add(&tmp3, &tmp3, &tmp3);
    fe_sub(&tmp2, &tmp3, &out->y);

    fe_mul(&point->y, &point->z, &tmp1);
    fe_add(&tmp1, &tmp1, &out->z);
    out->infinity = 0;
}

__device__ void jacobian_add_generator(const jacobian_t* point, jacobian_t* out) {
    if (jacobian_is_infinity(point)) {
        jacobian_set_generator(out);
        return;
    }

    fe_t gx;
    fe_t gy;
    fe_t z1z1;
    fe_t U2;
    fe_t S2;
    fe_t H;
    fe_t HH;
    fe_t I;
    fe_t J;
    fe_t r;
    fe_t V;
    fe_t tmp1;
    fe_t tmp2;

    fe_from_constant(&gx, secp_generator_x);
    fe_from_constant(&gy, secp_generator_y);

    fe_square(&point->z, &z1z1);
    fe_mul(&gx, &z1z1, &U2);

    fe_mul(&point->z, &z1z1, &tmp1);
    fe_mul(&gy, &tmp1, &S2);

    if (fe_equal(&U2, &point->x)) {
        if (fe_equal(&S2, &point->y)) {
            jacobian_double(point, out);
        } else {
            jacobian_set_infinity(out);
        }
        return;
    }

    fe_sub(&U2, &point->x, &H);
    fe_square(&H, &HH);
    fe_add(&HH, &HH, &I);
    fe_add(&I, &I, &I);
    fe_mul(&H, &I, &J);

    fe_sub(&S2, &point->y, &tmp1);
    fe_add(&tmp1, &tmp1, &r);

    fe_mul(&point->x, &I, &V);

    fe_square(&r, &out->x);
    fe_sub(&out->x, &J, &out->x);
    fe_add(&V, &V, &tmp1);
    fe_sub(&out->x, &tmp1, &out->x);

    fe_sub(&V, &out->x, &tmp1);
    fe_mul(&r, &tmp1, &out->y);
    fe_mul(&point->y, &J, &tmp2);
    fe_add(&tmp2, &tmp2, &tmp2);
    fe_sub(&out->y, &tmp2, &out->y);

    fe_add(&point->z, &H, &out->z);
    fe_square(&out->z, &out->z);
    fe_sub(&out->z, &z1z1, &out->z);
    fe_sub(&out->z, &HH, &out->z);
    out->infinity = 0;
}

__device__ void jacobian_to_affine(const jacobian_t* point, affine_t* out) {
    if (jacobian_is_infinity(point)) {
        fe_clear(&out->x);
        fe_clear(&out->y);
        out->infinity = 1;
        return;
    }

    fe_t zInv;
    fe_t zInv2;
    fe_t zInv3;

    fe_inverse(&point->z, &zInv);
    fe_square(&zInv, &zInv2);
    fe_mul(&zInv2, &zInv, &zInv3);

    fe_mul(&point->x, &zInv2, &out->x);
    fe_mul(&point->y, &zInv3, &out->y);
    out->infinity = 0;
}

__device__ int scalar_mul_generator_to_affine(const unsigned char* scalar, affine_t* out) {
    unsigned int anyBit = 0;
    jacobian_t acc;
    jacobian_set_infinity(&acc);

    #pragma unroll
    for (int byteIndex = 0; byteIndex < 32; ++byteIndex) {
        unsigned char value = scalar[byteIndex];
        anyBit |= value;
        for (int bit = 7; bit >= 0; --bit) {
            jacobian_t doubled;
            jacobian_double(&acc, &doubled);
            acc = doubled;
            if (((value >> bit) & 1U) != 0) {
                jacobian_t added;
                jacobian_add_generator(&acc, &added);
                acc = added;
            }
        }
    }

    if (anyBit == 0 || jacobian_is_infinity(&acc)) {
        fe_clear(&out->x);
        fe_clear(&out->y);
        out->infinity = 1;
        return 0;
    }

    jacobian_to_affine(&acc, out);
    return out->infinity == 0;
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

extern "C" __global__ void secp256k1_pubkey_kernel(
    const unsigned char* scalars,
    int count,
    unsigned char* pubkeys,
    unsigned char* statuses
) {
    int idx = blockIdx.x * blockDim.x + threadIdx.x;
    if (idx >= count) {
        return;
    }

    const unsigned char* scalar = scalars + ((unsigned long long)idx * 32ULL);
    unsigned char* out = pubkeys + ((unsigned long long)idx * 64ULL);
    affine_t pubkey;

    if (!scalar_mul_generator_to_affine(scalar, &pubkey)) {
        #pragma unroll
        for (int i = 0; i < 64; ++i) {
            out[i] = 0;
        }
        statuses[idx] = 0;
        return;
    }

    fe_to_big_endian(&pubkey.x, out);
    fe_to_big_endian(&pubkey.y, out + 32);
    statuses[idx] = 1;
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
