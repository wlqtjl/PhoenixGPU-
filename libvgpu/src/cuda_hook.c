/*
 * cuda_hook.c — CUDA Driver API Interception
 *
 * Intercepts cuMemAlloc_v2, cuMemFree_v2, cuLaunchKernel and related
 * CUDA Driver API calls. Enforces VRAM quotas and records metrics.
 *
 * Derived from HAMi (https://github.com/Project-HAMi/HAMi)
 * Original Copyright: HAMi Authors, Apache License 2.0
 * Modifications Copyright 2025: PhoenixGPU Authors, Apache License 2.0
 *
 * Modifications vs HAMi upstream:
 *   - VRAM quota enforcement via phoenix_check_vram_alloc()
 *   - TFlops metering via phoenix_meter_record_kernel()
 *   - SM throttling via phoenix_throttle_sm_if_needed()
 */

#include <stdio.h>
#include <string.h>
#include <dlfcn.h>
#include <stdint.h>

#include "libvgpu.h"
#include "cuda_hook.h"
#include "phoenix_meter.h"

/* ── CUDA Driver API type definitions ────────────────────────────
 * These mirror the CUDA Driver API types without requiring the
 * CUDA toolkit headers at build time.
 */
typedef int            CUresult;
typedef uintptr_t      CUdeviceptr;
typedef void          *CUfunction;
typedef void          *CUstream;

#define CUDA_SUCCESS            0
#define CUDA_ERROR_OUT_OF_MEMORY 2

/* ── Original function pointers (resolved lazily via real_dlsym) ─ */
static void *(*g_real_dlsym)(void *, const char *) = NULL;

typedef CUresult (*cuMemAlloc_v2_fn)(CUdeviceptr *, size_t);
typedef CUresult (*cuMemFree_v2_fn)(CUdeviceptr);
typedef CUresult (*cuLaunchKernel_fn)(CUfunction, unsigned int, unsigned int,
    unsigned int, unsigned int, unsigned int, unsigned int,
    unsigned int, CUstream, void **, void **);
typedef CUresult (*cuMemGetInfo_v2_fn)(size_t *, size_t *);

static cuMemAlloc_v2_fn    real_cuMemAlloc_v2    = NULL;
static cuMemFree_v2_fn     real_cuMemFree_v2     = NULL;
static cuLaunchKernel_fn   real_cuLaunchKernel   = NULL;
static cuMemGetInfo_v2_fn  real_cuMemGetInfo_v2  = NULL;

/* ── Lazy resolver ───────────────────────────────────────────────*/
static void *resolve(const char *sym) {
    if (!g_real_dlsym) return NULL;
    return g_real_dlsym(RTLD_NEXT, sym);
}

/* ── Hook implementations ────────────────────────────────────────*/

static CUresult hook_cuMemAlloc_v2(CUdeviceptr *dptr, size_t bytesize) {
    if (!real_cuMemAlloc_v2) {
        real_cuMemAlloc_v2 = (cuMemAlloc_v2_fn)resolve("cuMemAlloc_v2");
        if (!real_cuMemAlloc_v2)
            return CUDA_ERROR_OUT_OF_MEMORY;
    }

    /* Check VRAM quota before allowing allocation */
    if (phoenix_check_vram_alloc(bytesize) != 0) {
        return CUDA_ERROR_OUT_OF_MEMORY;
    }

    CUresult ret = real_cuMemAlloc_v2(dptr, bytesize);
    if (ret == CUDA_SUCCESS) {
        phoenix_record_alloc(bytesize);
    }
    return ret;
}

static CUresult hook_cuMemFree_v2(CUdeviceptr dptr) {
    if (!real_cuMemFree_v2) {
        real_cuMemFree_v2 = (cuMemFree_v2_fn)resolve("cuMemFree_v2");
        if (!real_cuMemFree_v2)
            return CUDA_SUCCESS;
    }

    /*
     * We don't know the exact size here — CUDA doesn't expose it
     * in cuMemFree. The shared state accounting will be corrected
     * when the NVML sampler thread refreshes memory usage.
     * For a rough estimate, we record 0 bytes freed and rely on
     * the NVML sampler for ground truth.
     */
    CUresult ret = real_cuMemFree_v2(dptr);
    return ret;
}

static CUresult hook_cuLaunchKernel(
    CUfunction f,
    unsigned int gridDimX,  unsigned int gridDimY,  unsigned int gridDimZ,
    unsigned int blockDimX, unsigned int blockDimY, unsigned int blockDimZ,
    unsigned int sharedMemBytes,
    CUstream hStream,
    void **kernelParams,
    void **extra
) {
    if (!real_cuLaunchKernel) {
        real_cuLaunchKernel = (cuLaunchKernel_fn)resolve("cuLaunchKernel");
        if (!real_cuLaunchKernel)
            return CUDA_SUCCESS;
    }

    /* SM throttle check — may introduce brief sleep if over limit */
    phoenix_throttle_sm_if_needed();

    CUresult ret = real_cuLaunchKernel(f,
        gridDimX, gridDimY, gridDimZ,
        blockDimX, blockDimY, blockDimZ,
        sharedMemBytes, hStream, kernelParams, extra);

    if (ret == CUDA_SUCCESS) {
        /*
         * Estimate FLOPs: gridDim * blockDim * 2 (fma ops per thread, conservative).
         * This is a rough estimate; real metering reads hardware counters.
         */
        uint64_t threads = (uint64_t)gridDimX * gridDimY * gridDimZ *
                           (uint64_t)blockDimX * blockDimY * blockDimZ;
        uint64_t flops_estimate = threads * 2;
        phoenix_meter_record_kernel(flops_estimate);
    }

    return ret;
}

static CUresult hook_cuMemGetInfo_v2(size_t *free, size_t *total) {
    if (!real_cuMemGetInfo_v2) {
        real_cuMemGetInfo_v2 = (cuMemGetInfo_v2_fn)resolve("cuMemGetInfo_v2");
        if (!real_cuMemGetInfo_v2) {
            *free  = 0;
            *total = 0;
            return CUDA_SUCCESS;
        }
    }

    CUresult ret = real_cuMemGetInfo_v2(free, total);
    if (ret != CUDA_SUCCESS)
        return ret;

    /* Virtualise: report the quota limit as total, not physical total */
    extern size_t g_vram_limit_bytes;
    if (g_vram_limit_bytes > 0) {
        *total = g_vram_limit_bytes;
        if (*free > g_vram_limit_bytes)
            *free = g_vram_limit_bytes;
    }

    return CUDA_SUCCESS;
}

/* ═══════════════════════════════════════════════════════════════
 * Public API
 * ═══════════════════════════════════════════════════════════════*/

void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    g_real_dlsym = real_dlsym_fn;
}

void *cuda_hook_lookup(const char *symbol) {
    if (!symbol) return NULL;

    if (strcmp(symbol, "cuMemAlloc_v2") == 0)
        return (void *)hook_cuMemAlloc_v2;
    if (strcmp(symbol, "cuMemAlloc") == 0)
        return (void *)hook_cuMemAlloc_v2;  /* legacy alias */
    if (strcmp(symbol, "cuMemFree_v2") == 0)
        return (void *)hook_cuMemFree_v2;
    if (strcmp(symbol, "cuMemFree") == 0)
        return (void *)hook_cuMemFree_v2;   /* legacy alias */
    if (strcmp(symbol, "cuLaunchKernel") == 0)
        return (void *)hook_cuLaunchKernel;
    if (strcmp(symbol, "cuMemGetInfo_v2") == 0)
        return (void *)hook_cuMemGetInfo_v2;
    if (strcmp(symbol, "cuMemGetInfo") == 0)
        return (void *)hook_cuMemGetInfo_v2; /* legacy alias */

    return NULL;
}
