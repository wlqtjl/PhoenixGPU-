/*
 * libvgpu.so — CUDA Driver API Interception Layer
 *
 * Derived from HAMi (https://github.com/Project-HAMi/HAMi)
 * Original Copyright: HAMi Authors, Apache License 2.0
 * Modifications Copyright 2025: PhoenixGPU Authors, Apache License 2.0
 *
 * Modifications vs HAMi upstream:
 *   - Added TFlops·h metering via cuLaunchKernel timing (PHOENIX_TFLOPS_METERING)
 *   - Added PhoenixJob context propagation (PHOENIX_JOB_CONTEXT)
 *   - Added SM utilization sampling for billing (PHOENIX_SM_SAMPLING)
 *   - Refactored quota tracking to support namespaced accounting
 *
 * Mechanism:
 *   Injected via LD_PRELOAD. Overrides dlsym() so that when the application
 *   calls any CUDA Driver API function, it hits our proxy first.
 *   The proxy enforces VRAM quotas and records utilization metrics.
 */

#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/time.h>
#include <pthread.h>
#include <fcntl.h>
#include <sys/mman.h>

#include "libvgpu.h"
#include "cuda_hook.h"
#include "nvml_hook.h"
#include "phoenix_meter.h"

/* ── Shared memory layout ────────────────────────────────────────
 * /tmp/phoenix-{pod-uid}.shm  (per-container, per-GPU)
 * Layout matches PhoenixSharedState in libvgpu.h
 */
/* Non-static: accessed from cuda_hook.c, nvml_hook.c, phoenix_meter.c
 * via weak extern declarations. */
phoenix_shared_state_t *g_shared = NULL;
static pthread_mutex_t  g_lock   = PTHREAD_MUTEX_INITIALIZER;

/* ── Original dlsym pointer (before our override) ───────────────*/
static void *(*real_dlsym)(void *, const char *) = NULL;

/* ── Environment variables read at init ─────────────────────────
 *   PHOENIX_VRAM_LIMIT_MB   Hard VRAM limit in MiB (set by Device Plugin)
 *   PHOENIX_SM_LIMIT_PCT    SM utilization ceiling 1-100 (set by Device Plugin)
 *   PHOENIX_JOB_UID         PhoenixJob UID for metering (set by Webhook)
 *   PHOENIX_POD_NAMESPACE   Pod namespace for quota accounting
 */
size_t  g_vram_limit_bytes = 0;
static int     g_sm_limit_pct    = 100;
static char    g_job_uid[256]    = {0};
static char    g_namespace[256]  = {0};

/* ── Forward declarations ────────────────────────────────────────*/
static void  __attribute__((constructor)) phoenix_init(void);
static void  __attribute__((destructor))  phoenix_fini(void);
static int   init_shared_memory(void);
static void  read_env_config(void);

/* ════════════════════════════════════════════════════════════════
 * Constructor: called when libvgpu.so is loaded via LD_PRELOAD
 * ════════════════════════════════════════════════════════════════*/
static void __attribute__((constructor)) phoenix_init(void) {
    /* Grab the real dlsym before we override it */
    real_dlsym = dlsym(RTLD_NEXT, "dlsym");
    if (!real_dlsym) {
        fprintf(stderr, "[PhoenixGPU] FATAL: cannot find real dlsym\n");
        abort();
    }

    read_env_config();

    if (init_shared_memory() != 0) {
        fprintf(stderr, "[PhoenixGPU] WARNING: shared memory init failed, "
                        "quota enforcement may be degraded\n");
    }

    /* Initialize CUDA and NVML hook tables */
    cuda_hook_init(real_dlsym);
    nvml_hook_init(real_dlsym);

    /* Initialize TFlops metering (Phoenix extension) */
    phoenix_meter_init(g_job_uid, g_namespace);

    fprintf(stderr, "[PhoenixGPU] libvgpu loaded: vram_limit=%zuMiB sm_limit=%d%% "
                    "job=%s ns=%s\n",
            g_vram_limit_bytes / (1024*1024),
            g_sm_limit_pct,
            g_job_uid[0] ? g_job_uid : "(none)",
            g_namespace[0] ? g_namespace : "(none)");
}

static void __attribute__((destructor)) phoenix_fini(void) {
    phoenix_meter_flush();
    if (g_shared) {
        munmap(g_shared, sizeof(phoenix_shared_state_t));
        g_shared = NULL;
    }
}

/* ════════════════════════════════════════════════════════════════
 * dlsym override — the central hook dispatch
 * ════════════════════════════════════════════════════════════════*/
void *dlsym(void *handle, const char *symbol) {
    /* ── CUDA Driver API hooks ──*/
    void *cuda_fn = cuda_hook_lookup(symbol);
    if (cuda_fn) return cuda_fn;

    /* ── NVML hooks ──*/
    void *nvml_fn = nvml_hook_lookup(symbol);
    if (nvml_fn) return nvml_fn;

    /* Not a hooked symbol — pass through to real dlsym */
    return real_dlsym(handle, symbol);
}

/* ════════════════════════════════════════════════════════════════
 * Core quota check — called by cuda_hook before cuMemAlloc
 * ════════════════════════════════════════════════════════════════
 * Returns 0 if allocation is allowed, -1 if it would exceed quota.
 */
int phoenix_check_vram_alloc(size_t request_bytes) {
    if (g_vram_limit_bytes == 0) return 0; /* no limit configured */

    pthread_mutex_lock(&g_lock);
    size_t current = g_shared ? g_shared->vram_allocated_bytes : 0;
    int allowed = (current + request_bytes) <= g_vram_limit_bytes;
    pthread_mutex_unlock(&g_lock);

    if (!allowed) {
        fprintf(stderr,
            "[PhoenixGPU] VRAM quota exceeded: requested=%zuMiB "
            "current=%zuMiB limit=%zuMiB\n",
            request_bytes / (1024*1024),
            current / (1024*1024),
            g_vram_limit_bytes / (1024*1024));
    }
    return allowed ? 0 : -1;
}

/* Record a successful VRAM allocation */
void phoenix_record_alloc(size_t bytes) {
    pthread_mutex_lock(&g_lock);
    if (g_shared) g_shared->vram_allocated_bytes += bytes;
    pthread_mutex_unlock(&g_lock);
}

/* Record a VRAM free */
void phoenix_record_free(size_t bytes) {
    pthread_mutex_lock(&g_lock);
    if (g_shared && g_shared->vram_allocated_bytes >= bytes)
        g_shared->vram_allocated_bytes -= bytes;
    pthread_mutex_unlock(&g_lock);
}

/* ════════════════════════════════════════════════════════════════
 * Phoenix extension: SM utilization soft throttle
 * Called from cuda_hook on cuLaunchKernel.
 * If SM usage is above limit, introduces a brief sleep to throttle.
 * ════════════════════════════════════════════════════════════════*/
void phoenix_throttle_sm_if_needed(void) {
    if (g_sm_limit_pct >= 100) return;
    if (!g_shared) return;

    int current_pct = (int)g_shared->sm_utilization_pct;
    if (current_pct > g_sm_limit_pct) {
        /* Simple token-bucket style throttle: sleep proportional to excess */
        int excess = current_pct - g_sm_limit_pct;
        usleep(excess * 100); /* 100µs per percentage point over limit */
    }
}

/* ════════════════════════════════════════════════════════════════
 * Internal helpers
 * ════════════════════════════════════════════════════════════════*/

static void read_env_config(void) {
    const char *vram = getenv("PHOENIX_VRAM_LIMIT_MB");
    if (vram) g_vram_limit_bytes = (size_t)atol(vram) * 1024 * 1024;

    const char *sm = getenv("PHOENIX_SM_LIMIT_PCT");
    if (sm) g_sm_limit_pct = atoi(sm);

    const char *job = getenv("PHOENIX_JOB_UID");
    if (job) strncpy(g_job_uid, job, sizeof(g_job_uid) - 1);

    const char *ns = getenv("PHOENIX_POD_NAMESPACE");
    if (ns) strncpy(g_namespace, ns, sizeof(g_namespace) - 1);
}

static int init_shared_memory(void) {
    char shm_path[512];
    const char *pod_uid = getenv("PHOENIX_POD_UID");
    if (!pod_uid) pod_uid = "default";

    snprintf(shm_path, sizeof(shm_path), "/tmp/phoenix-%s.shm", pod_uid);

    int fd = open(shm_path, O_RDWR | O_CREAT, 0600);
    if (fd < 0) return -1;

    if (ftruncate(fd, sizeof(phoenix_shared_state_t)) != 0) {
        close(fd);
        return -1;
    }

    g_shared = mmap(NULL, sizeof(phoenix_shared_state_t),
                    PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    close(fd);

    if (g_shared == MAP_FAILED) {
        g_shared = NULL;
        return -1;
    }

    /* Initialize on first use */
    if (g_shared->magic != PHOENIX_SHM_MAGIC) {
        memset(g_shared, 0, sizeof(phoenix_shared_state_t));
        g_shared->magic = PHOENIX_SHM_MAGIC;
        g_shared->pid   = (uint32_t)getpid();
    }

    return 0;
}
