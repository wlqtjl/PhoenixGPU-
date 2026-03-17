/*
 * libvgpu.h — PhoenixGPU CUDA Interception Layer Public Header
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ── Shared memory magic number ────────────────────────────────── */
#define PHOENIX_SHM_MAGIC  0x504E5800u  /* 'PNX\0' */

/* ── Per-container shared state (mmap'd to /tmp/phoenix-<uid>.shm) */
typedef struct {
    uint32_t magic;                  /* PHOENIX_SHM_MAGIC */
    uint32_t pid;                    /* main process PID */

    /* VRAM accounting */
    size_t   vram_allocated_bytes;   /* current allocated VRAM */
    size_t   vram_limit_bytes;       /* configured limit */

    /* SM utilization (updated by NVML sampler thread) */
    float    sm_utilization_pct;     /* 0.0 – 100.0 */

    /* TFlops metering (Phoenix extension) */
    double   tflops_accumulated;     /* total TFlops computed since start */
    uint64_t kernel_launches;        /* number of cuLaunchKernel calls */
    uint64_t last_update_ns;         /* epoch ns of last update */

    /* Padding for cache line alignment */
    uint8_t  _pad[64];
} phoenix_shared_state_t;

/* ── Quota check API (called from cuda_hook) ────────────────────── */
int  phoenix_check_vram_alloc(size_t request_bytes);
void phoenix_record_alloc(size_t bytes);
void phoenix_record_free(size_t bytes);
void phoenix_throttle_sm_if_needed(void);

/* ── Hook init (called from constructor) ────────────────────────── */
void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *));
void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *));
void *cuda_hook_lookup(const char *symbol);
void *nvml_hook_lookup(const char *symbol);

/* ── TFlops metering (Phoenix extension) ────────────────────────── */
void phoenix_meter_init(const char *job_uid, const char *namespace_);
void phoenix_meter_record_kernel(uint64_t flops_estimate);
void phoenix_meter_flush(void);

#ifdef __cplusplus
}
#endif
