# PhoenixGPU 工程规约 (Engineering Covenant)

> 本规约是 PhoenixGPU 所有代码必须遵守的强制性标准。
> 违反任意一条，PR 将被拒绝合并。
> 基于 Obra/Superpowers 框架 + 项目实战经验迭代。

---

## 一、开发流程（Superpowers 强制）

1. **禁止跳过头脑风暴**：任何新功能必须先经过 `brainstorming` 阶段
2. **先写测试（TDD）**：红 → 绿 → 重构，无测试的代码不进 main
3. **YAGNI**：不实现"将来可能用到"的功能；动态调整、自适应算法是 V2 的事
4. **DRY**：超过两次重复的逻辑必须抽象；但不要过度抽象（YAGNI 制衡）
5. **任务颗粒度**：每个任务 2-8 小时完成，超出必须拆分
6. **两阶段 Code Review**：
   - 阶段一：规范符合性（本文档）
   - 阶段二：并发安全 / 资源泄漏 / 逻辑正确性

---

## 二、Go 代码规范

### 2.1 错误处理（强制）

```go
// ✅ 正确：error 链携带上下文
if err := doSomething(); err != nil {
    return fmt.Errorf("checkpoint dump pid=%d dir=%s: %w", pid, dir, err)
}

// ❌ 禁止：直接返回裸 error
return err

// ❌ 禁止：库函数中 panic
panic("something went wrong")

// ❌ 禁止：忽略 error
os.Remove(tmpFile) // 没有检查 error
```

### 2.2 并发安全（强制）

```go
// ✅ 优先原子操作（适用于计数器、状态标志）
var allocated int64
atomic.AddInt64(&allocated, int64(bytes))

// ✅ Mutex 用于复杂结构体保护，但必须明确锁的粒度
mu.Lock()
// 临界区保持最短：只包含必须串行的操作
state.vram += bytes
mu.Unlock()

// ❌ 禁止：持锁调用外部 I/O 或 RPC
mu.Lock()
resp, err := s3Client.Upload(...)  // 这会导致长时间锁持有
mu.Unlock()

// ❌ 禁止：libvgpu hook 路径上的 Mutex（高频调用，用 atomic）
```

### 2.3 K8s Controller 规范（强制）

```go
// ✅ Reconcile 必须幂等：多次调用结果相同
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 先 Get，确认资源当前状态
    // 根据"期望状态 vs 实际状态"决定操作
    // 操作必须可重入
}

// ✅ 非阻塞：长时操作放 goroutine，Reconcile 立即返回
go r.triggerCheckpoint(context.Background(), job)
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

// ❌ 禁止：Reconcile 中同步等待耗时操作
err := r.doCheckpoint(ctx, job) // 可能耗时 30s+，会堵塞 worker pool
```

### 2.4 Channel 与 Worker Pool（Sprint 2 起适用）

```go
// ✅ 带缓冲 Channel 防止 I/O 波动挤爆内存
tasks := make(chan SnapshotTask, 64) // 缓冲区大小通过配置文件调整

// ✅ 固定 Worker Pool（数量从配置读取，默认 4）
for i := 0; i < cfg.UploadWorkers; i++ {
    go func() {
        for task := range tasks {
            uploadWithContext(ctx, task)
        }
    }()
}

// ✅ Context 超时隔离：S3 上传超时不能阻塞本地 Checkpoint 生成
uploadCtx, cancel := context.WithTimeout(ctx, cfg.S3UploadTimeout)
defer cancel()
if err := upload(uploadCtx, task); err != nil {
    log.Warn("S3 upload failed, local snapshot retained", zap.Error(err))
    // 不 fatal — 本地快照仍然有效
}

// ❌ 禁止：动态调整 Worker 数量（V2 功能，当前 YAGNI）
workers := runtime.NumCPU() * 2 // 不要这样做
```

---

## 三、C/C++ 规范（libvgpu）

### 3.1 头文件包含顺序（强制，ARM64 对齐安全）

```c
/* 顺序必须严格遵守，违反会导致 ARM64 对齐崩溃 */

/* 1. 系统头文件（按字母序）*/
#include <dlfcn.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* 2. 第三方头文件 */
#include <cuda.h>
#include <nvml.h>

/* 3. 本项目头文件 */
#include "libvgpu.h"
#include "cuda_hook.h"
```

### 3.2 内存对齐（强制）

```c
/* ✅ 共享内存结构体必须显式对齐，确保跨进程访问安全 */
typedef struct __attribute__((packed)) {
    uint32_t magic;
    uint32_t pid;
    int64_t  vram_allocated;   // 使用固定宽度类型
    uint8_t  _pad[64 - 16];   // 填充到 cache line 边界
} phoenix_shared_state_t;

/* ❌ 禁止：依赖编译器默认对齐（不同平台行为不同）*/
typedef struct {
    int flag;   // x86: 4 bytes, ARM64 with Wpadded: 可能不同
    long value;
} bad_struct_t;
```

### 3.3 C 层错误必须包装后传递给 Go

```c
/* ✅ 将 C 错误码转为字符串，由 Go 包装成 error 链 */
const char* phoenix_last_error(void) {
    return strerror(errno);
}

/* Go 侧 */
if ret := C.phoenix_dump(pid, dir); ret != 0 {
    return fmt.Errorf("criu dump: %s", C.GoString(C.phoenix_last_error()))
}
```

---

## 四、可观测性（强制，Sprint 2 起）

所有关键路径必须暴露 Prometheus 指标：

```go
// ✅ Checkpoint 操作必须有耗时指标
var (
    checkpointDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "phoenixgpu_checkpoint_duration_seconds",
            Help:    "Time taken to complete a CRIU checkpoint",
            Buckets: []float64{1, 5, 10, 30, 60, 120},
        },
        []string{"namespace", "job", "result"},
    )
    snapshotUploadBytes = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "phoenixgpu_snapshot_upload_bytes_total",
            Help: "Total bytes uploaded to snapshot storage",
        },
        []string{"backend", "namespace"},
    )
    restoreAttempts = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "phoenixgpu_restore_attempts_total",
            Help: "Total restore attempts by result",
        },
        []string{"result", "namespace"},
    )
)
```

**必须暴露指标的路径：**
- Checkpoint 耗时（直方图）
- Snapshot 上传速度和字节数
- 故障检测到恢复的端到端延迟
- VRAM 配额使用率（Gauge）
- 计费记录写入延迟

---

## 五、YAGNI 检查清单

在实现任何功能前，问自己：

- [ ] Sprint 计划中明确要求了这个功能吗？
- [ ] 有真实用户场景驱动这个需求吗？
- [ ] 如果不实现，当前 Sprint 的验收标准会失败吗？

如果三个都是"否"，**不要实现**。记录在 `docs/backlog.md` 中等待未来 Sprint。

---

## 六、Sprint 2 具体技术约束

基于头脑风暴决策和代码审查建议，Sprint 2（Snapshot Manager）的硬性约束：

1. **零磁盘二次拷贝**：使用 `io.Pipe` + S3 Multipart Upload，CRIU 文件直接流式上传
2. **固定 Worker Pool**：默认 4 个 Upload Worker，通过 `values.yaml` 配置，不做动态调整
3. **S3 超时隔离**：上传超时（默认 5 分钟）不能影响本地 Checkpoint 继续生成
4. **本地快照保留策略**：S3 上传失败时，本地快照保留（不删除），下次重传
5. **Prometheus 指标**：上传耗时、字节数、失败率必须暴露，无例外

---

*本规约版本：v0.2 — Sprint 1 后更新，引入可观测性和并发安全章节*
*下次更新：Sprint 2 完成后*
