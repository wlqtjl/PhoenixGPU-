# Sprint 2 计划书 — Snapshot Manager

**周期**: 月2 第1-2周  
**目标**: 节点故障后能自动从 PVC 或 S3 恢复训练（端到端可跑通）  
**验收标准**: 模拟节点宕机 → 训练在 60s 内在新节点从断点恢复，loss 不回退

---

## 架构决策（基于工程规约 v0.2）

```
CRIU Watcher
    │  (文件路径 + 元数据)
    ▼
Task Channel (buffered=64)
    │
    ├── Worker 1 ──→ io.Pipe ──→ S3 Multipart Upload
    ├── Worker 2 ──→ io.Pipe ──→ S3 Multipart Upload
    ├── Worker 3 ──→ io.Pipe ──→ S3 Multipart Upload  (固定4个，配置可调)
    └── Worker 4 ──→ io.Pipe ──→ S3 Multipart Upload
         │
         └── 失败? → 保留本地快照，下次 Checkpoint 时重传
                   → 记录 Prometheus 失败计数

上传超时 Context (默认5min) 与 本地Checkpoint 完全隔离
```

---

## 任务列表

### T16 — StorageBackend 接口定义 (2h)
**文件**: `pkg/checkpoint/storage.go`  
**产出**:
```go
type StorageBackend interface {
    Save(ctx context.Context, src string, meta SnapshotMeta) error
    Load(ctx context.Context, meta SnapshotMeta, dst string) error
    List(ctx context.Context, jobKey string) ([]SnapshotMeta, error)
    Delete(ctx context.Context, meta SnapshotMeta) error
    Prune(ctx context.Context, jobKey string, keep int) error
}
```
**测试**: 接口合规测试（MockBackend 实现）

### T17 — LocalPVC Backend (3h)
**文件**: `pkg/checkpoint/storage_pvc.go`  
**产出**: `Save`=文件复制、`Load`=文件复制、`Prune`=按 seq 删除最老快照  
**测试**: 使用 `os.TempDir()` 测试 Save/Load/Prune 完整循环

### T18 — S3 Backend with io.Pipe (6h)
**文件**: `pkg/checkpoint/storage_s3.go`  
**核心**: `io.Pipe` + AWS SDK v2 Multipart Upload，零磁盘二次拷贝  
**测试**: 使用 MinIO Docker 做集成测试（`//go:build integration`）

### T19 — Upload Worker Pool (4h)
**文件**: `pkg/checkpoint/uploader.go`  
**核心**: 带缓冲 Channel(64) + 固定 N Worker + Context 超时隔离  
**Prometheus 指标**:
- `phoenixgpu_snapshot_upload_bytes_total`
- `phoenixgpu_snapshot_upload_duration_seconds`
- `phoenixgpu_snapshot_upload_failures_total`

### T20 — Prune 策略 (2h)
**文件**: `pkg/checkpoint/pruner.go`  
**逻辑**: 保留最新 N 个快照（N 从 PhoenixJob spec 读取，默认 5）  
**测试**: TDD，验证超过 N 个时最老的被删除

### T21 — E2E 故障恢复测试 (8h) — 关键路径
**文件**: `test/e2e/ha_restore_test.go`  
**验收标准（必须全部通过）**:
1. 启动合成训练进程（写递增计数器到文件，代表训练 step）
2. 等待计数器到 50，触发 Checkpoint
3. kill -9 进程（模拟节点宕机）
4. 调用 Restore
5. 等待进程恢复，读取计数器文件
6. 断言：计数器 >= 50（不能从 0 重新开始）
7. 断言：恢复耗时 < 60s

### T22 — 代码审查（两阶段）(3h)
阶段一：对照工程规约逐条检查  
阶段二：并发安全（Channel 无死锁）、资源泄漏（goroutine 全部可退出）

---

## Sprint 2 总工时: 28h

---

## I/O 容量规划参考（文档用，不驱动实现）

$$T_{total} = \min\left( R_{disk},\ \frac{BW_{net}}{C_{overhead}},\ N \cdot S_{s3} \right)$$

- $R_{disk}$: 本地顺序读（SSD ~2GB/s，HDD ~200MB/s）
- $BW_{net}$: 节点出向带宽（万兆 ~1.25GB/s）
- $C_{overhead}$: TLS + Multipart 头部（约 1.05×）
- $N$: Worker 数量（默认 4）
- $S_{s3}$: 单连接 S3 吞吐（约 100-200MB/s）

**典型场景**: 4 Worker × 100MB/s = 400MB/s，受网络带宽限制在 1.25GB/s 以内，瓶颈通常在 $R_{disk}$（HDD 场景）或 $S_{s3}$（高速 NVMe + 慢 S3 场景）。

---

*开始 T16？回复"T16 开始"*
