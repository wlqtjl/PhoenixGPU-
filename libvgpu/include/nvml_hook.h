/*
 * nvml_hook.h — NVML Hook Declarations
 *
 * Intercepts NVML calls so that vGPU containers see virtualised
 * device properties (memory, utilization) matching their quota.
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/*
 * nvml_hook_init — Initialize the NVML hook table.
 * Must be called once from the library constructor.
 *
 * @param real_dlsym_fn  Pointer to the real dlsym.
 */
void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *));

/*
 * nvml_hook_lookup — Look up an NVML symbol.
 * Returns our proxy function if the symbol is hooked, NULL otherwise.
 *
 * @param symbol  The symbol name being looked up (e.g. "nvmlDeviceGetMemoryInfo").
 * @return  Pointer to the proxy function, or NULL if not intercepted.
 */
void *nvml_hook_lookup(const char *symbol);

#ifdef __cplusplus
}
#endif
