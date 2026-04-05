# PhoenixGPU 实施文档

> **版本**: v0.1.0 | **最后更新**: 2026-04-05 | **许可证**: Apache 2.0

---

## 目录

- [1. 基础环境介绍](#1-基础环境介绍)
- [2. 系统架构概览](#2-系统架构概览)
  - [2.1 三层架构](#21-三层架构)
  - [2.2 组件列表](#22-组件列表)
  - [2.3 项目目录结构](#23-项目目录结构)
- [3. 核心组件详解](#3-核心组件详解)
  - [3.1 Phoenix HA Controller](#31-phoenix-ha-controller)
  - [3.2 Device Plugin](#32-device-plugin)
  - [3.3 Scheduler Extender](#33-scheduler-extender)
  - [3.4 MutatingWebhook](#34-mutatingwebhook)
  - [3.5 Billing Engine](#35-billing-engine)
  - [3.6 API Server](#36-api-server)
  - [3.7 WebUI](#37-webui)
  - [3.8 libvgpu.so (CUDA 拦截层)](#38-libvgpuso-cuda-拦截层)
- [4. 数据模型](#4-数据模型)
  - [4.1 PhoenixJob CRD](#41-phoenixjob-crd)
  - [4.2 计费数据库 Schema](#42-计费数据库-schema)
  - [4.3 域对象模型](#43-域对象模型)
- [5. 关键流程](#5-关键流程)
  - [5.1 GPU 故障自动恢复流程](#51-gpu-故障自动恢复流程)
  - [5.2 Checkpoint 流程](#52-checkpoint-流程)
  - [5.3 计费采集流程](#53-计费采集流程)
  - [5.4 调度决策流程](#54-调度决策流程)
- [6. 构建系统](#6-构建系统)
  - [6.1 构建标签 (Build Tags)](#61-构建标签-build-tags)
  - [6.2 Makefile 命令](#62-makefile-命令)
  - [6.3 Docker 镜像构建](#63-docker-镜像构建)
- [7. Helm Chart 配置](#7-helm-chart-配置)
- [8. API 接口](#8-api-接口)
- [9. 可观测性](#9-可观测性)
- [10. 安全机制](#10-安全机制)
- [11. 架构决策记录 (ADR)](#11-架构决策记录-adr)
- [12. 性能指标与 SLA](#12-性能指标与-sla)

---

## 1. 基础环境介绍

### 测试 GPU 服务器配置

| 节点 | GPU 型号 | GPU 数量 | 单卡显存 | 总显存 | 典型负载 |
|------|---------|---------|---------|--------|---------|
| `gpu-node-01` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM 82%, 380W, 72°C |
| `gpu-node-02` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM 65%, 310W, 68°C |
| `gpu-node-03` | NVIDIA A100 40GB | 8 | 40 GB | 40,960 MiB | SM 71%, 270W, 64°C |
| `gpu-node-04` | NVIDIA H800 | 8 | 80 GB | 81,920 MiB | SM 91%, 420W, 79°C |

### 软件环境

| 组件 | 版本 | 用途 |
|------|------|------|
| Linux | Ubuntu 20.04+ / CentOS 7+ | 操作系统（CRIU 仅支持 Linux） |
| NVIDIA GPU 驱动 | ≥ 525 | GPU 硬件驱动 |
| CUDA Toolkit | 12.x | GPU 编程运行时 |
| NVIDIA Container Toolkit | ≥ 1.13 | 容器中访问 GPU |
| Kubernetes | ≥ 1.26 | 容器编排平台 |
| Helm | ≥ 3.x | Kubernetes 包管理 |
| CRIU | ≥ 3.17 | 进程检查点/恢复 |
| Go | ≥ 1.22（实际使用 1.24） | 后端编译语言 |
| Node.js | ≥ 18 | WebUI 构建 |
| React | 18 | WebUI 前端框架 |
| TypeScript | - | WebUI 类型安全 |

### 主要依赖库

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/gin-gonic/gin` | v1.9.1 | HTTP 路由框架 |
| `github.com/lib/pq` | v1.10.9 | PostgreSQL 驱动 |
| `github.com/prometheus/client_golang` | v1.19.0 | Prometheus 指标 |
| `github.com/spf13/cobra` | v1.8.0 | CLI 框架 |
| `go.uber.org/zap` | v1.27.0 | 结构化日志 |
| `k8s.io/client-go` | v0.29.3 | Kubernetes 客户端 |
| `sigs.k8s.io/controller-runtime` | v0.17.2 | 控制器框架 |
| `github.com/aws/aws-sdk-go-v2` | v1.41.5 | S3 存储后端 |

---

## 2. 系统架构概览

### 2.1 三层架构

```
┌─────────────────────────────────────────────────────────────┐
│                        用户层 (User Layer)                    │
│  ┌──────────────────┐  ┌──────────────────────────────────┐ │
│  │  kubectl apply   │  │  WebUI Dashboard (React 18)      │ │
│  │  PhoenixJob.yaml │  │  http://localhost:3000            │ │
│  └──────────────────┘  └──────────────────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                      控制平面 (Control Plane)                 │
│  ┌──────────────┐ ┌──────────────┐ ┌────────────────────┐  │
│  │ Phoenix HA   │ │  Scheduler   │ │  Billing Engine    │  │
│  │ Controller   │ │  Extender    │ │  (TFlops·h 计量)    │  │
│  │ ・FaultDetect │ │  ・binpack   │ │  ・采集 UsageRecord │  │
│  │ ・CkptSchedul │ │  ・spread    │ │  ・配额管理         │  │
│  │ ・RestoreEng  │ │  ・NUMA感知   │ │  ・告警触发         │  │
│  └──────────────┘ └──────────────┘ └────────────────────┘  │
│  ┌──────────────┐ ┌──────────────┐ ┌────────────────────┐  │
│  │ API Server   │ │   Webhook    │ │  Snapshot Manager  │  │
│  │  REST API    │ │ LD_PRELOAD   │ │  PVC/S3/NFS 后端    │  │
│  │  :8090       │ │  注入         │ │  快照裁剪           │  │
│  └──────────────┘ └──────────────┘ └────────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                       节点平面 (Node Plane)                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Device Plugin DaemonSet (vGPU 资源注册)              │   │
│  │  libvgpu.so (CUDA 拦截层)                             │   │
│  │  ・VRAM 配额硬隔离  ・SM 占用率软节流                    │   │
│  │  ・TFlops 计量      ・NVML 虚拟化                      │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 组件列表

| 组件 | 类型 | 说明 | 构建标签 | 端口 |
|------|------|------|---------|------|
| `api-server` | Deployment | REST API + WebUI 后端 | (默认) | 8090, 8091 |
| `phoenix-controller` | Deployment | HA 控制器 (FaultDetector + CkptScheduler + RestoreEngine) | `controllerfull` | - |
| `device-plugin` | DaemonSet | GPU 设备插件，向 Kubelet 注册 vGPU 资源 | `devicepluginfull` | - |
| `scheduler-extender` | Deployment | K8s 调度器扩展，GPU 拓扑感知调度 | `schedulerfull` | - |
| `webhook` | Deployment | MutatingWebhook，注入 libvgpu.so | `webhookfull` | - |
| `billing-engine` | Deployment | 计费采集引擎 | `billingenginefull` | - |
| `webui` | Deployment | React 前端 (通过 nginx 服务) | - | 80 |

### 2.3 项目目录结构

```
PhoenixGPU-/
├── cmd/                          # 可执行组件入口
│   ├── api-server/               #   REST API 服务
│   │   ├── main.go               #     主入口
│   │   ├── api_test.go           #     API 测试
│   │   └── internal/             #     内部包 (router, handlers)
│   ├── phoenix-controller/       #   HA 控制器
│   ├── device-plugin/            #   GPU 设备插件
│   ├── scheduler-extender/       #   调度器扩展
│   ├── webhook/                  #   MutatingWebhook
│   └── billing-engine/           #   计费采集引擎
├── pkg/                          # 共享库包
│   ├── types/                    #   域对象类型定义 + FakeK8sClient
│   │   └── types.go              #     ClusterSummary, GPUNode, PhoenixJob 等
│   ├── billing/                  #   计费引擎核心逻辑
│   │   └── engine.go             #     GPUSpec, UsageRecord, Engine
│   ├── checkpoint/               #   CRIU Checkpoint/Restore 封装
│   │   ├── criu.go               #     CRIUCheckpointer
│   │   ├── storage_contract_test.go
│   │   └── ...
│   ├── k8s/                      #   Kubernetes 客户端封装
│   │   ├── client.go             #     K8sClient 实现
│   │   ├── billing_querier.go    #     BillingQuerier 接口
│   │   └── prom_parser.go        #     Prometheus 指标解析
│   ├── controller/               #   控制器核心逻辑
│   ├── deviceplugin/             #   Device Plugin 核心逻辑
│   ├── scheduler/                #   调度器扩展核心逻辑
│   ├── webhook/                  #   Webhook 核心逻辑
│   ├── collector/                #   计费采集器
│   ├── hacontroller/             #   HA 控制器 (FaultDetector)
│   ├── migration/                #   GPU 热迁移
│   ├── vgpu/                     #   vGPU 超额分配
│   └── util/                     #   通用工具
├── libvgpu/                      # CUDA 拦截层 (C/C++)
│   ├── CMakeLists.txt            #   CMake 构建
│   ├── include/                  #   头文件
│   └── src/                      #   源代码
├── deploy/                       # 部署配置
│   ├── helm/phoenixgpu/          #   Helm Chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   ├── manifests/crd/            #   CRD 定义
│   │   └── phoenixjobs.yaml
│   └── db/                       #   数据库 Schema
│       └── 001_initial_schema.sql
├── build/                        # Docker 构建文件
│   ├── Dockerfile.api-server
│   ├── Dockerfile.phoenix-controller
│   ├── Dockerfile.device-plugin
│   ├── Dockerfile.scheduler-extender
│   ├── Dockerfile.webhook
│   ├── Dockerfile.billing-engine
│   ├── Dockerfile.webui
│   └── nginx.conf
├── hack/                         # 开发/运维脚本
│   ├── setup-dev.sh              #   开发环境初始化
│   ├── kind-setup.sh             #   KinD 本地集群
│   ├── preflight-check.sh        #   部署前检查
│   ├── profile-matrix.sh         #   构建配置矩阵测试
│   └── init-helm-repo.sh         #   Helm 仓库初始化
├── test/                         # E2E 测试
│   └── e2e/
│       └── ha_restore_test.go
├── webui/                        # React WebUI
│   ├── src/
│   └── package.json
├── docs/                         # 文档
│   ├── architecture/ARCHITECTURE.md
│   ├── community/
│   └── dev-guide/
├── Makefile                      # 构建入口
├── go.mod / go.sum               # Go 依赖管理
└── README.md
```

---

## 3. 核心组件详解

### 3.1 Phoenix HA Controller

**职责**：GPU 故障检测、自动 Checkpoint 调度、故障恢复编排。

**核心模块**：

| 模块 | 功能 | 关键参数 |
|------|------|---------|
| FaultDetector | 轮询节点健康状态，检测 NotReady 节点 | `faultDetectorPollSeconds: 10`, `notReadyThresholdSeconds: 30` |
| CkptScheduler | 按配置间隔触发 CRIU Checkpoint | `checkpointIntervalSeconds: 300` |
| RestoreEngine | 在健康节点恢复进程 | `maxRestoreAttempts: 3`, `restoreTimeoutSeconds: 120` |

**CRIU Checkpoint/Restore 封装** (`pkg/checkpoint/criu.go`)：

```
CRIUCheckpointer
├── Dump(ctx, pid, snapDir)      # CRIU 检查点：冻结进程 → 序列化内存/状态 → 保存到目录
├── Restore(ctx, snapDir)        # CRIU 恢复：从目录恢复进程状态 → 继续执行
└── PreDump(ctx, pid, snapDir)   # 预转储：增量记录脏页，减少 Dump 时的冻结时间
```

**恢复流程**：
```
Node A Down → K8s NotReady → FaultDetector (30s 阈值)
→ HA Controller: HandleNodeFault()
→ 下载最后一个 Snapshot
→ 在 Node B 创建新 Pod
→ 注入 libvgpu.so
→ CRIU Restore
→ 训练从 Checkpoint 继续（总计 ≈60s）
```

### 3.2 Device Plugin

**职责**：在每个 GPU 节点以 DaemonSet 运行，向 Kubelet 注册 vGPU 资源。

**核心功能**：
- GPU 发现：通过 `PHOENIX_GPU_CONFIG` 环境变量 (JSON) 或 `/dev/nvidia*` 设备文件
- 资源注册：`nvidia.com/vgpu`（vGPU 设备数）、`nvidia.com/vgpu-memory`（显存配额，MiB）
- 分配追踪：返回 libvgpu 注入环境变量 (`LD_PRELOAD`, `NVIDIA_VISIBLE_DEVICES`, `PHOENIX_VGPU_MEMORY_LIMIT`)
- 健康探测：`/healthz`, `/readyz`

**Prometheus 指标**：
- `phoenixgpu_device_total` — 总设备数
- `phoenixgpu_device_healthy` — 健康设备数
- `phoenixgpu_device_allocated` — 已分配设备数
- `phoenixgpu_device_free` — 空闲设备数

### 3.3 Scheduler Extender

**职责**：Kubernetes 调度器扩展，实现 GPU 拓扑感知调度。

**核心算法**：

| 方法 | 功能 | 说明 |
|------|------|------|
| `Filter()` | 节点过滤 | 检查 `nvidia.com/vgpu` 和 `nvidia.com/vgpu-memory` 是否满足 |
| `Prioritize()` | 节点打分 (0-100) | `binpack`：优先选择较满的节点；`spread`：优先选择较空的节点 |

**NUMA 拓扑加分**：当节点有 NUMA 拓扑标签时 +10 分（上限 100）。

### 3.4 MutatingWebhook

**职责**：Pod 创建时自动注入 libvgpu.so 和计费标注。

**注入条件**（满足任一即可）：
- Pod 有 `phoenixgpu.io/managed` 注解
- Pod 请求了 `nvidia.com/vgpu` 资源

**注入内容**（RFC 6902 JSON Patch）：
1. 环境变量：`LD_PRELOAD=/usr/local/lib/libvgpu.so`
2. Volume：`libvgpu` (HostPath)
3. VolumeMount：`/usr/local/lib/libvgpu.so`
4. 计费注解：`phoenixgpu.io/billing-*`

### 3.5 Billing Engine

**职责**：GPU 使用量采集与成本计算。

**计费模型**：
```
TFlops·h = AllocRatio × GPUSpec.FP16TFlops × DurationHours
Cost(CNY) = AllocRatio × GPUSpec.PricePerHour × DurationHours

示例:
  A100 80G 50% × 2h = 0.5 × 312 × 2 = 312 TFlops·h, ¥35
  H800 25% × 2h     = 0.25 × 2000 × 2 = 1000 TFlops·h, ¥27.5
```

**四级配额层次**：
```
集群总配额
 └─ 部门配额 (如: 算法研究院 1500 卡时/月)
     └─ 项目配额 (如: LLM预训练 600 卡时)
         └─ 单任务限制 (如: < 100 卡时)
```

**核心接口**：
- `JobLister` — 发现活跃 PhoenixJob（`K8sJobLister` 生产实现 / `FakeJobLister` 测试实现）
- `Collector.Collect()` — 采集 UsageRecord → `Engine.Record()` 写入数据库
- 采集间隔：默认 60s，数据保留 90 天

### 3.6 API Server

**职责**：提供 REST API，连接前端 WebUI 与后端集群数据。

**核心接口** (`K8sClientInterface`)：

| 方法 | 路径 | 说明 |
|------|------|------|
| `GetClusterSummary` | `GET /api/v1/cluster/summary` | 集群概览 |
| `ListGPUNodes` | `GET /api/v1/nodes` | GPU 节点列表 |
| `ListPhoenixJobs` | `GET /api/v1/jobs` | PhoenixJob 列表 |
| `GetPhoenixJob` | `GET /api/v1/jobs/:namespace/:name` | 单个任务详情 |
| `TriggerCheckpoint` | `POST /api/v1/jobs/:namespace/:name/checkpoint` | 手动触发 Checkpoint |
| `GetBillingByDepartment` | `GET /api/v1/billing/departments` | 部门计费汇总 |
| `GetBillingRecords` | `GET /api/v1/billing/records` | 计费明细 |
| `GetUtilizationHistory` | `GET /api/v1/cluster/utilization` | GPU 利用率历史 |
| `ListAlerts` | `GET /api/v1/alerts` | 告警列表 |
| `ResolveAlert` | `POST /api/v1/alerts/:id/resolve` | 解决告警 |

**安全特性**：
- Bearer Token 认证 (`--auth-tokens`)
- TLS 加密 (`--tls-cert`, `--tls-key`)
- 请求速率限制 (`--rate-limit-rps`)
- `/healthz`, `/readyz`, `/metrics` 免认证

### 3.7 WebUI

**技术栈**：React 18 + TypeScript + Vite

**页面**：
- 集群概览仪表板
- GPU 节点列表与详情
- PhoenixJob 管理
- 部门计费报表
- 告警中心

### 3.8 libvgpu.so (CUDA 拦截层)

**原理**：通过 `LD_PRELOAD` 注入到容器进程，拦截 CUDA API 调用。

**拦截链**：
```
PyTorch → libcudart.so → libvgpu.so (拦截) → libcuda.so → GPU 内核
```

**拦截函数**：

| CUDA 函数 | 用途 | PhoenixGPU 操作 |
|-----------|------|----------------|
| `cuMemAlloc_v2` | GPU 显存分配 | 配额检查 + 账本更新 |
| `cuMemFree_v2` | GPU 显存释放 | 账本更新 |
| `cuLaunchKernel` | CUDA 内核启动 | TFlops 计量 + SM 节流 |
| `nvmlDeviceGetMemoryInfo` | 查询 VRAM | 返回虚拟化值 |
| `nvmlDeviceGetUtilizationRates` | 查询利用率 | 返回按比例缩放值 |
| `cuDeviceGetAttribute` | 查询设备属性 | 虚拟化部分属性 |

**构建**：
```bash
make build-libvgpu
# 输出: libvgpu/build/libvgpu.so
# 依赖: CMake, CUDA Toolkit
```

---

## 4. 数据模型

### 4.1 PhoenixJob CRD

**API 版本**：`phoenixgpu.io/v1alpha1`

**Spec 字段**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `checkpoint.intervalSeconds` | int | 否 | 300 | Checkpoint 间隔 (60-86400s) |
| `checkpoint.storageBackend` | string | **是** | - | `pvc` / `s3` / `nfs` |
| `checkpoint.pvcName` | string | 条件 | - | PVC 名称 (backend=pvc 时必填) |
| `checkpoint.s3.bucket` | string | 条件 | - | S3 桶名 (backend=s3 时) |
| `checkpoint.s3.endpoint` | string | 条件 | - | S3 端点 (backend=s3 时) |
| `checkpoint.s3.secretRef` | string | 条件 | - | S3 凭证 Secret 名 |
| `checkpoint.maxSnapshots` | int | 否 | 5 | 最大快照保留数 (1-100) |
| `checkpoint.preDump` | bool | 否 | false | 启用预转储（大模型推荐） |
| `restorePolicy.onNodeFailure` | string | 否 | "restore" | `restore` / `restart` / `fail` |
| `restorePolicy.restoreTimeoutSeconds` | int | 否 | 120 | 恢复超时（秒） |
| `restorePolicy.maxRestoreAttempts` | int | 否 | 3 | 最大恢复重试次数 |
| `billing.department` | string | 否 | - | 部门归属 |
| `billing.project` | string | 否 | - | 项目归属 |
| `billing.costCenter` | string | 否 | - | 成本中心 |
| `template` | PodTemplateSpec | **是** | - | Pod 模板 |

**Status 字段**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `phase` | string | `Pending` / `Running` / `Checkpointing` / `Restoring` / `Succeeded` / `Failed` |
| `lastCheckpointTime` | date-time | 最后一次 Checkpoint 时间 |
| `lastCheckpointDir` | string | 最后一个快照路径 |
| `checkpointCount` | int | 累计 Checkpoint 次数 |
| `restoreAttempts` | int | 当前恢复重试次数 |
| `currentPodName` | string | 当前运行的 Pod 名称 |
| `currentNodeName` | string | 当前运行的节点名称 |
| `conditions` | array | 状态条件列表 |

### 4.2 计费数据库 Schema

数据库使用 **PostgreSQL + TimescaleDB** 扩展：

**核心表**：

| 表/视图 | 类型 | 用途 |
|---------|------|------|
| `usage_records` | 超级表 (hypertable) | GPU 使用记录，按月分区 |
| `quota_policies` | 普通表 | 配额策略（4级层次） |
| `billing_alerts` | 普通表 | 计费告警事件 |
| `daily_dept_summary` | 连续聚合（物化视图） | 部门每日汇总 |
| `current_month_billing` | 视图 | 当月部门计费 |
| `quota_utilization` | 视图 | 配额利用率 |

**自动化策略**：
- 数据压缩：7 天后自动压缩（约 20× 压缩比）
- 数据保留：90 天后自动删除
- 聚合刷新：每小时更新，覆盖最近 7 天

### 4.3 域对象模型

所有共享域对象定义在 `pkg/types/types.go`：

| 类型 | 说明 | 关键字段 |
|------|------|---------|
| `ClusterSummary` | 集群概览 | TotalGPUs, ActiveJobs, AvgUtilPct, TotalCostCNY |
| `GPUNode` | GPU 节点 | Name, GPUModel, GPUCount, VRAMTotalMiB, SMUtilPct, Ready, Faulted |
| `PhoenixJob` | GPU 训练任务 | Phase, CheckpointCount, GPUModel, AllocRatio, Department |
| `DeptBilling` | 部门计费 | Department, GPUHours, TFlopsHours, CostCNY, UsedPct |
| `Alert` | 告警 | Severity, Tenant, Message, Resolved |
| `UtilizationPoint` | 利用率数据点 | Timestamp, GPUUtil, VRAMUtil |
| `K8sClientInterface` | K8s 客户端接口 | 8 个核心方法 |
| `FakeK8sClient` | 模拟客户端 | 用于开发/测试，返回预置数据 |

---

## 5. 关键流程

### 5.1 GPU 故障自动恢复流程

```
1. 节点故障          → Node A 硬件故障或网络断开
2. K8s 检测          → kubelet 停止心跳，节点状态变为 NotReady
3. FaultDetector     → 每 10s 轮询节点状态，发现 NotReady 超过 30s
4. 触发故障处理       → HandleNodeFault(nodeA)
5. 查找受影响任务     → 列出 nodeA 上的所有 PhoenixJob
6. 下载快照           → 从 PVC/S3 获取最后一个 Checkpoint 目录
7. 选择目标节点       → Scheduler Extender 选择健康的 GPU 节点
8. 创建新 Pod        → 在目标节点创建 Pod，Webhook 自动注入 libvgpu.so
9. CRIU Restore      → 从 Checkpoint 恢复进程状态
10. 训练继续          → 进程从断点处继续执行
```

**SLA 目标**：整个恢复流程 < 60 秒。

### 5.2 Checkpoint 流程

```
1. CkptScheduler 定时触发    → 每 300s（可配置）
2. 可选: PreDump             → 增量记录脏页，减少后续冻结窗口
3. CRIU Dump                 → 冻结进程 → 序列化内存+寄存器+FD+信号 → 写入快照目录
4. 上传快照                   → 保存到 PVC/S3/NFS
5. 更新 Status                → checkpointCount++, lastCheckpointTime, lastCheckpointDir
6. 快照裁剪                   → 超过 maxSnapshots 时删除最旧快照
7. 进程继续                   → 训练进程恢复执行（冻结窗口 < 5s）
```

### 5.3 计费采集流程

```
1. Collector 定时触发         → 每 60s
2. 发现活跃任务               → JobLister.ListActiveJobs()
3. 采集使用量                 → 每个任务生成 UsageRecord
4. 计算 TFlops·h             → AllocRatio × FP16TFlops × Duration
5. 计算 Cost(CNY)            → AllocRatio × PricePerHour × Duration
6. 写入数据库                 → Engine.Record(records)
7. 检查配额                   → 超过 80%/100% 触发告警
```

### 5.4 调度决策流程

```
1. Pod 创建请求               → 请求 nvidia.com/vgpu + nvidia.com/vgpu-memory
2. K8s Scheduler → Filter    → 调用 Extender.Filter()
3. GPU 资源检查               → 检查节点 vGPU 和显存是否满足
4. 过滤不满足的节点            → 返回可行节点列表
5. K8s Scheduler → Prioritize → 调用 Extender.Prioritize()
6. 策略打分 (0-100)           → binpack: 优先满节点 | spread: 优先空节点
7. NUMA 拓扑加分              → 有拓扑标签的节点 +10 分
8. 返回得分                   → K8s 选择最高分节点
```

---

## 6. 构建系统

### 6.1 构建标签 (Build Tags)

PhoenixGPU 使用 Go 构建标签控制组件编译。默认构建产生 stub 二进制文件：

| 构建标签 | 组件 | 说明 |
|---------|------|------|
| (无标签) | 所有 stub | 最小化编译，用于 CI |
| `k8sfull` | API Server (真实 K8s 客户端) | 连接真实集群 |
| `controllerfull` | Phoenix Controller | 启用控制器逻辑 |
| `devicepluginfull` | Device Plugin | 启用设备插件 |
| `schedulerfull` | Scheduler Extender | 启用调度器扩展 |
| `webhookfull` | Webhook | 启用 MutatingWebhook |
| `billingenginefull` | Billing Engine | 启用计费引擎 |
| `billingfull` | 计费核心库 | 启用计费计算逻辑 |
| `checkpointfull` | Checkpoint 库 | 启用 CRIU 封装 |
| `migrationfull` | GPU 热迁移 | 启用迁移功能 |

每个组件都有 `main.go`（带标签）和 `main_stub.go`（反向标签），确保所有组件始终可编译。

### 6.2 Makefile 命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译 api-server 和 phoenix-controller |
| `make build-libvgpu` | 编译 CUDA 拦截层 (libvgpu.so) |
| `make test` | 运行 Go 单元测试 (带 -race) |
| `make test-short` | 运行快速测试 |
| `make test-verbose` | 运行详细输出测试 |
| `make test-e2e` | 运行端到端测试 (需要集群) |
| `make lint` | 运行 golangci-lint |
| `make fmt` | 代码格式化 (go fmt + goimports) |
| `make vet` | 静态检查 (go vet) |
| `make docker-build` | 构建所有组件 Docker 镜像 |
| `make docker-push` | 推送所有组件 Docker 镜像 |
| `make helm-lint` | Helm Chart 校验 |
| `make helm-install` | Helm 安装到集群 |
| `make helm-uninstall` | Helm 卸载 |
| `make kind-up` | 创建 KinD 开发集群 |
| `make kind-down` | 删除 KinD 开发集群 |
| `make clean` | 清理构建产物 |
| `make deps` | 下载并整理 Go 依赖 |
| `make validate` | Mock 模式部署验证 |
| `make validate-live` | 真实集群部署验证 |

### 6.3 Docker 镜像构建

所有镜像采用多阶段构建，最终镜像基于 `distroless/static-debian12:nonroot`（约 15 MiB）：

```bash
# 构建单个组件
docker build -f build/Dockerfile.api-server \
  --build-arg VERSION=$(git describe --tags) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  -t ghcr.io/wlqtjl/phoenixgpu/api-server:latest .

# 构建所有组件
make docker-build

# 推送
make docker-push
```

**镜像列表**：

| 镜像 | Dockerfile | 基础镜像 |
|------|-----------|---------|
| `api-server` | `build/Dockerfile.api-server` | distroless/static |
| `phoenix-controller` | `build/Dockerfile.phoenix-controller` | distroless/static |
| `device-plugin` | `build/Dockerfile.device-plugin` | distroless/static |
| `scheduler-extender` | `build/Dockerfile.scheduler-extender` | distroless/static |
| `webhook` | `build/Dockerfile.webhook` | distroless/static |
| `billing-engine` | `build/Dockerfile.billing-engine` | distroless/static |
| `webui` | `build/Dockerfile.webui` | nginx |

---

## 7. Helm Chart 配置

Helm Chart 位于 `deploy/helm/phoenixgpu/`：

**模板文件**：

| 文件 | 内容 |
|------|------|
| `_helpers.tpl` | 模板助手函数 (fullname, labels, selectorLabels) |
| `api-webui.yaml` | API Server + WebUI Deployment/Service/PVC |
| `phoenix-controller-deployment.yaml` | Controller Deployment + Volumes |
| `rbac.yaml` | ServiceAccount + ClusterRole + ClusterRoleBinding |

**RBAC 权限**：

| ServiceAccount | 权限 |
|---------------|------|
| `phoenix-controller` | PhoenixJobs (CRUD), Nodes/Pods (读取), Events (创建) |
| `device-plugin` | Nodes/Pods/PhoenixJobs (读取) |

---

## 8. API 接口

**基础 URL**: `http://<api-server>:8090`

| 端点 | 方法 | 说明 | 认证 |
|------|------|------|------|
| `/healthz` | GET | 健康检查 | 否 |
| `/readyz` | GET | 就绪检查 | 否 |
| `/metrics` | GET | Prometheus 指标 | 否 |
| `/api/v1/cluster/summary` | GET | 集群概览 | 是 |
| `/api/v1/cluster/utilization` | GET | GPU 利用率历史 | 是 |
| `/api/v1/nodes` | GET | GPU 节点列表 | 是 |
| `/api/v1/jobs` | GET | PhoenixJob 列表 | 是 |
| `/api/v1/jobs/:ns/:name` | GET | 任务详情 | 是 |
| `/api/v1/jobs/:ns/:name/checkpoint` | POST | 手动 Checkpoint | 是 |
| `/api/v1/billing/departments` | GET | 部门计费 | 是 |
| `/api/v1/billing/records` | GET | 计费明细 | 是 |
| `/api/v1/alerts` | GET | 告警列表 | 是 |
| `/api/v1/alerts/:id/resolve` | POST | 解决告警 | 是 |

---

## 9. 可观测性

### Prometheus 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `phoenixgpu_criu_dump_duration_seconds` | Histogram | Checkpoint 耗时分布 |
| `phoenixgpu_criu_dump_total` | Counter | Checkpoint 总次数 |
| `phoenixgpu_criu_restore_duration_seconds` | Histogram | Restore 耗时分布 |
| `phoenixgpu_criu_restore_total` | Counter | Restore 总次数 |
| `phoenixgpu_criu_snapshot_bytes` | Gauge | 快照大小 |

### DCGM Exporter

启用后通过 `nvcr.io/nvidia/k8s/dcgm-exporter:3.3.5-3.4.0-ubuntu22.04` 采集 GPU 硬件指标。

---

## 10. 安全机制

| 机制 | 说明 | 配置 |
|------|------|------|
| Bearer Token 认证 | API 请求需携带 Bearer Token | `--auth-tokens token1,token2` |
| TLS 加密 | HTTPS 加密传输 | `--tls-cert`, `--tls-key` |
| 请求速率限制 | 按 IP 限制请求速率 | `--rate-limit-rps 100` |
| 免认证端点 | 健康检查和指标不需认证 | `/healthz`, `/readyz`, `/metrics` |
| 陈旧 IP 自动清理 | 速率限制器自动清理不活跃 IP | 内置逻辑 |
| distroless 镜像 | 最小攻击面，无 shell | `gcr.io/distroless/static-debian12:nonroot` |
| RBAC 最小权限 | 每个组件独立 ServiceAccount | Helm Chart 中定义 |

---

## 11. 架构决策记录 (ADR)

| ADR | 决策 | 理由 | 代价 |
|-----|------|------|------|
| ADR-001 | 使用 CRIU v3.x + cuda-checkpoint | CRIU 在 K8s Pod 迁移、Podman 中经过生产验证 | 需维护 CUDA 版本兼容性矩阵 |
| ADR-002 | 基于 HAMi 的 libvgpu.so | HAMi 覆盖 222 个 CUDA 函数，经 CNCF 验证 | 需维护 UPSTREAM.md，避免 fork 漂移 |
| ADR-003 | TFlops·h 而非 GPU% 计费 | GPU% 跨型号不可比；TFlops·h 跨型号公平 | 需维护 GPU 型号→TFlops 映射表 |

---

## 12. 性能指标与 SLA

| 指标 | 设计目标 | 说明 |
|------|---------|------|
| GPU 故障恢复时间 | < 60 秒 | 从节点故障到训练继续 |
| Checkpoint 耗时 | < 30 秒 | 单次 CRIU Dump |
| Checkpoint 吞吐影响 | < 5% | 5 分钟间隔下 < 1% |
| 进程冻结窗口 | < 5 秒 | 使用 PreDump 优化 |
| 故障检测时间 | < 30 秒 | FaultDetector 轮询阈值 |
| 计费精度 | < 1% 误差 | 对比实际运行时间 |
| VRAM 隔离 | 硬隔离 | 超额分配触发 OOM |
