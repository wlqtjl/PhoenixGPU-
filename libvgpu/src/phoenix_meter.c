/*
 * phoenix_meter.c — TFlops metering subsystem
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 *
 * Accumulates FLOPs from intercepted cuLaunchKernel calls and
 * writes the running total to the per-container shared memory segment.
 * The billing engine reads this periodically to compute TFlops·h charges.
 */

#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <time.h>
#include <pthread.h>

#include "libvgpu.h"
#include "phoenix_meter.h"

/* ── Metering state ─────────────────────────────────────────────*/
static char     g_meter_job_uid[256]   = {0};
static char     g_meter_namespace[256] = {0};

static uint64_t g_kernel_count         = 0;
static double   g_flops_accumulated    = 0.0;

static pthread_mutex_t g_meter_lock = PTHREAD_MUTEX_INITIALIZER;

/* ── Shared state pointer (defined in hook.c) ───────────────── */
extern phoenix_shared_state_t *g_shared __attribute__((weak));

/* Helper: current time in nanoseconds */
static uint64_t now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}

/* ════════════════════════════════════════════════════════════════
 * Init: store job context for billing attribution
 * ════════════════════════════════════════════════════════════════*/
void phoenix_meter_init(const char *job_uid, const char *namespace_) {
    if (job_uid)
        strncpy(g_meter_job_uid, job_uid, sizeof(g_meter_job_uid) - 1);
    if (namespace_)
        strncpy(g_meter_namespace, namespace_, sizeof(g_meter_namespace) - 1);

    g_kernel_count      = 0;
    g_flops_accumulated = 0.0;

    fprintf(stderr, "[PhoenixGPU] meter init: job=%s ns=%s\n",
            g_meter_job_uid[0] ? g_meter_job_uid : "(none)",
            g_meter_namespace[0] ? g_meter_namespace : "(none)");
}

/* ════════════════════════════════════════════════════════════════
 * Record: called on every intercepted cuLaunchKernel
 * ════════════════════════════════════════════════════════════════*/
void phoenix_meter_record_kernel(uint64_t flops_estimate) {
    pthread_mutex_lock(&g_meter_lock);

    g_kernel_count++;
    g_flops_accumulated += (double)flops_estimate;

    /* Write to shared memory every 64 kernel launches to
     * amortise the cost of the shared-memory update. */
    if ((g_kernel_count & 63) == 0) {
        phoenix_meter_flush();
    }

    pthread_mutex_unlock(&g_meter_lock);
}

/* ════════════════════════════════════════════════════════════════
 * Flush: write accumulated metrics to shared memory
 * ════════════════════════════════════════════════════════════════*/
void phoenix_meter_flush(void) {
    /* g_shared is a weak extern — check it exists and is mapped */
    phoenix_shared_state_t *shared =
        (&g_shared != NULL) ? g_shared : NULL;

    if (shared && shared->magic == PHOENIX_SHM_MAGIC) {
        shared->kernel_launches    = g_kernel_count;
        /* Convert FLOPs → TFLOPs (10^12) */
        shared->tflops_accumulated = g_flops_accumulated / 1e12;
        shared->last_update_ns     = now_ns();
    }
}
