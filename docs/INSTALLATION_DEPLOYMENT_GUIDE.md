# PhoenixGPU 安装部署指南

> **版本**: v0.1.0 | **最后更新**: 2026-04-05 | **许可证**: Apache 2.0

---

## 目录

- [1. 基础环境介绍](#1-基础环境介绍)
  - [1.1 测试 GPU 服务器配置](#11-测试-gpu-服务器配置)
  - [1.2 软件环境要求](#12-软件环境要求)
  - [1.3 网络要求](#13-网络要求)
- [2. 环境准备](#2-环境准备)
  - [2.1 操作系统配置](#21-操作系统配置)
  - [2.2 安装 NVIDIA GPU 驱动](#22-安装-nvidia-gpu-驱动)
  - [2.3 安装 CUDA Toolkit](#23-安装-cuda-toolkit)
  - [2.4 安装 NVIDIA Container Toolkit](#24-安装-nvidia-container-toolkit)
  - [2.5 安装 Kubernetes 集群](#25-安装-kubernetes-集群)
  - [2.6 安装 Helm](#26-安装-helm)
  - [2.7 安装 CRIU](#27-安装-criu)
- [3. PhoenixGPU 安装](#3-phoenixgpu-安装)
  - [3.1 安装 CRD](#31-安装-crd)
  - [3.2 Helm 安装 PhoenixGPU](#32-helm-安装-phoenixgpu)
  - [3.3 自定义配置](#33-自定义配置)
  - [3.4 验证安装](#34-验证安装)
- [4. 本地开发环境部署 (KinD)](#4-本地开发环境部署-kind)
- [5. 生产环境部署](#5-生产环境部署)
  - [5.1 高可用部署](#51-高可用部署)
  - [5.2 存储后端配置](#52-存储后端配置)
  - [5.3 数据库配置](#53-数据库配置)
  - [5.4 监控配置](#54-监控配置)
  - [5.5 安全配置](#55-安全配置)
- [6. 升级与回滚](#6-升级与回滚)
- [7. 卸载](#7-卸载)
- [8. 故障排查](#8-故障排查)

---

## 1. 基础环境介绍

### 1.1 测试 GPU 服务器配置

PhoenixGPU 测试集群由 **4 个 GPU 节点** 组成，总计 **32 块 GPU**：

| 节点名称 | GPU 型号 | GPU 数量 | 单卡显存 | 总显存 | 典型负载指标 |
|----------|---------|---------|---------|--------|------------|
| `gpu-node-01` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM利用率 82%, 功耗 380W, 温度 72°C |
| `gpu-node-02` | NVIDIA A100 80GB | 8 | 80 GB | 81,920 MiB | SM利用率 65%, 功耗 310W, 温度 68°C |
| `gpu-node-03` | NVIDIA A100 40GB | 8 | 40 GB | 40,960 MiB | SM利用率 71%, 功耗 270W, 温度 64°C |
| `gpu-node-04` | NVIDIA H800 | 8 | 80 GB | 81,920 MiB | SM利用率 91%, 功耗 420W, 温度 79°C |

> **注**: 以上为 `pkg/types/types.go` 中 FakeK8sClient 模拟数据定义的参考配置。实际部署时根据硬件自动发现。

**支持的 GPU 型号与计费规格**：

| GPU 型号 | FP16 算力 (TFlops) | 参考单价 (CNY/h) | 备注 |
|---------|-------------------|-----------------|------|
| NVIDIA H800 | 2000 | ¥55 | 最高性能，适合大模型预训练 |
| NVIDIA A100 80GB | 312 | ¥35 | 标准参考型号 |
| NVIDIA A100 40GB | 312 | ¥22 | 低显存版本 |
| NVIDIA RTX 4090 | 165 | ¥12 | 消费级 GPU |
| 华为 Ascend 910B | 256 | ¥28 | 国产替代加速卡 |

> 计费单位为 **TFlops·h**，公式：`TFlops·h = AllocRatio × GPUSpec.FP16TFlops × DurationHours`

**集群概览指标（测试环境默认值）**：

| 指标 | 值 |
|------|-----|
| 总 GPU 数 | 32 |
| 活跃任务 | 18 |
| Checkpoint 中的任务 | 2 |
| 恢复中的任务 | 1 |
| 平均 GPU 利用率 | 74.2% |
| 告警数 | 3 |
| 累计 GPU·h | 1,840 |
| 累计费用 (CNY) | ¥64,400 |

### 1.2 软件环境要求

#### 最低要求

| 组件 | 最低版本 | 验证命令 | 说明 |
|------|---------|---------|------|
| **操作系统** | Linux (Ubuntu 20.04+ / CentOS 7+) | `uname -a` | CRIU 仅支持 Linux |
| **NVIDIA GPU 驱动** | ≥ 525 | `nvidia-smi` | 需支持 CUDA 12.x |
| **CUDA Toolkit** | 12.x | `nvcc --version` | GPU 编程运行时 |
| **NVIDIA Container Toolkit** | ≥ 1.13 | `nvidia-ctk --version` | 容器 GPU 访问 |
| **Kubernetes** | ≥ 1.26 | `kubectl version` | 容器编排平台 |
| **Helm** | ≥ 3.x | `helm version` | Kubernetes 包管理器 |
| **CRIU** | ≥ 3.17 | `criu --version` | 进程检查点/恢复工具 |
| **Go** | ≥ 1.22 (编译用) | `go version` | 仅编译时需要 |

#### 推荐配置

| 资源 | 最小 | 推荐 |
|------|------|------|
| 管理节点 CPU | 2 核 | 4 核 |
| 管理节点内存 | 4 GB | 8 GB |
| GPU 节点 CPU | 16 核 | 32 核 |
| GPU 节点内存 | 64 GB | 128 GB |
| Checkpoint 存储 | 50 Gi | 200 Gi |
| 数据库存储 | 10 Gi | 20 Gi |

### 1.3 网络要求

| 端口 | 组件 | 协议 | 说明 |
|------|------|------|------|
| 8090 | API Server | HTTP/HTTPS | REST API 端口 |
| 8091 | API Server | HTTP | Metrics/Health 端口 |
| 80/443 | WebUI | HTTP/HTTPS | Web 管理界面 |
| 5432 | PostgreSQL | TCP | 计费数据库 |
| 10250 | Kubelet | gRPC | Device Plugin 通信 |
| 9400 | DCGM Exporter | HTTP | GPU 指标采集 |

---

## 2. 环境准备

### 2.1 操作系统配置

```bash
# 更新系统
sudo apt-get update && sudo apt-get upgrade -y   # Ubuntu/Debian
# 或
sudo yum update -y                                 # CentOS/RHEL

# 安装基础工具
sudo apt-get install -y curl wget git jq apt-transport-https ca-certificates

# 关闭 swap (Kubernetes 要求)
sudo swapoff -a
sudo sed -i '/ swap / s/^\(.*\)$/#\1/g' /etc/fstab

# 加载内核模块
cat <<EOF | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

# 设置网络参数
cat <<EOF | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF

sudo sysctl --system
```

### 2.2 安装 NVIDIA GPU 驱动

```bash
# 检查 GPU 硬件
lspci | grep -i nvidia

# Ubuntu — 使用 NVIDIA 官方源
sudo apt-get install -y linux-headers-$(uname -r)
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg

sudo apt-get update
sudo apt-get install -y nvidia-driver-525

# 重启并验证
sudo reboot
nvidia-smi
# 应显示驱动版本 ≥ 525 及 GPU 信息
```

### 2.3 安装 CUDA Toolkit

```bash
# 下载并安装 CUDA 12.x
wget https://developer.download.nvidia.com/compute/cuda/12.2.0/local_installers/cuda_12.2.0_535.54.03_linux.run
sudo sh cuda_12.2.0_535.54.03_linux.run --toolkit --silent

# 配置环境变量
echo 'export PATH=/usr/local/cuda-12.2/bin:$PATH' >> ~/.bashrc
echo 'export LD_LIBRARY_PATH=/usr/local/cuda-12.2/lib64:$LD_LIBRARY_PATH' >> ~/.bashrc
source ~/.bashrc

# 验证
nvcc --version
# cuda_12.2.xxx
```

### 2.4 安装 NVIDIA Container Toolkit

```bash
# 安装 nvidia-container-toolkit
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \
  sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg

curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
  sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
  sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit

# 配置 Docker/containerd 运行时
sudo nvidia-ctk runtime configure --runtime=containerd
sudo systemctl restart containerd

# 验证
nvidia-ctk --version
```

### 2.5 安装 Kubernetes 集群

```bash
# 安装 kubeadm/kubelet/kubectl (v1.29 示例)
sudo apt-get update
sudo apt-get install -y apt-transport-https ca-certificates curl gpg

curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.29/deb/Release.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg

echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.29/deb/ /' | \
  sudo tee /etc/apt/sources.list.d/kubernetes.list

sudo apt-get update
sudo apt-get install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

# 初始化集群 (Master 节点)
sudo kubeadm init --pod-network-cidr=10.244.0.0/16

# 配置 kubectl
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin-conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config

# 安装网络插件 (Calico 示例)
kubectl apply -f https://docs.projectcalico.org/manifests/calico.yaml

# Worker 节点加入集群
# 在 Worker 节点执行 (使用 kubeadm init 输出的 join 命令):
# sudo kubeadm join <master-ip>:6443 --token <token> --discovery-token-ca-cert-hash sha256:<hash>

# 标记 GPU 节点
kubectl label node <gpu-node-name> node-role.kubernetes.io/gpu=true

# 验证
kubectl get nodes
kubectl version
```

### 2.6 安装 Helm

```bash
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# 验证
helm version
```

### 2.7 安装 CRIU

```bash
# Ubuntu/Debian
sudo apt-get install -y criu

# CentOS/RHEL
sudo yum install -y criu

# 验证
criu --version
sudo criu check    # 检查内核支持
```

---

## 3. PhoenixGPU 安装

### 3.1 安装 CRD

PhoenixJob 自定义资源定义 (CRD) 必须先于 Helm 安装：

```bash
# 在线安装
kubectl apply -f https://raw.githubusercontent.com/wlqtjl/PhoenixGPU-/main/deploy/manifests/crd/phoenixjobs.yaml

# 或从本地仓库安装
kubectl apply -f deploy/manifests/crd/phoenixjobs.yaml

# 验证 CRD
kubectl get crd phoenixjobs.phoenixgpu.io
# NAME                       CREATED AT
# phoenixjobs.phoenixgpu.io  2026-04-05T03:00:00Z
```

**PhoenixJob CRD 核心字段**:

```yaml
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: my-training-job
spec:
  checkpoint:
    intervalSeconds: 300        # 自动 Checkpoint 间隔 (60-86400s)
    storageBackend: pvc         # pvc | s3 | nfs
    pvcName: phoenix-snapshots  # PVC 名称
    maxSnapshots: 5             # 最大快照保留数 (1-100)
    preDump: false              # 大模型建议开启 (>10GiB VRAM)
  restorePolicy:
    onNodeFailure: restore      # restore | restart | fail
    restoreTimeoutSeconds: 120  # 恢复超时
    maxRestoreAttempts: 3       # 最大恢复重试次数
  billing:
    department: "算法研究院"
    project: "LLM预训练"
    costCenter: "CC-001"
  template:
    spec:
      containers:
        - name: training
          image: pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime
          resources:
            limits:
              nvidia.com/vgpu: "4"
              nvidia.com/vgpu-memory: "32768"  # 32GiB
```

### 3.2 Helm 安装 PhoenixGPU

```bash
# 添加 Helm 仓库
helm repo add phoenixgpu https://wlqtjl.github.io/PhoenixGPU-/charts
helm repo update

# 安装（默认配置）
helm install phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system \
  --create-namespace \
  --wait \
  --timeout=5m

# 或从本地 Chart 安装
helm install phoenixgpu ./deploy/helm/phoenixgpu \
  --namespace phoenixgpu-system \
  --create-namespace \
  --wait
```

### 3.3 自定义配置

创建 `custom-values.yaml` 进行自定义：

```yaml
# ── 全局配置 ──────────────────────────────────
global:
  imageRegistry: ghcr.io/wlqtjl/phoenixgpu
  imagePullPolicy: IfNotPresent
  namespace: phoenixgpu-system

# ── Phoenix 控制器 ────────────────────────────
phoenixController:
  enabled: true
  replicaCount: 1
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits:   { cpu: 500m, memory: 512Mi }
  config:
    checkpointIntervalSeconds: 300   # 5 分钟自动 Checkpoint
    faultDetectorPollSeconds: 10     # 10 秒健康检查轮询
    notReadyThresholdSeconds: 30     # 30 秒判定节点故障
    maxRestoreAttempts: 3            # 最大恢复重试
    restoreTimeoutSeconds: 120       # 恢复超时 (60s SLA 目标)

# ── Device Plugin ─────────────────────────────
devicePlugin:
  enabled: true
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits:   { cpu: 200m, memory: 256Mi }
  config:
    resourceName: nvidia.com/vgpu
    memoryResourceName: nvidia.com/vgpu-memory

# ── 调度器扩展 ────────────────────────────────
schedulerExtender:
  enabled: true
  replicaCount: 1
  config:
    schedulingPolicy: binpack   # binpack（装箱优先）| spread（分散优先）
    numaAware: true             # NUMA 拓扑感知

# ── 计费引擎 ──────────────────────────────────
billingEngine:
  enabled: true
  config:
    collectionIntervalSeconds: 60  # 1 分钟采集间隔
    retentionDays: 90              # 90 天数据保留
  database:
    host: ""                       # 为空则部署内置 PostgreSQL
    port: 5432
    name: phoenixgpu
    secretName: phoenixgpu-db-secret

# ── API Server ────────────────────────────────
apiServer:
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits:   { cpu: 300m, memory: 256Mi }

# ── 数据库 (内置 PostgreSQL) ──────────────────
postgresql:
  enabled: true
  auth:
    database: phoenixgpu
  primary:
    persistence:
      size: 20Gi

# ── WebUI ─────────────────────────────────────
webui:
  enabled: true
  service:
    type: ClusterIP
    port: 80
  ingress:
    enabled: false
    className: nginx
    host: phoenixgpu.example.com

# ── Checkpoint 存储 ───────────────────────────
snapshotStorage:
  backend: pvc              # pvc | s3
  pvc:
    storageClass: ""        # 使用默认 StorageClass
    size: 200Gi
  s3:
    bucket: ""
    endpoint: ""
    secretName: phoenixgpu-s3-secret

# ── 监控 ──────────────────────────────────────
monitoring:
  enabled: true
  serviceMonitor:
    enabled: false          # 启用后需 Prometheus Operator
  dcgmExporter:
    enabled: true
    image: nvcr.io/nvidia/k8s/dcgm-exporter:3.3.5-3.4.0-ubuntu22.04
```

使用自定义配置安装：

```bash
helm install phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system \
  --create-namespace \
  -f custom-values.yaml \
  --wait --timeout=5m
```

### 3.4 验证安装

```bash
# 1. 检查所有 Pod 状态
kubectl get pods -n phoenixgpu-system
# 预期: 所有 Pod 为 Running 状态
# NAME                                    READY   STATUS    RESTARTS   AGE
# phoenixgpu-api-server-xxx               1/1     Running   0          2m
# phoenixgpu-controller-xxx               1/1     Running   0          2m
# phoenixgpu-device-plugin-xxx            1/1     Running   0          2m  (DaemonSet)
# phoenixgpu-scheduler-extender-xxx       1/1     Running   0          2m
# phoenixgpu-billing-engine-xxx           1/1     Running   0          2m
# phoenixgpu-webui-xxx                    1/1     Running   0          2m
# phoenixgpu-webhook-xxx                  1/1     Running   0          2m

# 2. 检查 CRD
kubectl get crd phoenixjobs.phoenixgpu.io

# 3. 检查 API Server 健康
kubectl port-forward -n phoenixgpu-system svc/phoenixgpu-api-server 8090:8090
curl http://localhost:8090/healthz       # {"status":"ok"}
curl http://localhost:8090/readyz        # {"status":"ok"}

# 4. 检查 API 端点
curl http://localhost:8090/api/v1/cluster/summary | jq .

# 5. 检查 WebUI
kubectl port-forward -n phoenixgpu-system svc/phoenixgpu-webui 3000:80
# 浏览器打开 http://localhost:3000

# 6. 检查 GPU 节点发现
kubectl get nodes -l node-role.kubernetes.io/gpu=true

# 7. 检查 RBAC
kubectl get clusterrole | grep phoenix
kubectl get clusterrolebinding | grep phoenix
```

---

## 4. 本地开发环境部署 (KinD)

适用于没有真实 GPU 硬件的开发和测试场景：

```bash
# 前置条件
# - Docker Desktop 或 Docker Engine
# - kind, kubectl, helm 已安装

# 一键部署
bash hack/kind-setup.sh

# 部署完成后:
# ┌──────────────────────────────────────────────┐
# │  WebUI:      http://localhost:3000            │
# │  API Server: http://localhost:8090/api/v1     │
# │  API Health: http://localhost:8090/healthz    │
# └──────────────────────────────────────────────┘
```

**KinD 集群配置详情**：

- 集群名称：`phoenixgpu-dev`
- 1 个 Control Plane 节点
- 2 个 Worker 节点（自动标记 `node-role.kubernetes.io/gpu=true`）
- 端口映射：`30090→8090` (API), `30080→3000` (WebUI)
- 使用 FakeK8sClient 提供模拟 GPU 数据
- DCGM Exporter 禁用（无真实 GPU）

```bash
# 重置环境
bash hack/kind-setup.sh --reset

# 删除集群
kind delete cluster --name phoenixgpu-dev

# 常用命令
kubectl get phoenixjobs -A                                        # 查看所有 PhoenixJob
kubectl logs -n phoenixgpu-system -l app.kubernetes.io/component=phoenix-controller -f  # 查看控制器日志
```

---

## 5. 生产环境部署

### 5.1 高可用部署

```yaml
# ha-values.yaml
phoenixController:
  replicaCount: 2                # 双副本
  resources:
    requests: { cpu: 500m, memory: 512Mi }
    limits:   { cpu: "1", memory: 1Gi }

apiServer:
  replicaCount: 2
  resources:
    requests: { cpu: 500m, memory: 256Mi }
    limits:   { cpu: "1", memory: 512Mi }

schedulerExtender:
  replicaCount: 2

billingEngine:
  replicaCount: 2
```

### 5.2 存储后端配置

#### PVC 存储（推荐用于单集群）

```yaml
snapshotStorage:
  backend: pvc
  pvc:
    storageClass: ceph-rbd     # 使用 Ceph 或其他高性能 SC
    size: 500Gi
```

#### S3 存储（推荐用于多集群/灾备）

```bash
# 创建 S3 凭证 Secret
kubectl create secret generic phoenixgpu-s3-secret \
  -n phoenixgpu-system \
  --from-literal=access-key=YOUR_ACCESS_KEY \
  --from-literal=secret-key=YOUR_SECRET_KEY
```

```yaml
snapshotStorage:
  backend: s3
  s3:
    bucket: phoenixgpu-snapshots
    endpoint: https://s3.amazonaws.com  # 或 MinIO 地址
    secretName: phoenixgpu-s3-secret
```

### 5.3 数据库配置

#### 使用外部 PostgreSQL/TimescaleDB

```bash
# 1. 创建数据库
psql -h <db-host> -U postgres -c "CREATE DATABASE phoenixgpu;"

# 2. 初始化 Schema
psql -h <db-host> -U postgres -d phoenixgpu -f deploy/db/001_initial_schema.sql

# 3. 创建数据库 Secret
kubectl create secret generic phoenixgpu-db-secret \
  -n phoenixgpu-system \
  --from-literal=BILLING_DB_DSN="postgres://user:pass@db-host:5432/phoenixgpu?sslmode=require"
```

```yaml
# Helm 配置
postgresql:
  enabled: false              # 禁用内置 PostgreSQL
billingEngine:
  database:
    host: db-host.example.com
    port: 5432
    name: phoenixgpu
    secretName: phoenixgpu-db-secret
```

**数据库 Schema 说明**（`deploy/db/001_initial_schema.sql`）：

| 表名 | 用途 | 关键字段 |
|------|------|---------|
| `usage_records` | GPU 使用记录时序表 | period_hour, gpu_model, alloc_ratio, tflops_hours, cost_cny |
| `quota_policies` | 配额策略表 | tenant_type, soft/hard_limit, period |
| `billing_alerts` | 计费告警表 | tenant_id, alert_type, severity |
| `daily_dept_summary` | 部门每日汇总（物化视图） | day, department, total_gpu_hours |
| `current_month_billing` | 当月计费视图 | department, gpu_hours, cost_cny |
| `quota_utilization` | 配额利用率视图 | tenant_id, used_pct |

### 5.4 监控配置

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true              # 需要 Prometheus Operator
  dcgmExporter:
    enabled: true
```

**PhoenixGPU Prometheus 指标**：

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `phoenixgpu_criu_dump_duration_seconds` | Histogram | CRIU Checkpoint 耗时 |
| `phoenixgpu_criu_dump_total` | Counter | CRIU Checkpoint 总次数 |
| `phoenixgpu_criu_restore_duration_seconds` | Histogram | CRIU Restore 耗时 |
| `phoenixgpu_criu_restore_total` | Counter | CRIU Restore 总次数 |
| `phoenixgpu_criu_snapshot_bytes` | Gauge | 快照大小 (字节) |

### 5.5 安全配置

```bash
# API Server 启用认证和 TLS
# 在 Helm values 中配置或通过命令行参数:
#   --auth-tokens <token1>,<token2>     Bearer Token 认证
#   --tls-cert /path/to/cert.pem        TLS 证书
#   --tls-key /path/to/key.pem          TLS 私钥
#   --rate-limit-rps 100                请求速率限制

# 创建 TLS Secret
kubectl create secret tls phoenixgpu-tls \
  -n phoenixgpu-system \
  --cert=tls.crt \
  --key=tls.key
```

> API Server 的 `/healthz`、`/readyz`、`/metrics` 端点不需要认证。

---

## 6. 升级与回滚

```bash
# 升级
helm repo update
helm upgrade phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system \
  -f custom-values.yaml \
  --wait --timeout=5m

# 回滚
helm rollback phoenixgpu 1 -n phoenixgpu-system

# 查看历史版本
helm history phoenixgpu -n phoenixgpu-system
```

---

## 7. 卸载

```bash
# 卸载 Helm release
helm uninstall phoenixgpu -n phoenixgpu-system

# 删除 CRD（注意：会删除所有 PhoenixJob 资源）
kubectl delete crd phoenixjobs.phoenixgpu.io

# 删除命名空间
kubectl delete namespace phoenixgpu-system

# 清理 PVC（如需要）
kubectl delete pvc -n phoenixgpu-system --all
```

---

## 8. 故障排查

### 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|---------|
| Device Plugin Pod CrashLoopBackOff | 节点无 GPU 或驱动未安装 | 检查 `nvidia-smi`；确认节点标签 |
| Checkpoint 失败 | CRIU 权限不足 | 确保 Pod 有 `CAP_SYS_PTRACE`；或以 root 运行 |
| Restore 超时 (>60s) | 快照文件过大或存储慢 | 启用 `preDump`；升级存储性能 |
| 计费数据为空 | 数据库连接失败 | 检查 DB Secret 和连接字符串 |
| WebUI 无法访问 | Service 类型未配置 | 使用 `kubectl port-forward` 或设置 Ingress |

### 日志查看

```bash
# 控制器日志
kubectl logs -n phoenixgpu-system -l app.kubernetes.io/component=phoenix-controller -f

# API Server 日志
kubectl logs -n phoenixgpu-system -l app.kubernetes.io/component=api-server -f

# Device Plugin 日志 (DaemonSet)
kubectl logs -n phoenixgpu-system -l app.kubernetes.io/component=device-plugin -f

# 计费引擎日志
kubectl logs -n phoenixgpu-system -l app.kubernetes.io/component=billing-engine -f

# 所有组件事件
kubectl get events -n phoenixgpu-system --sort-by='.metadata.creationTimestamp'
```

### 环境检查脚本

```bash
# 使用内置的预检脚本
bash hack/preflight-check.sh

# 环境变量映射到构建标签:
# PHOENIX_ENABLE_REAL_K8S=true    → k8sfull
# PHOENIX_ENABLE_MIGRATION=true   → migrationfull
# PHOENIX_ENABLE_CHECKPOINT=true  → checkpointfull
# PHOENIX_ENABLE_BILLING=true     → billingfull
```
