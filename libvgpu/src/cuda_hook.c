#include "libvgpu.h"

void cuda_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    (void)real_dlsym_fn;
}

void *cuda_hook_lookup(const char *symbol) {
    (void)symbol;
    return 0;
}
