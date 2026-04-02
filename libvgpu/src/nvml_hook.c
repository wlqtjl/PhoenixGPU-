/*
 * nvml_hook.c — NVIDIA Management Library interception
 *
 * Derived from HAMi (https://github.com/Project-HAMi/HAMi)
 * Original Copyright: HAMi Authors, Apache License 2.0
 * Modifications Copyright 2025: PhoenixGPU Authors, Apache License 2.0
 *
 * Intercepts NVML memory-info and utilization queries so that
 * containers see only their allocated slice of GPU resources,
 * not the full physical device.
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
#include "nvml_hook.h"

/* ── Real function pointers resolved at init ─────────────────── */
static void *(*g_real_dlsym)(void *, const char *) = NULL;
#ifndef PHOENIX_STUB_MODE
static void  *g_nvml_lib = NULL;
#endif

/* Simplified NVML types (avoid requiring NVML headers at build time) */
typedef int (*nvmlDeviceGetMemoryInfo_fn)(void *device, void *memory);
typedef int (*nvmlDeviceGetUtilizationRates_fn)(void *device, void *utilization);

static nvmlDeviceGetMemoryInfo_fn        real_nvmlDeviceGetMemoryInfo        = NULL;
static nvmlDeviceGetUtilizationRates_fn  real_nvmlDeviceGetUtilizationRates  = NULL;

/* NVML memory info struct layout (matches nvmlMemory_t):
 *   uint64_t total;
 *   uint64_t free;
 *   uint64_t used;
 */
typedef struct {
    uint64_t total;
    uint64_t free;
    uint64_t used;
} phoenix_nvml_memory_t;

/* NVML utilization struct layout (matches nvmlUtilization_t):
 *   unsigned int gpu;
 *   unsigned int memory;
 */
typedef struct {
    unsigned int gpu;
    unsigned int memory;
} phoenix_nvml_utilization_t;

/* ── Reference to shared state (defined in hook.c) ──────────────
 * We access it via the public API functions declared in libvgpu.h.
 * The vram_limit is read from the shared state indirectly through
 * phoenix_check_vram_alloc. For the memory query proxy we use
 * the environment-based limit that was parsed at init time.
 */
extern size_t g_vram_limit_bytes __attribute__((weak));

/* ════════════════════════════════════════════════════════════════
 * Proxy: nvmlDeviceGetMemoryInfo — report virtual slice
 * ════════════════════════════════════════════════════════════════*/
static int proxy_nvmlDeviceGetMemoryInfo(void *device, void *memory_out) {
    int ret = real_nvmlDeviceGetMemoryInfo
        ? real_nvmlDeviceGetMemoryInfo(device, memory_out)
        : 0;

    if (ret == 0 && memory_out) {
        phoenix_nvml_memory_t *mem = (phoenix_nvml_memory_t *)memory_out;
        /* If a VRAM limit is configured, present the virtual slice */
        size_t limit = (&g_vram_limit_bytes != NULL) ? g_vram_limit_bytes : 0;
        if (limit > 0 && limit < mem->total) {
            mem->total = (uint64_t)limit;
            if (mem->used > mem->total)
                mem->used = mem->total;
            mem->free = mem->total - mem->used;
        }
    }
    return ret;
}

/* ════════════════════════════════════════════════════════════════
 * Proxy: nvmlDeviceGetUtilizationRates — report per-container view
 * ════════════════════════════════════════════════════════════════*/
static int proxy_nvmlDeviceGetUtilizationRates(void *device, void *util_out) {
    /* Pass through — actual per-container SM isolation is handled
     * by the throttle mechanism in hook.c, not by faking NVML numbers.
     * We hook this mainly so we can intercept and log if needed. */
    if (!real_nvmlDeviceGetUtilizationRates) return 0;
    return real_nvmlDeviceGetUtilizationRates(device, util_out);
}

/* ════════════════════════════════════════════════════════════════
 * Init: resolve real NVML symbols
 * ════════════════════════════════════════════════════════════════*/
void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    g_real_dlsym = real_dlsym_fn;
    if (!g_real_dlsym) return;

#ifndef PHOENIX_STUB_MODE
    g_nvml_lib = dlopen("libnvidia-ml.so.1", RTLD_LAZY | RTLD_NOLOAD);
    if (!g_nvml_lib)
        g_nvml_lib = dlopen("libnvidia-ml.so", RTLD_LAZY | RTLD_NOLOAD);

    if (g_nvml_lib) {
        real_nvmlDeviceGetMemoryInfo =
            (nvmlDeviceGetMemoryInfo_fn)g_real_dlsym(g_nvml_lib, "nvmlDeviceGetMemoryInfo");
        real_nvmlDeviceGetUtilizationRates =
            (nvmlDeviceGetUtilizationRates_fn)g_real_dlsym(g_nvml_lib, "nvmlDeviceGetUtilizationRates");
        fprintf(stderr, "[PhoenixGPU] NVML hooks installed (memInfo=%p utilRates=%p)\n",
                (void *)real_nvmlDeviceGetMemoryInfo,
                (void *)real_nvmlDeviceGetUtilizationRates);
    } else {
        fprintf(stderr, "[PhoenixGPU] libnvidia-ml.so not loaded — NVML hooks inactive\n");
    }
#else
    fprintf(stderr, "[PhoenixGPU] stub mode — NVML hooks compiled out\n");
#endif
}

/* ════════════════════════════════════════════════════════════════
 * Lookup: return proxy if symbol matches a hooked NVML function
 * ════════════════════════════════════════════════════════════════*/
void *nvml_hook_lookup(const char *symbol) {
    if (!symbol) return NULL;

    if (strcmp(symbol, "nvmlDeviceGetMemoryInfo") == 0)
        return (void *)proxy_nvmlDeviceGetMemoryInfo;
    if (strcmp(symbol, "nvmlDeviceGetUtilizationRates") == 0)
        return (void *)proxy_nvmlDeviceGetUtilizationRates;

    return NULL;
}
