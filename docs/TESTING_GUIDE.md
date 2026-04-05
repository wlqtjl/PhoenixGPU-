# PhoenixGPU 测试文档

> **版本**: v0.1.0 | **最后更新**: 2026-04-05 | **许可证**: Apache 2.0

---

## 目录

- [1. 基础环境介绍](#1-基础环境介绍)
  - [1.1 测试 GPU 服务器配置](#11-测试-gpu-服务器配置)
  - [1.2 软件环境要求](#12-软件环境要求)
  - [1.3 开发环境搭建](#13-开发环境搭建)
- [2. 测试概览](#2-测试概览)
  - [2.1 测试策略](#21-测试策略)
  - [2.2 测试分层](#22-测试分层)
  - [2.3 测试命令速查](#23-测试命令速查)
- [3. 单元测试](#3-单元测试)
  - [3.1 默认单元测试 (无标签)](#31-默认单元测试-无标签)
  - [3.2 带标签的单元测试](#32-带标签的单元测试)
  - [3.3 API Server 测试](#33-api-server-测试)
  - [3.4 计费引擎测试](#34-计费引擎测试)
  - [3.5 Checkpoint/CRIU 测试](#35-checkpointcriu-测试)
  - [3.6 HA 控制器测试](#36-ha-控制器测试)
  - [3.7 K8s 客户端测试](#37-k8s-客户端测试)
  - [3.8 GPU 热迁移测试](#38-gpu-热迁移测试)
  - [3.9 vGPU 超额分配测试](#39-vgpu-超额分配测试)
- [4. WebUI 测试](#4-webui-测试)
- [5. 端到端 (E2E) 测试](#5-端到端-e2e-测试)
  - [5.1 HA 恢复 E2E 测试](#51-ha-恢复-e2e-测试)
  - [5.2 运行条件](#52-运行条件)
  - [5.3 验收标准](#53-验收标准)
- [6. 集成测试](#6-集成测试)
  - [6.1 部署验证测试](#61-部署验证测试)
  - [6.2 API Server 质量测试](#62-api-server-质量测试)
- [7. 构建配置矩阵测试](#7-构建配置矩阵测试)
- [8. CI/CD 测试流水线](#8-cicd-测试流水线)
- [9. 手动功能验证](#9-手动功能验证)
  - [9.1 GPU 虚拟化验证](#91-gpu-虚拟化验证)
  - [9.2 自动 Checkpoint 验证](#92-自动-checkpoint-验证)
  - [9.3 故障恢复验证](#93-故障恢复验证)
  - [9.4 WebUI 验证](#94-webui-验证)
  - [9.5 计费验证](#95-计费验证)
- [10. 性能与基准测试](#10-性能与基准测试)
- [11. 测试编写规范](#11-测试编写规范)
- [12. 测试用例清单](#12-测试用例清单)

---

## 1. 基础环境介绍

### 1.1 测试 GPU 服务器配置

PhoenixGPU 测试环境使用以下 GPU 集群配置（定义于 `pkg/types/types.go`）：

| 节点 | GPU 型号 | GPU 数量 | 单卡显存 | 总显存 | 典型负载 |
|------|---------|---------|---------|--------|---------|
| `gpu-node-01` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM 82%, 380W, 72°C |
| `gpu-node-02` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM 65%, 310W, 68°C |
| `gpu-node-03` | NVIDIA A100 40GB | 8 | 40 GB | 40,960 MiB | SM 71%, 270W, 64°C |
| `gpu-node-04` | NVIDIA H800 | 8 | 80 GB | 81,920 MiB | SM 91%, 420W, 79°C |

**测试用 GPU 型号与计费参数**：

| GPU 型号 | FP16 TFlops | 单价 (CNY/h) | 用途 |
|---------|------------|-------------|------|
| NVIDIA H800 | 2000 | ¥55 | 大模型预训练 |
| NVIDIA A100 80GB | 312 | ¥35 | 标准训练 |
| NVIDIA A100 40GB | 312 | ¥22 | 中等规模 |
| NVIDIA RTX 4090 | 165 | ¥12 | 轻量级任务 |
| 华为 Ascend 910B | 256 | ¥28 | 国产替代 |

**测试用 PhoenixJob 样例数据**（3 个预置任务）：

| 任务名 | 部门 | 阶段 | Checkpoint 数 | GPU 型号 | 分配比例 | 运行时长 |
|--------|------|------|-------------|---------|---------|---------|
| `llm-pretrain-v3` | 算法研究院 | Running | 24 | H800 | 50% | 72h |
| `rlhf-finetune` | NLP平台组 | Checkpointing | 8 | A100 80GB | 25% | 24h |
| `cv-detection-v2` | CV工程组 | Restoring | 12 | A100 40GB | 50% | 48h |

**测试用部门计费数据**（5 个部门）：

| 部门 | GPU时 | TFlops·h | 费用 (CNY) | 配额 (h) | 使用率 |
|------|-------|---------|-----------|---------|--------|
| 算法研究院 | 620 | 193,440 | ¥21,700 | 800 | 77.5% |
| NLP平台组 | 480 | 149,760 | ¥16,800 | 600 | 80% |
| CV工程组 | 380 | 118,560 | ¥13,300 | 500 | 76% |
| 推理基础设施 | 280 | 87,360 | ¥9,800 | 400 | 70% |
| 数据工程部 | 150 | 24,750 | ¥1,800 | 300 | 50% |

### 1.2 软件环境要求

**测试运行环境**：

| 组件 | 最低版本 | 用途 |
|------|---------|------|
| Go | ≥ 1.22 (实际使用 1.24) | 编译和运行测试 |
| Linux | Ubuntu 20.04+ | CRIU E2E 测试 |
| CRIU | ≥ 3.17 | E2E Checkpoint/Restore |
| Node.js | ≥ 18 | WebUI 测试 |
| kubectl | 最新版 | 集成测试 |
| KinD | 最新版 | 本地集群测试 |
| Docker | 20.10+ | 容器构建和运行 |
| Helm | ≥ 3.x | Chart 校验 |

**CI 环境**（GitHub Actions `ubuntu-latest`）：
- 无需真实 GPU — 所有 CI 测试使用 `FakeK8sClient` 模拟数据
- CRIU 在 CI 中不可用 — E2E 测试会自动跳过
- GPU 集成测试在专门的 K8s 工作流中运行

### 1.3 开发环境搭建

```bash
# 一键初始化开发环境
bash hack/setup-dev.sh

# 该脚本会:
# 1. 检查 Go (≥1.22)          → 缺失则报错
# 2. 检查 kubectl              → 缺失则警告
# 3. 检查 CRIU                 → 缺失则警告 (E2E 测试需要)
# 4. 检查 cuda-checkpoint      → 缺失则警告 (可选)
# 5. 安装 Go 工具:
#    - golangci-lint@v1.57.2   (代码检查)
#    - controller-gen@v0.14.0  (CRD 代码生成)
# 6. 下载 Go 依赖              → go mod download
# 7. 运行快速测试               → go test ./... -short -count=1
```

---

## 2. 测试概览

### 2.1 测试策略

PhoenixGPU 采用以下测试策略：

- **TDD 驱动**：先写测试，后写实现
- **Go 标准库**：仅使用 `testing` 包，不依赖 testify 等第三方框架
- **表驱动测试**：大量使用 `[]struct{ name, input, expected }` 模式
- **Mock 注入**：通过接口注入 `FakeK8sClient`、`errorK8sClient` 等
- **Contract 测试**：存储层使用契约测试确保接口一致性
- **异步断言**：使用 `polling with deadline` 模式处理异步操作
- **构建标签隔离**：不同组件测试通过 build tag 隔离

### 2.2 测试分层

```
┌─────────────────────────────────────────────────────────────┐
│  L6: E2E 测试                                                │
│  ─ CRIU Checkpoint/Restore 全流程                            │
│  ─ 需要: Linux + CRIU + root 权限                            │
│  ─ 命令: make test-e2e                                       │
├─────────────────────────────────────────────────────────────┤
│  L5: 集成测试                                                │
│  ─ KinD 集群部署验证                                         │
│  ─ 需要: KinD + Docker + Helm                                │
│  ─ 命令: make validate / make validate-live                  │
├─────────────────────────────────────────────────────────────┤
│  L4: 基准测试                                                │
│  ─ API Server 性能基准                                       │
│  ─ 命令: make quality-api-server-bench                       │
├─────────────────────────────────────────────────────────────┤
│  L3: 竞态检测                                                │
│  ─ Go Race Detector (-race)                                  │
│  ─ 命令: make quality-api-server-race                        │
├─────────────────────────────────────────────────────────────┤
│  L2: 带标签单元测试                                          │
│  ─ 组件级单元测试 (checkpointfull, billingfull 等)           │
│  ─ 命令: go test -tags=<tag> ./pkg/<pkg>/...                 │
├─────────────────────────────────────────────────────────────┤
│  L1: 默认单元测试                                            │
│  ─ 无标签, 所有环境均可运行                                   │
│  ─ 命令: make test                                           │
├─────────────────────────────────────────────────────────────┤
│  L0: 静态分析                                                │
│  ─ golangci-lint, go vet, go fmt                             │
│  ─ 命令: make lint / make vet / make fmt                     │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 测试命令速查

| 命令 | 说明 | 环境要求 | 运行时间 |
|------|------|---------|---------|
| `make test` | 默认单元测试 (带 -race) | Go | ~30s |
| `make test-short` | 快速单元测试 | Go | ~10s |
| `make test-verbose` | 详细输出单元测试 | Go | ~30s |
| `make test-e2e` | E2E 测试 (30m 超时) | Linux + CRIU + root | ~5m |
| `make lint` | 代码检查 (golangci-lint) | Go + golangci-lint | ~60s |
| `make vet` | 静态检查 (go vet) | Go | ~10s |
| `make quality-api-server` | API Server 覆盖率 | Go | ~30s |
| `make quality-api-server-race` | API Server 竞态检测 | Go | ~30s |
| `make quality-api-server-bench` | API Server 基准测试 | Go | ~15s |
| `make validate` | Mock 模式部署验证 | Go | ~30s |
| `make validate-live` | 真实集群部署验证 | K8s + Helm | ~5m |
| `make validate-api` | API Go 测试验证 | Go | ~30s |
| `cd webui && npx vitest run` | WebUI 测试 | Node.js | ~15s |
| `make helm-lint` | Helm Chart 校验 | Helm | ~5s |

---

## 3. 单元测试

### 3.1 默认单元测试 (无标签)

默认 `make test` 运行所有无构建标签的测试：

```bash
# 运行所有默认测试（带竞态检测）
make test
# 等价于: go test ./... -race -coverprofile=coverage.out -covermode=atomic -timeout=5m

# 快速模式（跳过耗时测试）
make test-short

# 查看覆盖率报告
go tool cover -html=coverage.out -o coverage.html
```

**测试范围**：
- `cmd/api-server/api_test.go` — API 路由和响应测试
- `pkg/vgpu/oversub_test.go` — vGPU 超额分配算法测试
- `pkg/types/` — 域对象和 FakeK8sClient 测试
- `pkg/util/` — 工具函数测试
- `test/validation/` — 部署验证测试

### 3.2 带标签的单元测试

组件级测试需要指定构建标签：

```bash
# Checkpoint/CRIU 测试
go test -tags=checkpointfull ./pkg/checkpoint/... -v

# 计费引擎测试
go test -tags=billingfull ./pkg/billing/... -v

# K8s 客户端测试
go test -tags=k8sfull ./pkg/k8s/... -v

# HA 控制器测试
go test -tags=controllerfull ./pkg/hacontroller/... -v

# GPU 热迁移测试
go test -tags=migrationfull ./pkg/migration/... -v
```

### 3.3 API Server 测试

```bash
# 基础测试
go test ./cmd/api-server/... -v

# 覆盖率测试
make quality-api-server

# 竞态检测
make quality-api-server-race

# 基准测试
make quality-api-server-bench
```

**测试用例**（`cmd/api-server/api_test.go`）：

| 测试 | 说明 |
|------|------|
| `TestClusterSummaryEndpoint` | 验证集群概览 API 返回正确数据 |
| `TestGPUNodesEndpoint` | 验证 GPU 节点列表 API |
| `TestPhoenixJobsEndpoint` | 验证 PhoenixJob 列表 API |
| `TestBillingEndpoint` | 验证计费 API |
| `TestHealthzEndpoint` | 验证健康检查端点 |
| `TestReadyzEndpoint` | 验证就绪检查端点 |
| `TestErrorHandling` | 验证 K8s 客户端错误处理 |

**Mock 注入示例**：

```go
// 使用 FakeK8sClient 测试正常路径
client := types.NewFakeK8sClient()
router := setupRouter(client)
// ...发送请求并验证响应

// 使用 errorK8sClient 测试错误路径
client := &errorK8sClient{err: fmt.Errorf("connection refused")}
router := setupRouter(client)
// ...验证返回 500 + 错误消息
```

### 3.4 计费引擎测试

```bash
go test -tags=billingfull ./pkg/billing/... -v
```

**测试覆盖**：

| 测试场景 | 说明 |
|---------|------|
| TFlops·h 计算 | 验证 `AllocRatio × FP16TFlops × Duration` 公式 |
| 成本计算 | 验证 CNY 成本 = `AllocRatio × PricePerHour × Duration` |
| 多 GPU 型号 | 验证 H800/A100/4090/910B 的计算正确性 |
| 边界条件 | AllocRatio=0, Duration=0, 未知 GPU 型号 |
| UsageRecord 批量处理 | 验证批量记录写入 |
| 配额检查 | 验证超额告警触发 |

### 3.5 Checkpoint/CRIU 测试

```bash
go test -tags=checkpointfull ./pkg/checkpoint/... -v
```

**测试覆盖**：

| 测试场景 | 说明 |
|---------|------|
| CRIUCheckpointer 创建 | 验证快照目录创建 |
| Dump/Restore 生命周期 | 验证序列化/反序列化 |
| PreDump 增量转储 | 验证预转储减少冻结窗口 |
| 快照存储契约测试 | PVC/S3 后端的统一接口验证 |
| Prometheus 指标 | 验证 dump_duration, dump_total 等指标注册和更新 |
| 错误处理 | CRIU 二进制缺失、权限不足、超时 |

**存储契约测试**（`pkg/checkpoint/storage_contract_test.go`）：

```go
// 契约测试确保 PVC 和 S3 后端实现一致的接口行为
func runStorageContractTests(t *testing.T, store SnapshotStore) {
    t.Run("Save", func(t *testing.T) { ... })
    t.Run("Load", func(t *testing.T) { ... })
    t.Run("List", func(t *testing.T) { ... })
    t.Run("Delete", func(t *testing.T) { ... })
    t.Run("Prune", func(t *testing.T) { ... })
}
```

### 3.6 HA 控制器测试

```bash
go test -tags=controllerfull ./pkg/hacontroller/... -v
```

**测试覆盖**：

| 测试场景 | 说明 |
|---------|------|
| FaultDetector 启动/停止 | 验证生命周期管理 |
| 故障检测 | 模拟节点 NotReady → 触发 FaultEvent |
| 恢复编排 | HandleNodeFault → 创建新 Pod → Restore |
| 重试限制 | 超过 maxRestoreAttempts → 标记 Failed |
| 超时处理 | 恢复超过 restoreTimeoutSeconds → 重试/失败 |

### 3.7 K8s 客户端测试

```bash
go test -tags=k8sfull ./pkg/k8s/... -v
```

**测试覆盖**：

| 测试场景 | 说明 |
|---------|------|
| 集群概览查询 | 验证 GetClusterSummary 聚合逻辑 |
| GPU 节点列表 | 验证 ListGPUNodes 数据映射 |
| PhoenixJob CRUD | 验证 CRD 操作 |
| 计费查询 | 验证 BillingQuerier 接口和回退逻辑 |
| Prometheus 指标解析 | 验证 prom_parser.go 的指标文本解析 |

### 3.8 GPU 热迁移测试

```bash
go test -tags=migrationfull ./pkg/migration/... -v
```

### 3.9 vGPU 超额分配测试

```bash
go test ./pkg/vgpu/... -v
```

**测试覆盖**（`pkg/vgpu/oversub_test.go`）：

| 测试场景 | 说明 |
|---------|------|
| 显存分配计算 | 验证 VRAM 配额分配和限制 |
| SM 节流计算 | 验证 SM 利用率软节流逻辑 |
| 边界条件 | 0% 分配、100% 分配、超额请求 |

---

## 4. WebUI 测试

```bash
cd webui
npx vitest run          # 运行测试
npx vitest run --watch  # 监视模式
npx vitest run --coverage  # 覆盖率
```

**测试范围**：
- 组件渲染测试
- API 客户端 Mock 测试
- 路由测试

---

## 5. 端到端 (E2E) 测试

### 5.1 HA 恢复 E2E 测试

**文件**: `test/e2e/ha_restore_test.go`

这是 PhoenixGPU 最关键的 E2E 测试，验证 GPU 故障自动恢复的完整流程：

```bash
# 运行 E2E 测试
make test-e2e
# 等价于: go test ./test/e2e/... -v -tags e2e -timeout=30m
```

**测试流程** (`TestHARestore_TrainingContinuesAfterFault`)：

```
步骤 1: 编译并启动合成 "训练" 进程
         → 每 100ms 写入递增计数器到文件
         → 模拟 GPU 训练的 step 积累

步骤 2: 等待 step ≥ 50 (最多 30s)
         → 确保训练进程有足够进度

步骤 3: CRIU Checkpoint
         → 调用 CRIUCheckpointer.Dump(pid, snapDir)
         → 记录 Checkpoint 耗时

步骤 4: Kill 进程（模拟节点故障）
         → trainCmd.Process.Kill()
         → 记录故障前 step 值

步骤 5: CRIU Restore
         → 调用 CRIUCheckpointer.Restore(snapDir)
         → 记录恢复耗时

步骤 6: 验证验收标准
         → 等待 2 秒让恢复进程写入更多 steps
         → 检查 3 个验收标准
```

**辅助测试** (`TestHARestore_FaultDetectorToRestorePipeline`)：

- 无需 CRIU，在标准 CI 中运行
- 验证 FaultEvent → HandleNodeFault → Restore 管线连线正确
- 使用 Mock 组件

### 5.2 运行条件

| 条件 | 说明 | 检查方式 |
|------|------|---------|
| Linux 操作系统 | CRIU 仅支持 Linux | `runtime.GOOS == "linux"` |
| CRIU 已安装 | Checkpoint/Restore 核心依赖 | `exec.LookPath("criu")` |
| root 或 CAP_SYS_PTRACE | CRIU 需要 ptrace 权限 | 运行时检查 |

> 不满足条件时测试会 `t.Skip()` 而非失败。

### 5.3 验收标准

| 编号 | 标准 | 阈值 | 验证方式 |
|------|------|------|---------|
| AC1 | 训练不回退 | step_after ≥ step_before | `stepAfterRestore >= stepBeforeCheckpoint` |
| AC2 | 恢复时间 SLA | < 60 秒 | `restoreTime < 60s` |
| AC3 | Checkpoint 耗时预算 | < 30 秒 | `ckptDuration < 30s` |

**示例输出**：

```
=== RUN   TestHARestore_TrainingContinuesAfterFault
    training process started: pid=12345
    waiting for training step >= 50...
    step reached 52 — triggering checkpoint
    running CRIU checkpoint...
    checkpoint completed in 1.2s
    killing training process (simulating node fault)...
    process killed at step 67
    restoring from checkpoint...
    process restored: pid=12346 in 3.5s
    --- Acceptance Criteria Results ---
    Step before checkpoint: 52
    Step after restore:     72
    Recovery time:          3.5s
    Checkpoint duration:    1.2s
    PASS AC1: training continues from step 72
    PASS AC2: recovery time 3.5s < 60s
    PASS AC3: checkpoint duration 1.2s
--- PASS: TestHARestore_TrainingContinuesAfterFault (35.7s)
```

---

## 6. 集成测试

### 6.1 部署验证测试

```bash
# Mock 模式 (无需集群)
make validate
# 等价于: go test ./test/validation/... -v

# 真实集群模式
make validate-live

# 仅 API Go 测试
make validate-api
```

**验证测试说明**：

`test/validation/` 目录下的测试使用自包含的 Mock Router（基于 `pkg/types.FakeK8sClient`），验证 API 端点的正确性。

> **注意**: Go 内部包 (`cmd/*/internal/`) 无法从 `test/` 目录导入。因此验证测试在 `test/validation/helpers_test.go` 中重建了一个独立的 Mock Router。

**验证项**：

| 测试 | 说明 |
|------|------|
| 集群概览 | GET /api/v1/cluster/summary 返回正确结构 |
| GPU 节点列表 | GET /api/v1/nodes 返回 4 个节点 |
| 任务列表 | GET /api/v1/jobs 返回 3 个任务 |
| 计费查询 | GET /api/v1/billing/departments 返回 5 个部门 |
| 健康检查 | GET /healthz 返回 200 |
| 就绪检查 | GET /readyz 返回 200 |

### 6.2 API Server 质量测试

```bash
# 覆盖率基线
make quality-api-server
# 运行 cmd/api-server 测试并输出覆盖率

# 竞态检测
make quality-api-server-race
# 使用 -race 标志检测数据竞争

# 基准冒烟测试
make quality-api-server-bench
# 运行 Benchmark 函数，验证性能基线
```

---

## 7. 构建配置矩阵测试

使用 `hack/profile-matrix.sh` 验证所有构建标签组合均可编译和通过测试：

```bash
# 列出所有配置
bash hack/profile-matrix.sh --list
# 输出:
# default      (无标签)
# k8sfull      (真实 K8s 客户端)
# checkpointfull
# migrationfull
# billingfull
# controllerfull
# 各种组合...

# 运行所有配置
bash hack/profile-matrix.sh

# 运行特定配置
bash hack/profile-matrix.sh billingfull
```

**每个配置执行**：
```bash
go build -tags=<profile> ./...    # 编译检查
go test -tags=<profile> ./...     # 单元测试
```

---

## 8. CI/CD 测试流水线

### GitHub Actions 工作流

**1. API Server 质量流水线** (`.github/workflows/api-server-quality.yml`)

| 阶段 | 说明 | 运行环境 |
|------|------|---------|
| L1 | 静态分析 (go vet, golangci-lint) | ubuntu-latest |
| L2 | 单元测试 | ubuntu-latest |
| L3 | 竞态检测 (-race) | ubuntu-latest |
| L4 | 基准冒烟测试 | ubuntu-latest |
| L5 | 集成测试 (需 KinD) | 单独工作流 |
| L6 | E2E 测试 (需 CRIU + GPU) | 单独工作流 |

**2. 静态分析流水线** (`.github/workflows/static.yml`)

| 检查项 | 工具 |
|--------|------|
| Go 格式 | go fmt |
| Go Vet | go vet |
| Lint | golangci-lint |
| Helm Lint | helm lint |

### CI 环境特点

- **无 GPU**: 所有 CI 运行在 `ubuntu-latest`，无 NVIDIA GPU
- **使用 FakeK8sClient**: API 测试使用模拟数据
- **CRIU 不可用**: E2E 测试自动 Skip
- **推荐**: GPU 集成测试在专用 K8s 集群中运行（独立工作流）

---

## 9. 手动功能验证

以下测试用于在真实或 KinD 集群环境中进行手动功能验证。

### 9.1 GPU 虚拟化验证

```bash
# 1. 创建 GPU 测试任务
cat <<EOF | kubectl apply -f -
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: gpu-test
  namespace: default
spec:
  checkpoint:
    storageBackend: pvc
    pvcName: phoenix-snapshots
    intervalSeconds: 60
  template:
    spec:
      containers:
        - name: training
          image: pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime
          command: ["python", "-c", "import torch; print(torch.cuda.get_device_name(0)); x=torch.randn(1000,1000,device='cuda'); print('Success:', x.shape)"]
          resources:
            limits:
              nvidia.com/vgpu: "1"
              nvidia.com/vgpu-memory: "4096"
EOF

# 2. 验证 Pod 运行
kubectl get pods -l phoenixjob=gpu-test
# STATUS 应为 Running 或 Completed

# 3. 检查 GPU 可见性
kubectl logs <pod-name>
# 应显示: NVIDIA A100 80GB (或实际 GPU 型号)
# 应显示: Success: torch.Size([1000, 1000])
```

### 9.2 自动 Checkpoint 验证

```bash
# 1. 提交持续运行的训练任务
cat <<EOF | kubectl apply -f -
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: ckpt-test
spec:
  checkpoint:
    storageBackend: pvc
    pvcName: phoenix-snapshots
    intervalSeconds: 60       # 1 分钟间隔（测试用）
    maxSnapshots: 3
  template:
    spec:
      containers:
        - name: training
          image: pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime
          command: ["python", "-c", "import time; i=0;\nwhile True: i+=1; print(f'step {i}'); time.sleep(1)"]
          resources:
            limits:
              nvidia.com/vgpu: "1"
EOF

# 2. 等待 70 秒后检查 Checkpoint
sleep 70
kubectl get phoenixjobs ckpt-test -o jsonpath='{.status.checkpointCount}'
# 应返回 ≥ 1

# 3. 检查 Checkpoint 详情
kubectl get phoenixjobs ckpt-test -o yaml | grep -A5 status
# 应看到:
#   phase: Running (或 Checkpointing)
#   checkpointCount: 1
#   lastCheckpointTime: 2026-04-05T03:XX:XXZ
#   lastCheckpointDir: /snapshots/ckpt-test/...
```

### 9.3 故障恢复验证

**这是 PhoenixGPU 核心功能验证**：

```bash
# 1. 确认任务运行节点
NODE=$(kubectl get phoenixjobs ckpt-test -o jsonpath='{.status.currentNodeName}')
echo "任务运行在: $NODE"

# 2. 模拟节点故障 (排空节点)
kubectl cordon $NODE
kubectl drain $NODE --ignore-daemonsets --delete-emptydir-data --force

# 3. 观察恢复过程 (新终端)
watch kubectl get phoenixjobs ckpt-test
# 预期变化:
# Phase: Running → Restoring → Running (在 60s 内)

# 4. 验证恢复结果
NEW_NODE=$(kubectl get phoenixjobs ckpt-test -o jsonpath='{.status.currentNodeName}')
echo "任务恢复到: $NEW_NODE (应不同于 $NODE)"

RESTORE_ATTEMPTS=$(kubectl get phoenixjobs ckpt-test -o jsonpath='{.status.restoreAttempts}')
echo "恢复尝试次数: $RESTORE_ATTEMPTS"

# 5. 恢复节点
kubectl uncordon $NODE
```

**验收标准**：
- ✅ Phase 在 60 秒内从 `Restoring` 变回 `Running`
- ✅ 任务迁移到不同节点
- ✅ 训练 step 从 Checkpoint 处继续，不从 0 开始

### 9.4 WebUI 验证

```bash
# 方式 1: kubectl port-forward
kubectl port-forward -n phoenixgpu-system svc/phoenixgpu-webui 3000:80
# 打开 http://localhost:3000

# 方式 2: KinD 环境 (自动端口映射)
# 直接打开 http://localhost:3000
```

**验证项**：

| 页面 | 验证点 |
|------|--------|
| 集群概览 | 显示 32 GPU, 18 活跃任务, 74.2% 利用率 |
| GPU 节点 | 显示 4 个节点及其状态 |
| 任务列表 | 显示 3 个预置任务 (Running/Checkpointing/Restoring) |
| 计费报表 | 显示 5 个部门的 GPU 使用量和费用 |
| 告警中心 | 显示 3 条告警 |

### 9.5 计费验证

```bash
# 查询部门计费
curl http://localhost:8090/api/v1/billing/departments | jq .
# 应返回 5 个部门的计费数据

# 查询计费明细
curl http://localhost:8090/api/v1/billing/records | jq .

# 数据库直接查询 (如使用外部数据库)
psql $BILLING_DB_DSN -c "SELECT * FROM current_month_billing;"
psql $BILLING_DB_DSN -c "SELECT * FROM quota_utilization;"
```

---

## 10. 性能与基准测试

### API Server 基准

```bash
make quality-api-server-bench
# 运行 Benchmark* 函数
```

### 性能 SLA 验证

| 指标 | SLA 目标 | 测试方法 |
|------|---------|---------|
| GPU 故障恢复时间 | < 60s | E2E 测试 AC2 |
| Checkpoint 耗时 | < 30s | E2E 测试 AC3 |
| Checkpoint 吞吐影响 | < 5% | 训练吞吐基准对比 |
| 进程冻结窗口 | < 5s | PreDump 模式下的 Dump 耗时 |
| 故障检测时间 | < 30s | FaultDetector 单元测试 |
| API 响应时间 | < 100ms (P99) | 基准测试 |

---

## 11. 测试编写规范

### 规范一览

| 规范 | 说明 |
|------|------|
| 测试框架 | 仅使用 Go 标准库 `testing` 包 |
| 断言方式 | 直接 `if/t.Errorf/t.Fatalf`，不使用 testify |
| 测试命名 | `Test<Function>_<Scenario>`，如 `TestDump_TimeoutExceeded` |
| 表驱动测试 | 大量使用 `[]struct{name string; ...}` + `t.Run(tc.name)` |
| Mock 注入 | 通过接口注入，不使用 monkey patch |
| 异步测试 | 使用 polling + deadline 模式 |
| 构建标签 | 组件测试文件首行: `//go:build <tag>` |
| 超时 | 默认 5 分钟，E2E 30 分钟 |

### 表驱动测试示例

```go
func TestCalculateTFlopsH(t *testing.T) {
    tests := []struct {
        name       string
        gpuModel   string
        allocRatio float64
        hours      float64
        want       float64
    }{
        {"A100 50% 2h", "NVIDIA-A100-80GB", 0.5, 2.0, 312.0},
        {"H800 25% 1h", "NVIDIA-H800", 0.25, 1.0, 500.0},
        {"zero alloc", "NVIDIA-A100-80GB", 0.0, 2.0, 0.0},
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := calculateTFlopsH(tc.gpuModel, tc.allocRatio, tc.hours)
            if got != tc.want {
                t.Errorf("got %f, want %f", got, tc.want)
            }
        })
    }
}
```

### Polling + Deadline 异步断言示例

```go
func TestAsyncOperation(t *testing.T) {
    deadline := time.Now().Add(10 * time.Second)
    for time.Now().Before(deadline) {
        result := checkCondition()
        if result {
            return // PASS
        }
        time.Sleep(200 * time.Millisecond)
    }
    t.Fatal("condition not met within deadline")
}
```

---

## 12. 测试用例清单

### 单元测试用例

| 编号 | 组件 | 用例 | 构建标签 | 级别 |
|------|------|------|---------|------|
| UT-001 | API Server | 集群概览端点返回正确数据 | 无 | L1 |
| UT-002 | API Server | GPU 节点列表端点 | 无 | L1 |
| UT-003 | API Server | PhoenixJob 列表端点 | 无 | L1 |
| UT-004 | API Server | 计费端点 | 无 | L1 |
| UT-005 | API Server | 健康/就绪检查 | 无 | L1 |
| UT-006 | API Server | K8s 客户端错误处理 | 无 | L1 |
| UT-007 | API Server | 认证 Token 验证 | 无 | L1 |
| UT-008 | API Server | 速率限制 | 无 | L1 |
| UT-009 | vGPU | VRAM 配额计算 | 无 | L1 |
| UT-010 | vGPU | SM 节流计算 | 无 | L1 |
| UT-011 | vGPU | 边界条件 (0%, 100%, 超额) | 无 | L1 |
| UT-012 | 计费 | TFlops·h 计算公式 | billingfull | L2 |
| UT-013 | 计费 | CNY 成本计算 | billingfull | L2 |
| UT-014 | 计费 | 多 GPU 型号正确性 | billingfull | L2 |
| UT-015 | 计费 | 未知 GPU 型号回退 | billingfull | L2 |
| UT-016 | 计费 | UsageRecord 批量处理 | billingfull | L2 |
| UT-017 | Checkpoint | CRIUCheckpointer 创建 | checkpointfull | L2 |
| UT-018 | Checkpoint | Dump 序列化 | checkpointfull | L2 |
| UT-019 | Checkpoint | Restore 反序列化 | checkpointfull | L2 |
| UT-020 | Checkpoint | PreDump 增量转储 | checkpointfull | L2 |
| UT-021 | Checkpoint | Prometheus 指标注册 | checkpointfull | L2 |
| UT-022 | Checkpoint | 存储契约测试 (PVC) | checkpointfull | L2 |
| UT-023 | Checkpoint | 存储契约测试 (S3) | checkpointfull | L2 |
| UT-024 | HA Controller | FaultDetector 生命周期 | controllerfull | L2 |
| UT-025 | HA Controller | 故障检测触发 | controllerfull | L2 |
| UT-026 | HA Controller | 恢复编排 | controllerfull | L2 |
| UT-027 | HA Controller | 重试限制 | controllerfull | L2 |
| UT-028 | K8s | 集群概览查询 | k8sfull | L2 |
| UT-029 | K8s | GPU 节点列表 | k8sfull | L2 |
| UT-030 | K8s | BillingQuerier 接口 | k8sfull | L2 |
| UT-031 | K8s | Prometheus 指标解析 | k8sfull | L2 |
| UT-032 | 热迁移 | 迁移执行器 | migrationfull | L2 |

### 集成测试用例

| 编号 | 场景 | 命令 | 级别 |
|------|------|------|------|
| IT-001 | Mock 模式部署验证 | `make validate` | L5 |
| IT-002 | 真实集群部署验证 | `make validate-live` | L5 |
| IT-003 | Helm Chart 校验 | `make helm-lint` | L5 |
| IT-004 | 构建配置矩阵 | `bash hack/profile-matrix.sh` | L5 |
| IT-005 | API 竞态检测 | `make quality-api-server-race` | L3 |
| IT-006 | API 基准冒烟 | `make quality-api-server-bench` | L4 |

### E2E 测试用例

| 编号 | 场景 | 验收标准 | 级别 |
|------|------|---------|------|
| E2E-001 | CRIU Checkpoint + Kill + Restore | AC1: 不回退, AC2: <60s, AC3: <30s | L6 |
| E2E-002 | FaultDetector→Restore 管线 | 管线连线正确 | L6 (无 CRIU) |

### 手动验证用例

| 编号 | 场景 | 预期结果 |
|------|------|---------|
| MV-001 | GPU 虚拟化 | Pod 可见 GPU 并执行 CUDA 计算 |
| MV-002 | 自动 Checkpoint | 60s 后 checkpointCount ≥ 1 |
| MV-003 | 故障恢复 | 任务在 60s 内恢复到新节点 |
| MV-004 | WebUI 集群概览 | 显示正确的集群统计数据 |
| MV-005 | WebUI GPU 节点 | 显示 4 个节点的详细信息 |
| MV-006 | WebUI 任务管理 | 显示 3 个预置任务 |
| MV-007 | WebUI 计费报表 | 显示 5 个部门的使用数据 |
| MV-008 | API 健康检查 | /healthz 返回 200 |
| MV-009 | 部门计费查询 | 返回正确的 TFlops·h 和 CNY |
| MV-010 | 手动触发 Checkpoint | POST 请求成功触发 |
