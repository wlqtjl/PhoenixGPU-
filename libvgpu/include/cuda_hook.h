/*
 * cuda_hook.h — CUDA Driver API hook declarations
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Initialize the CUDA Driver API hook table.
 * Must be called once from the library constructor (phoenix_init).
 * real_dlsym_fn: pointer to the original dlsym, used to resolve
 *                real CUDA symbols at runtime.
 */
void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *));

/*
 * Look up a hooked CUDA Driver API symbol.
 * Returns our proxy function pointer if the symbol is hooked,
 * or NULL if it should be passed through to the real implementation.
 */
void *cuda_hook_lookup(const char *symbol);

#ifdef __cplusplus
}
#endif
