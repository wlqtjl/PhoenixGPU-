/*
 * phoenix_meter.c - TFlops Metering for PhoenixGPU Billing
 *
 * Records kernel launch events and accumulates estimated TFlops
 * into per-container shared memory.
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */

#include <stdio.h>
#include <string.h>
#include <time.h>
#include <stdint.h>
#include <pthread.h>

#include "libvgpu.h"
#include "phoenix_meter.h"

static char          g_meter_job_uid[256]   = {0};
static char          g_meter_namespace[256] = {0};
static uint64_t      g_pending_flops        = 0;
static uint64_t      g_pending_kernels      = 0;
static pthread_mutex_t g_meter_lock         = PTHREAD_MUTEX_INITIALIZER;

#define FLUSH_KERNEL_THRESHOLD 1000

static uint64_t now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}

void phoenix_meter_init(const char *job_uid, const char *namespace_) {
    if (job_uid)
        strncpy(g_meter_job_uid, job_uid, sizeof(g_meter_job_uid) - 1);
    if (namespace_)
        strncpy(g_meter_namespace, namespace_, sizeof(g_meter_namespace) - 1);
    g_pending_flops   = 0;
    g_pending_kernels = 0;
}

void phoenix_meter_record_kernel(uint64_t flops_estimate) {
    if (flops_estimate == 0)
        flops_estimate = 1024;

    pthread_mutex_lock(&g_meter_lock);
    g_pending_flops   += flops_estimate;
    g_pending_kernels += 1;
    if (g_pending_kernels >= FLUSH_KERNEL_THRESHOLD) {
        pthread_mutex_unlock(&g_meter_lock);
        phoenix_meter_flush();
        return;
    }
    pthread_mutex_unlock(&g_meter_lock);
}

void phoenix_meter_flush(void) {
    pthread_mutex_lock(&g_meter_lock);
    uint64_t flops   = g_pending_flops;
    uint64_t kernels = g_pending_kernels;
    g_pending_flops   = 0;
    g_pending_kernels = 0;
    pthread_mutex_unlock(&g_meter_lock);

    if (flops == 0 && kernels == 0)
        return;

    extern phoenix_shared_state_t *g_shared;
    if (g_shared) {
        double tflops_delta = (double)flops / 1e12;
        g_shared->tflops_accumulated += tflops_delta;
        g_shared->kernel_launches    += kernels;
        g_shared->last_update_ns      = now_ns();
    }
}
