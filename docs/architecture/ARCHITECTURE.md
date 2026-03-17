# PhoenixGPU 技术架构文档

## 1. 设计目标

| 目标 | 指标 |
|------|------|
| GPU 故障恢复时间 | < 60 秒（节点宕机到新节点恢复运行）|
| Checkpoint 开销 | < 5% 训练吞吐量影响 |
| VRAM 隔离精度 | 硬隔离，超限即 OOM，非软限制 |
| 计费误差 | < 1% 误差（相较实际使用时长）|
| 兼容性 | 不修改应用代码，不依赖 MIG |

---

## 2. 系统分层

```
┌─────────────────────────────────────────────────────────────┐
│                       用户层 (Users)                         │
│  kubectl apply PhoenixJob.yaml                              │
│  Phoenix WebUI Dashboard                                    │
└─────────────────────┬───────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────┐
│                   控制面 (Control Plane)                     │
│                                                             │
│  ┌──────────────────┐    ┌────────────────────────────┐    │
│  │ Phoenix HA        │    │  Scheduler Extender        │    │
│  │ Controller        │    │  (拓扑感知 + HA 感知)       │    │
│  │                  │    │                            │    │
│  │ • FaultDetector  │    │  • 排除故障节点             │    │
│  │ • CkptScheduler  │    │  • 优先 Snapshot 亲和节点   │    │
│  │ • RestoreEngine  │    │  • Binpack / Spread 策略    │    │
│  └──────────┬───────┘    └────────────────────────────┘    │
│             │                                               │
│  ┌──────────▼───────┐    ┌────────────────────────────┐    │
│  │ Snapshot Manager │    │  Billing Engine            │    │
│  │                  │    │                            │    │
│  │ • PVC backend    │    │  • TFlops·h 计量            │    │
│  │ • S3 backend     │    │  • 配额管理                 │    │
│  │ • Pruning        │    │  • 告警推送                 │    │
│  └──────────────────┘    └────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────┐
│                   节点面 (Node Plane)                        │
│                                                             │
│  ┌──────────────────┐    ┌────────────────────────────┐    │
│  │ Device Plugin    │    │  MutatingWebhook           │    │
│  │ (每节点 DaemonSet)│    │  (Pod 注入)                │    │
│  │                  │    │                            │    │
│  │ • 注册 vGPU 资源  │    │  • 注入 LD_PRELOAD          │    │
│  │ • Allocate()     │    │  • 挂载 libvgpu.so         │    │
│  │ • 注入环境变量    │    │  • 注入 PhoenixJob 标签     │    │
│  └──────────────────┘    └────────────────────────────┘    │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              libvgpu.so (容器内)                      │  │
│  │                                                      │  │
│  │  AI 应用 → dlsym() override → cuMemAlloc 代理         │  │
│  │  • VRAM 配额强制 (硬隔离)                              │  │
│  │  • SM 占用软限速                                       │  │
│  │  • TFlops 计量 (cuLaunchKernel 拦截)                  │  │
│  │  • NVML 伪装 (应用看到虚拟 GPU)                        │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. PhoenixHA 故障恢复流程

```
时序图：节点宕机 → 训练自动恢复

GPU节点A          K8s API Server        FaultDetector         HA Controller
   │                    │                     │                     │
   │  [节点宕机]        │                     │                     │
   │ ────────────────→  │ Node.Status=        │                     │
   │                    │ NotReady            │                     │
   │                    │ ─────────────────→  │ Poll发现NotReady     │
   │                    │                     │ 等待30s threshold   │
   │                    │                     │ ─────────────────→  │
   │                    │                     │                     │ HandleNodeFault()
   │                    │                     │                     │ 扫描受影响Pod
   │                    │                     │                     │
   │                    │                     │                     │ 下载最近Snapshot
   │                    │                     │                     │ ←── PVC/S3
   │                    │                     │                     │
   │                    │    创建新Pod(节点B)  │                     │
   │                    │ ←───────────────────────────────────────  │
   │                    │                     │                     │
   GPU节点B             │                     │                     │
   │ 注入libvgpu.so     │                     │                     │
   │ ←────────────────  │                     │                     │
   │                    │                     │                     │
   │ criu restore       │                     │                     │
   │ ←──────────────────────────────────────────────────────────── │
   │                    │                     │                     │
   │ [训练从断点继续]   │                     │                     │
   │ ≈60s from fault    │                     │                     │
```

---

## 4. CUDA 拦截层详解

### 拦截原理

```
AI应用进程内存空间:

  PyTorch → libcudart.so → [libvgpu.so 拦截] → libcuda.so → 内核驱动

LD_PRELOAD=/usr/local/lib/libvgpu.so python train.py
         ↓
  libvgpu.so 的 constructor 在 main() 之前执行
         ↓
  覆盖 dlsym() 函数指针
         ↓
  应用调用 cuMemAlloc → 实际执行 phoenix_cuMemAlloc_proxy
```

### 拦截的关键函数

| 函数 | 用途 | Phoenix 操作 |
|------|------|-------------|
| `cuMemAlloc_v2` | GPU 内存分配 | 配额检查 + 账本更新 |
| `cuMemFree_v2` | GPU 内存释放 | 账本更新 |
| `cuLaunchKernel` | CUDA kernel 启动 | TFlops 计量 + SM 限速 |
| `nvmlDeviceGetMemoryInfo` | 查询显存 | 返回虚拟值（伪装）|
| `nvmlDeviceGetUtilizationRates` | 查询利用率 | 返回缩放值 |
| `cuDeviceGetAttribute` | 查询设备属性 | 部分属性虚拟化 |

---

## 5. 计费模型

### TFlops·h 作为统一货币

```
问题：1% A100 ≠ 1% RTX 4090（算力差距 1.9×）

解决：以 FP16 TFlops 标准化

TFlops·h = AllocRatio × GPUSpec.FP16TFlops × DurationHours

示例：
  A100 80G 50% × 2h = 0.5 × 312 × 2 = 312 TFlops·h
  H800    25% × 2h  = 0.25 × 2000 × 2 = 1000 TFlops·h

内部费用（可配置）：
  CostCNY = AllocRatio × PricePerHour × DurationHours
```

### 四层配额体系

```
集群总配额
  └── 部门配额 (e.g. 算法研究院: 1500 卡时/月)
        └── 项目配额 (e.g. LLM预训练: 600 卡时)
              └── 单任务上限 (e.g. 单次 < 100 卡时)
```

---

## 6. 关键设计决策 (ADR)

### ADR-001: 选择 CRIU 而非自研 CUDA Context 序列化

**决策**: 使用 CRIU v3.x + cuda-checkpoint 插件  
**理由**: CRIU 已在 K8s Pod 迁移、Podman 等项目中被大规模验证。自研 CUDA Context 序列化需要深入 NVIDIA 私有协议，风险极高且维护成本不可控。  
**代价**: 对某些 CUDA 版本的兼容性需要维护测试矩阵。

### ADR-002: 基于 HAMi 二次开发而非全部自研

**决策**: libvgpu.so 和 Device Plugin 基于 HAMi v2.4.0 二次开发  
**理由**: HAMi 的 CUDA 拦截层已覆盖 222 个函数，经过 CNCF 社区验证。从零重写需要 6-9 人月，核心壁垒应投入在 Checkpoint/Billing/HA 上。  
**代价**: 需维护 UPSTREAM.md 跟踪上游变更，避免分叉漂移。

### ADR-003: 以 TFlops·h 而非 GPU% 作为计费单位

**决策**: 内部使用 TFlops·h 作为算力货币  
**理由**: GPU% 在不同型号间没有可比性（A100 1% ≠ 3090 1%），导致按百分比计费产生不公平。TFlops·h 跨型号完全可比，且便于后续支持异构 GPU。  
**代价**: 需要维护 GPU 型号 → TFlops 映射表。

---

## 7. 仓库结构

见 README.md 和 CONTRIBUTING.md。
