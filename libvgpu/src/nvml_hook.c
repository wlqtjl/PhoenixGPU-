#include "libvgpu.h"

void nvml_hook_init(void *(*real_dlsym_fn)(void *, const char *)) {
    (void)real_dlsym_fn;
}

void *nvml_hook_lookup(const char *symbol) {
    (void)symbol;
    return 0;
}
