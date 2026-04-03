/*
 * nvml_hook.c — NVML Interception Layer
 *
 * Intercepts NVML calls so that vGPU containers see virtualised
 * memory and utilization values that match their configured quota,
 * rather than the full physical GPU values.
 *
 * Derived from HAMi (https://github.com/Project-HAMi/HAMi)
 * Original Copyright: HAMi Authors, Apache License 2.0
 * Modifications Copyright 2025: PhoenixGPU Authors, Apache License 2.0
 *
 * Modifications vs HAMi upstream:
 *   - Virtualised memory reporting based on PHOENIX_VRAM_LIMIT_MB
 *   - SM utilization sampling writes to shared memory for throttle
 */

#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <dlfcn.h>
#include <stdint.h>
#include <pthread.h>

#include "libvgpu.h"
#include "nvml_hook.h"

/* ── NVML type definitions (no NVML headers required) ────────────*/
typedef int nvmlReturn_t;
typedef void *nvmlDevice_t;

#define NVML_SUCCESS 0

typedef struct {
    uint64_t total;
    uint64_t free;
    uint64_t used;
} nvml_memory_t;

typedef struct {
    unsigned int gpu;    /* percent 0-100 */
    unsigned int memory; /* percent 0-100 */
} nvml_utilization_t;

/* ── Original function pointers ──────────────────────────────────*/
static void *(*g_real_dlsym)(void *, const char *) = NULL;

typedef nvmlReturn_t (*nvmlDeviceGetMemoryInfo_fn)(nvmlDevice_t, nvml_memory_t *);
typedef nvmlReturn_t (*nvmlDeviceGetUtilizationRates_fn)(nvmlDevice_t, nvml_utilization_t *);
typedef nvmlReturn_t (*nvmlInit_v2_fn)(void);
typedef nvmlReturn_t (*nvmlShutdown_fn)(void);

static nvmlDeviceGetMemoryInfo_fn         real_nvmlDeviceGetMemoryInfo         = NULL;
static nvmlDeviceGetUtilizationRates_fn   real_nvmlDeviceGetUtilizationRates   = NULL;
static nvmlInit_v2_fn                     real_nvmlInit_v2                     = NULL;
static nvmlShutdown_fn                    real_nvmlShutdown                    = NULL;

/* ── Lazy resolver ───────────────────────────────────────────────*/
static void *resolve(const char *sym) {
    if (!g_real_dlsym) return NULL;
    return g_real_dlsym(RTLD_NEXT, sym);
}

/* ── Hook implementations ────────────────────────────────────────*/

static nvmlReturn_t hook_nvmlDeviceGetMemoryInfo(nvmlDevice_t device, nvml_memory_t *memory) {
    if (!real_nvmlDeviceGetMemoryInfo) {
        real_nvmlDeviceGetMemoryInfo =
            (nvmlDeviceGetMemoryInfo_fn)resolve("nvmlDeviceGetMemoryInfo");
        if (!real_nvmlDeviceGetMemoryInfo) {
            memory->total = 0;
            memory->free  = 0;
            memory->used  = 0;
            return NVML_SUCCESS;
        }
    }

    nvmlReturn_t ret = real_nvmlDeviceGetMemoryInfo(device, memory);
    if (ret != NVML_SUCCESS)
        return ret;

    /* Virtualise: report quota limit as total memory */
    extern size_t g_vram_limit_bytes;
    if (g_vram_limit_bytes > 0) {
        uint64_t limit = (uint64_t)g_vram_limit_bytes;
        if (memory->used > limit) {
            memory->used = limit;
        }
        memory->total = limit;
        memory->free  = limit - memory->used;
    }

    return NVML_SUCCESS;
}

static nvmlReturn_t hook_nvmlDeviceGetUtilizationRates(
    nvmlDevice_t device, nvml_utilization_t *utilization
) {
    if (!real_nvmlDeviceGetUtilizationRates) {
        real_nvmlDeviceGetUtilizationRates =
            (nvmlDeviceGetUtilizationRates_fn)resolve("nvmlDeviceGetUtilizationRates");
        if (!real_nvmlDeviceGetUtilizationRates) {
            utilization->gpu    = 0;
            utilization->memory = 0;
            return NVML_SUCCESS;
        }
    }

    nvmlReturn_t ret = real_nvmlDeviceGetUtilizationRates(device, utilization);
    if (ret != NVML_SUCCESS)
        return ret;

    /* Write SM utilization to shared memory for throttle decisions */
    extern phoenix_shared_state_t *g_shared;
    if (g_shared) {
        g_shared->sm_utilization_pct = (float)utilization->gpu;
    }

    return NVML_SUCCESS;
}

static nvmlReturn_t hook_nvmlInit_v2(void) {
    if (!real_nvmlInit_v2) {
        real_nvmlInit_v2 = (nvmlInit_v2_fn)resolve("nvmlInit_v2");
        if (!real_nvmlInit_v2)
            return NVML_SUCCESS; /* graceful: pretend success if NVML absent */
    }
    return real_nvmlInit_v2();
}

static nvmlReturn_t hook_nvmlShutdown(void) {
    if (!real_nvmlShutdown) {
        real_nvmlShutdown = (nvmlShutdown_fn)resolve("nvmlShutdown");
        if (!real_nvmlShutdown)
            return NVML_SUCCESS;
    }
    return real_nvmlShutdown();
}

/* ═══════════════════════════════════════════════════════════════
 * Public API
 * ═══════════════════════════════════════════════════════════════*/

void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    g_real_dlsym = real_dlsym_fn;
}

void *nvml_hook_lookup(const char *symbol) {
    if (!symbol) return NULL;

    if (strcmp(symbol, "nvmlDeviceGetMemoryInfo") == 0)
        return (void *)hook_nvmlDeviceGetMemoryInfo;
    if (strcmp(symbol, "nvmlDeviceGetUtilizationRates") == 0)
        return (void *)hook_nvmlDeviceGetUtilizationRates;
    if (strcmp(symbol, "nvmlInit_v2") == 0)
        return (void *)hook_nvmlInit_v2;
    if (strcmp(symbol, "nvmlInit") == 0)
        return (void *)hook_nvmlInit_v2;  /* legacy alias */
    if (strcmp(symbol, "nvmlShutdown") == 0)
        return (void *)hook_nvmlShutdown;

    return NULL;
}
