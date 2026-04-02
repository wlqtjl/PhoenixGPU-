/*
 * nvml_hook.h — NVIDIA Management Library hook declarations
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Initialize the NVML hook table.
 * Must be called once from the library constructor (phoenix_init).
 * real_dlsym_fn: pointer to the original dlsym, used to resolve
 *                real NVML symbols at runtime.
 */
void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *));

/*
 * Look up a hooked NVML symbol.
 * Returns our proxy function pointer if the symbol is hooked,
 * or NULL if it should be passed through to the real implementation.
 */
void *nvml_hook_lookup(const char *symbol);

#ifdef __cplusplus
}
#endif
