#include "libvgpu.h"

void phoenix_meter_init(const char *job_uid, const char *namespace_) {
    (void)job_uid;
    (void)namespace_;
}

void phoenix_meter_record_kernel(uint64_t flops_estimate) {
    (void)flops_estimate;
}

void phoenix_meter_flush(void) {}
