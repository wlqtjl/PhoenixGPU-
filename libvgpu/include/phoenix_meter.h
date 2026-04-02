/*
 * phoenix_meter.h — TFlops metering declarations
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Initialize the TFlops metering subsystem.
 * job_uid:    PhoenixJob UID for billing attribution.
 * namespace_: Kubernetes namespace for quota accounting.
 */
void phoenix_meter_init(const char *job_uid, const char *namespace_);

/*
 * Record a single kernel launch with an estimated FLOPs count.
 * Called from the cuLaunchKernel hook.
 */
void phoenix_meter_record_kernel(uint64_t flops_estimate);

/*
 * Flush accumulated metrics to shared memory.
 * Called periodically and on library unload.
 */
void phoenix_meter_flush(void);

#ifdef __cplusplus
}
#endif
