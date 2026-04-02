/*
 * cuda_hook.c — CUDA Driver API interception
 *
 * Derived from HAMi (https://github.com/Project-HAMi/HAMi)
 * Original Copyright: HAMi Authors, Apache License 2.0
 * Modifications Copyright 2025: PhoenixGPU Authors, Apache License 2.0
 *
 * Intercepts cuMemAlloc / cuMemFree / cuLaunchKernel to enforce
 * VRAM quotas and collect TFlops metering data.
 *
 * When compiled without CUDA (PHOENIX_STUB_MODE), all functions are
 * safe no-ops so the library can be built and tested on non-GPU hosts.
 */

#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdio.h>
#include <string.h>
#include <stdint.h>

#include "libvgpu.h"
#include "cuda_hook.h"
#include "phoenix_meter.h"

/* ── Real function pointers resolved at init ─────────────────── */
static void *(*g_real_dlsym)(void *, const char *) = NULL;
#ifndef PHOENIX_STUB_MODE
static void  *g_cuda_lib = NULL;
#endif

/* Typedef for key CUDA Driver API functions we intercept */
typedef int (*cuMemAlloc_fn)(void **dptr, size_t bytesize);
typedef int (*cuMemFree_fn)(void *dptr);
typedef int (*cuLaunchKernel_fn)(void *f,
    unsigned int gridDimX, unsigned int gridDimY, unsigned int gridDimZ,
    unsigned int blockDimX, unsigned int blockDimY, unsigned int blockDimZ,
    unsigned int sharedMemBytes, void *hStream,
    void **kernelParams, void **extra);

static cuMemAlloc_fn      real_cuMemAlloc      = NULL;
static cuMemFree_fn       real_cuMemFree       = NULL;
static cuLaunchKernel_fn  real_cuLaunchKernel  = NULL;

/* ── Default FLOPs heuristic per kernel launch ───────────────── */
#define DEFAULT_KERNEL_FLOPS_ESTIMATE  1000000ULL  /* 1 MFLOP placeholder */

/* ════════════════════════════════════════════════════════════════
 * Proxy: cuMemAlloc — enforce VRAM quota before allocation
 * ════════════════════════════════════════════════════════════════*/
static int proxy_cuMemAlloc(void **dptr, size_t bytesize) {
    if (phoenix_check_vram_alloc(bytesize) != 0) {
        /* CUDA_ERROR_OUT_OF_MEMORY = 2 */
        return 2;
    }
    int ret = real_cuMemAlloc ? real_cuMemAlloc(dptr, bytesize) : 2;
    if (ret == 0) {
        phoenix_record_alloc(bytesize);
    }
    return ret;
}

/* ════════════════════════════════════════════════════════════════
 * Proxy: cuMemFree — track deallocation
 * ════════════════════════════════════════════════════════════════*/
static int proxy_cuMemFree(void *dptr) {
    /* Note: real free tracks by pointer; we use a simplified byte
     * accounting model consistent with the shared state design. */
    int ret = real_cuMemFree ? real_cuMemFree(dptr) : 0;
    /* In a production implementation, we would look up the allocation
     * size from an internal map. For now the free accounting is handled
     * by explicit phoenix_record_free calls from higher layers. */
    return ret;
}

/* ════════════════════════════════════════════════════════════════
 * Proxy: cuLaunchKernel — SM throttle + TFlops metering
 * ════════════════════════════════════════════════════════════════*/
static int proxy_cuLaunchKernel(void *f,
        unsigned int gridDimX, unsigned int gridDimY, unsigned int gridDimZ,
        unsigned int blockDimX, unsigned int blockDimY, unsigned int blockDimZ,
        unsigned int sharedMemBytes, void *hStream,
        void **kernelParams, void **extra) {

    /* Throttle if SM utilization exceeds the configured limit */
    phoenix_throttle_sm_if_needed();

    /* Estimate FLOPs for billing (grid * block = total threads) */
    uint64_t threads = (uint64_t)gridDimX * gridDimY * gridDimZ *
                       (uint64_t)blockDimX * blockDimY * blockDimZ;
    uint64_t flops = threads > 0 ? threads : DEFAULT_KERNEL_FLOPS_ESTIMATE;
    phoenix_meter_record_kernel(flops);

    if (!real_cuLaunchKernel) return 0;
    return real_cuLaunchKernel(f,
        gridDimX, gridDimY, gridDimZ,
        blockDimX, blockDimY, blockDimZ,
        sharedMemBytes, hStream, kernelParams, extra);
}

/* ════════════════════════════════════════════════════════════════
 * Init: resolve real CUDA symbols
 * ════════════════════════════════════════════════════════════════*/
void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    g_real_dlsym = real_dlsym_fn;
    if (!g_real_dlsym) return;

#ifndef PHOENIX_STUB_MODE
    g_cuda_lib = dlopen("libcuda.so.1", RTLD_LAZY | RTLD_NOLOAD);
    if (!g_cuda_lib)
        g_cuda_lib = dlopen("libcuda.so", RTLD_LAZY | RTLD_NOLOAD);

    if (g_cuda_lib) {
        real_cuMemAlloc     = (cuMemAlloc_fn)g_real_dlsym(g_cuda_lib, "cuMemAlloc_v2");
        real_cuMemFree      = (cuMemFree_fn)g_real_dlsym(g_cuda_lib, "cuMemFree_v2");
        real_cuLaunchKernel = (cuLaunchKernel_fn)g_real_dlsym(g_cuda_lib, "cuLaunchKernel");
        fprintf(stderr, "[PhoenixGPU] CUDA hooks installed (cuMemAlloc=%p cuLaunchKernel=%p)\n",
                (void *)real_cuMemAlloc, (void *)real_cuLaunchKernel);
    } else {
        fprintf(stderr, "[PhoenixGPU] libcuda.so not loaded — CUDA hooks inactive\n");
    }
#else
    fprintf(stderr, "[PhoenixGPU] stub mode — CUDA hooks compiled out\n");
#endif
}

/* ════════════════════════════════════════════════════════════════
 * Lookup: return proxy if symbol matches a hooked CUDA function
 * ════════════════════════════════════════════════════════════════*/
void *cuda_hook_lookup(const char *symbol) {
    if (!symbol) return NULL;

    if (strcmp(symbol, "cuMemAlloc_v2") == 0 || strcmp(symbol, "cuMemAlloc") == 0)
        return (void *)proxy_cuMemAlloc;
    if (strcmp(symbol, "cuMemFree_v2") == 0 || strcmp(symbol, "cuMemFree") == 0)
        return (void *)proxy_cuMemFree;
    if (strcmp(symbol, "cuLaunchKernel") == 0)
        return (void *)proxy_cuLaunchKernel;

    return NULL;
}
