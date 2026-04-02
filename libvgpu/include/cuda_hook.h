/*
 * cuda_hook.h — CUDA Driver API Hook Declarations
 *
 * Intercepts cuMemAlloc, cuMemFree, cuLaunchKernel and other
 * CUDA Driver API calls for VRAM quota enforcement and metering.
 *
 * Copyright 2025 PhoenixGPU Authors
 * SPDX-License-Identifier: Apache-2.0
 */
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/*
 * cuda_hook_init — Initialize the CUDA hook table.
 * Must be called once from the library constructor.
 *
 * @param real_dlsym_fn  Pointer to the real dlsym, used to resolve
 *                       original CUDA driver symbols at runtime.
 */
void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *));

/*
 * cuda_hook_lookup — Look up a CUDA Driver API symbol.
 * Returns our proxy function if the symbol is hooked, NULL otherwise.
 *
 * @param symbol  The symbol name being looked up (e.g. "cuMemAlloc_v2").
 * @return  Pointer to the proxy function, or NULL if not intercepted.
 */
void *cuda_hook_lookup(const char *symbol);

#ifdef __cplusplus
}
#endif
