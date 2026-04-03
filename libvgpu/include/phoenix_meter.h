/*
 * phoenix_meter.h — TFlops Metering Declarations
 *
 * Records kernel launch timing and estimates TFlops for
 * PhoenixGPU's per-job billing system.
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
 * phoenix_meter_init — Initialize the TFlops metering subsystem.
 *
 * @param job_uid     PhoenixJob UID (from PHOENIX_JOB_UID env var).
 * @param namespace_  Pod namespace (from PHOENIX_POD_NAMESPACE env var).
 */
void phoenix_meter_init(const char *job_uid, const char *namespace_);

/*
 * phoenix_meter_record_kernel — Record a single kernel launch.
 * Called from cuda_hook after each cuLaunchKernel.
 *
 * @param flops_estimate  Estimated FLOPs for this kernel launch.
 *                        If 0, a default estimate is used.
 */
void phoenix_meter_record_kernel(uint64_t flops_estimate);

/*
 * phoenix_meter_flush — Flush accumulated metrics to shared memory.
 * Called periodically and from the destructor.
 */
void phoenix_meter_flush(void);

#ifdef __cplusplus
}
#endif
