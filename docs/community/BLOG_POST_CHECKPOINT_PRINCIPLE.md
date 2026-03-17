# GPU 故障零中断：PhoenixGPU 的 Checkpoint/Restore 原理

> 作者：PhoenixGPU 团队 | 发布于：2026年4月
> 标签：Kubernetes · GPU · CRIU · 云原生 · AI基础设施

---

## 背景：为什么训练中断如此昂贵？

2026年，一张 NVIDIA H800 的内部租用成本约 ¥55/小时。一个使用 8 张 H800、跑了 72 小时的预训练任务，在节点宕机的瞬间，损失的不只是计算资源：

**损失 = GPU 成本 + 人力成本 + 时间成本 + 科研进度**

```
8 × H800 × ¥55/h × 72h = ¥31,680（直接成本）
实验员重新等待结果：+3 天
情绪损耗：无法量化
```

HAMi、Dynamia、KAI Scheduler 都无法解决这个问题——它们聚焦于"如何分配 GPU"，而非"GPU 故障后如何继续"。

PhoenixGPU 从另一个角度出发：**让 GPU 任务像数据库事务一样可恢复**。

---

## 核心技术：CRIU（Checkpoint/Restore in Userspace）

CRIU 是 Linux 内核项目的组成部分，原本用于容器热迁移。PhoenixGPU 将其扩展到 GPU 场景。

### CRIU 做了什么？

CRIU 的 `dump` 命令将一个运行中的进程"冻结"并序列化为文件：

```
/tmp/phoenix-snapshots/research/llm-pretrain/ckpt-00024/
├── core-1234.img      # CPU 寄存器状态
├── mm-1234.img        # 内存映射表
├── pages-1.img        # 实际内存页（最大，几 GB）
├── fs-1234.img        # 文件描述符状态
├── tcp-stream-*.img   # 网络连接状态
└── meta.json          # PhoenixGPU 元数据
```

恢复时，`criu restore` 读取这些文件，重建进程——就像它从未被中断过一样。

### GPU 上下文的挑战

普通进程的 CRIU dump 成熟稳定，但 GPU 进程有额外状态：

- **CUDA 上下文**：设备初始化、流、事件
- **GPU 显存内容**：模型参数、梯度、优化器状态
- **NCCL 通信状态**：多 GPU 分布式训练的集合操作

PhoenixGPU 使用 `cuda-checkpoint` 插件处理 GPU 上下文，在 CRIU dump 前后分别调用：

```bash
# CRIU 的 --action-script 钩子
pre-dump:  cuda-checkpoint --pid $PID --action checkpoint
post-restore: cuda-checkpoint --pid $NEW_PID --action restore
```

---

## PhoenixGPU 的实现：从节点宕机到自动恢复

### 1. 周期性 Checkpoint（防患于未然）

```
训练运行中
    ├── T+0m   → Checkpoint #1（快照写入 PVC）
    ├── T+5m   → Checkpoint #2
    ├── T+10m  → Checkpoint #3（当前最新）
    └── T+12m  → 节点宕机 ← 损失最多 5 分钟训练
```

PhoenixHA Controller 每 5 分钟（可配置）自动触发一次 CRIU dump。

### 2. 故障检测（< 30 秒）

```go
// FaultDetector 每 10s 轮询节点状态
func (fd *FaultDetector) checkNode(node *corev1.Node) {
    if !isNodeReady(node) {
        // 记录首次 NotReady 时间
        // 等待 30s 阈值（避免瞬时抖动误报）
        // 超过阈值 → 发出 FaultEvent
    }
}
```

### 3. 自动恢复（< 30 秒）

```go
func (r *PhoenixHAController) HandleNodeFault(ctx context.Context, event FaultEvent) {
    // 找到受影响的所有 PhoenixJob
    // 下载最新 Checkpoint（从 PVC 或 S3）
    // 在健康节点创建新 Pod
    // criu restore → 训练从断点继续
}
```

**总恢复时间 = 故障检测(≤30s) + 快照下载(≤10s) + CRIU restore(≤20s) = ≤60s**

---

## 关键设计权衡

### Checkpoint 频率 vs 开销

| 间隔 | 最大损失 | 性能影响 | 推荐场景 |
|------|---------|---------|---------|
| 1 分钟 | 1 分钟 | ~5% 吞吐降低 | 高价值短任务 |
| 5 分钟 | 5 分钟 | ~1% 吞吐降低 | **推荐默认** |
| 15 分钟 | 15 分钟 | < 0.5% | 长任务 / 存储有限 |

### Pre-dump：将冻结窗口从 60s 压缩到 < 5s

核心优化：在全量 dump 之前先做 `pre-dump`（不中断进程）：

```
Pre-dump（不暂停）：写入所有内存页 → 50s（进程继续运行）
Full dump（暂停）：只写入 pre-dump 后的脏页 → 2-5s ← 实际冻结窗口
```

对于 80GB VRAM 的训练任务，2% dirty ratio → 1.6GB → NVMe 2GB/s → **约 0.8s 冻结窗口**。

---

## 快速开始

```bash
# 安装 PhoenixGPU
helm repo add phoenixgpu https://wlqtjl.github.io/PhoenixGPU-/charts
helm install phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system --create-namespace

# 提交一个受保护的训练任务
kubectl apply -f - <<EOF
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: my-training
  namespace: research
spec:
  checkpoint:
    intervalSeconds: 300
    storageBackend: pvc
    pvcName: checkpoint-store
  template:
    spec:
      containers:
      - name: trainer
        image: pytorch/pytorch:2.3.0-cuda12.1
        command: ["python", "train.py"]
        resources:
          limits:
            nvidia.com/vgpu: "1"
            nvidia.com/vgpu-memory: "16384"
EOF
```

---

## 结语

PhoenixGPU 不是要替代 HAMi 或 KAI Scheduler，而是**在它们之上增加 HA 层**。

GPU 虚拟化解决了"如何高效分配算力"，PhoenixGPU 解决了"算力分配后如何保障不被浪费"。

项目开源：https://github.com/wlqtjl/PhoenixGPU-

欢迎高校 GPU 集群管理员试用，我们提供免费 PoC 支持。

---

*本文作者：PhoenixGPU 核心团队 · Apache 2.0 开源 · 欢迎 Star 和 PR*
