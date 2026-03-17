# KubeCon + CloudNativeCon 2026 Talk Proposal

## Title
**PhoenixGPU: GPU Fault Tolerance for AI Workloads on Kubernetes**

*Subtitle: How CRIU-based Checkpoint/Restore brings HA to GPU training jobs*

---

## Abstract (500 words)

GPU hardware failures are an inevitable reality in large-scale AI training clusters. 
A single node failure can invalidate hours or days of computation, wasting thousands 
of dollars in GPU time and delaying critical research.

Existing Kubernetes GPU solutions—NVIDIA Device Plugin, HAMi, KAI Scheduler—focus 
on efficient resource *allocation*, but none address what happens when a node goes down 
mid-training. The standard answer is "restart from the last manual checkpoint," which 
places the burden entirely on application developers and often means losing hours of work.

PhoenixGPU takes a different approach: **automatic, application-transparent GPU fault 
tolerance via CRIU (Checkpoint/Restore in Userspace)**.

In this talk, we will cover:

1. **The problem in numbers**: Real cost analysis of GPU training interruptions at 
   research institutions, and why existing solutions fall short.

2. **Architecture deep-dive**: How PhoenixGPU extends HAMi's GPU virtualization with:
   - Periodic automatic CRIU checkpoint (< 1% throughput overhead at 5-min intervals)
   - FaultDetector: node failure detection in < 30 seconds
   - Automatic restore on healthy nodes in < 60 seconds total
   - Pre-dump optimization: reducing process freeze window from ~60s to < 5s

3. **Live demo**: We will simulate a node failure during an active PyTorch training job 
   and show PhoenixGPU automatically restoring the job from the last checkpoint on a 
   new node, with training continuing from the exact step where it stopped.

4. **Lessons learned**: Challenges integrating CRIU with CUDA context serialization, 
   multi-GPU (NCCL) state, and the Kubernetes pod lifecycle.

5. **Roadmap**: GPU live migration (zero-freeze cross-node migration), VRAM 
   oversubscription, and our path to CNCF Sandbox.

PhoenixGPU is Apache 2.0 open source. It is already deployed at several Chinese 
universities and research institutions as an early adopter program.

---

## Session Type
☑ Technical Session (35 minutes + 10 min Q&A)

## Track
☑ Runtime / Scheduling
☑ AI & Machine Learning

## Audience Level
☑ Intermediate (familiar with Kubernetes, some GPU knowledge helpful)

## Speaker Bio
PhoenixGPU core team — building open-source GPU infrastructure for AI research.

---

# 高校 GPU 集群 PoC 部署方案

## 目标
让高校运维人员在 **2 小时内**完成 PhoenixGPU 安装并验证核心功能。

## 前置条件检查

```bash
# 运行检查脚本
curl -sL https://raw.githubusercontent.com/wlqtjl/PhoenixGPU-/main/hack/preflight-check.sh | bash
```

检查项：
- [ ] Kubernetes ≥ 1.26（`kubectl version`）
- [ ] NVIDIA GPU Driver ≥ 525（`nvidia-smi`）
- [ ] CUDA 12.x（`nvcc --version`）
- [ ] Helm ≥ 3.x（`helm version`）
- [ ] StorageClass 可用（`kubectl get sc`）
- [ ] 节点标签 `node-role.kubernetes.io/gpu=true`

## 安装步骤（≈ 20 分钟）

```bash
# Step 1: 安装 CRD
kubectl apply -f https://raw.githubusercontent.com/wlqtjl/PhoenixGPU-/main/deploy/manifests/crd/phoenixjobs.yaml

# Step 2: Helm 安装
helm repo add phoenixgpu https://wlqtjl.github.io/PhoenixGPU-/charts
helm install phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system \
  --create-namespace \
  --set snapshotStorage.pvc.size=50Gi \
  --wait --timeout=5m

# Step 3: 验证
kubectl get pods -n phoenixgpu-system
# 期望所有 Pod 处于 Running 状态
```

## 功能验证（≈ 30 分钟）

### 验证 1：基础 GPU 虚拟化

```bash
# 提交测试任务
kubectl apply -f - <<EOF
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: phoenix-test
  namespace: default
spec:
  checkpoint:
    intervalSeconds: 60
    storageBackend: pvc
    pvcName: phoenix-test-pvc
  template:
    spec:
      containers:
      - name: trainer
        image: pytorch/pytorch:2.3.0-cuda12.1
        command: ["python", "-c", "import torch; print(torch.cuda.get_device_name(0)); import time; [time.sleep(1) for _ in range(300)]"]
        resources:
          limits:
            nvidia.com/vgpu: "1"
            nvidia.com/vgpu-memory: "4096"
EOF

kubectl get phoenixjobs
# 期望 PHASE=Running
```

### 验证 2：Checkpoint 自动触发

```bash
# 等待 70 秒（第一次 Checkpoint）
sleep 70
kubectl get phoenixjobs phoenix-test -o jsonpath='{.status.checkpointCount}'
# 期望输出 >= 1
```

### 验证 3：故障恢复（核心验证）

```bash
# 模拟节点故障（排空节点）
NODE=$(kubectl get phoenixjobs phoenix-test -o jsonpath='{.status.currentNodeName}')
kubectl cordon $NODE
kubectl drain $NODE --ignore-daemonsets --delete-emptydir-data --force

# 观察自动恢复
watch kubectl get phoenixjobs phoenix-test
# 期望：Restoring → Running（60秒内）
```

## 访问 WebUI

```bash
kubectl port-forward -n phoenixgpu-system svc/phoenixgpu-webui 3000:80
# 打开浏览器：http://localhost:3000
```

## 反馈与支持

- GitHub Issues：https://github.com/wlqtjl/PhoenixGPU-/issues
- 微信群：见 README（高校用户优先支持）
- 邮件：见 MAINTAINERS.md

## PoC 合作条款

提供免费技术支持，换取：
- 允许在 Adopters.md 中列出贵机构名称（可匿名）
- 提供 1-2 段使用反馈（用于项目改进）
- 可选：联名发表案例研究文章
