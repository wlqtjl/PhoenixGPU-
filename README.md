<div align="center">

**GPU 故障，训练不中断。**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](go.mod)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.26+-326CE5?logo=kubernetes)](https://kubernetes.io)
[![CUDA](https://img.shields.io/badge/CUDA-12.x-76B900?logo=nvidia)](https://developer.nvidia.com/cuda-toolkit)
[![CI](https://github.com/wlqtjl/PhoenixGPU-/actions/workflows/ci.yml/badge.svg)](https://github.com/wlqtjl/PhoenixGPU-/actions)
[![Release](https://img.shields.io/github/v/release/wlqtjl/PhoenixGPU-)](https://github.com/wlqtjl/PhoenixGPU-/releases)

[**快速开始**](#-快速开始) · [架构设计](docs/architecture/ARCHITECTURE.md) · [贡献指南](CONTRIBUTING.md) · [CHANGELOG](CHANGELOG.md)

</div>

---

## 🔥 为什么是 PhoenixGPU？

高校与科研机构的 GPU 集群有一个共同噩梦：

> **深夜跑了 72 小时的实验，节点宕机，全部归零。**

PhoenixGPU 是第一个内置完整 Checkpoint/Restore 能力的开源 GPU 虚拟化平台。节点宕机后，训练任务像不死鸟一样从灰烬中复活，从断点继续运行。

**总恢复时间 < 60 秒，无需修改任何训练代码。**

## 📊 与竞品对比

| 能力 | HAMi | Dynamia (商业) | **PhoenixGPU** |
|------|:----:|:--------------:|:--------------:|
| GPU 虚拟化（VRAM + SM 隔离） | ✅ | ✅ | ✅ |
| 故障自动 Checkpoint/Restore | ❌ | ❌ | **✅** |
| 断点续训 | ❌ | ❌ | **✅** |
| 多租户计费（TFlops·h） | ❌ | 商业版 | **✅** |
| 可视化控制台 | 基础 | 商业版 | **✅** |
| 开源协议 | Apache 2.0 | 闭源 | **Apache 2.0** |

## ✨ 核心特性

### PhoenixHA — GPU 故障零中断

```yaml
apiVersion: phoenixgpu.io/v1alpha1
kind: PhoenixJob
metadata:
  name: llm-pretrain
spec:
  checkpoint:
    intervalSeconds: 300      # 每 5 分钟自动 Checkpoint
    storageBackend: pvc
    pvcName: checkpoint-store
  template:
    spec:
      containers:
      - name: trainer
        image: pytorch/pytorch:2.3.0-cuda12.1
        resources:
          limits:
            nvidia.com/vgpu: "1"
            nvidia.com/vgpu-memory: "8192"
```

PhoenixGPU 自动处理一切：节点宕机 → 60s 内恢复 → 训练从断点继续。

### 多租户计费（TFlops·h 统一货币）

```
TFlops·h = AllocRatio × GPUSpec.FP16TFlops × DurationHours

A100 50% × 2h  = 0.5 × 312  × 2 = 312 TFlops·h
H800  25% × 2h = 0.25 × 2000 × 2 = 1000 TFlops·h
```

## 🚀 快速开始

```bash
# 安装（Helm）
helm repo add phoenixgpu https://wlqtjl.github.io/PhoenixGPU-/charts
helm repo update
helm install phoenixgpu phoenixgpu/phoenixgpu \
  --namespace phoenixgpu-system --create-namespace --wait

# 本地开发（Kind）
git clone https://github.com/wlqtjl/PhoenixGPU-.git && cd PhoenixGPU-
bash hack/kind-setup.sh
# WebUI: http://localhost:3000  API: http://localhost:8090
```

## 🏗️ 架构

```
PhoenixHA (CRIU Checkpoint/Restore)
GPU Virtualization (HAMi Enhanced — VRAM/SM 隔离)
Billing Engine (TFlops·h 计量 · 配额 · 告警)
REST API Server (:8090) + WebUI (:80)
```

详见 [docs/architecture/ARCHITECTURE.md](docs/architecture/ARCHITECTURE.md)

## 📊 项目现状

- 53 个文件 · ~8,000 行代码 · 50+ 个测试
- Go 1.22 · React 18 · TypeScript · Kubernetes 1.26+
- 积极开发中，欢迎高校/科研机构早期试用

## 🙏 致谢

基于 [HAMi](https://github.com/Project-HAMi/HAMi)（Apache 2.0）和
[SJTU PhoenixOS](https://github.com/SJTU-IPADS/PhoenixOS) 的思路。

## License

[Apache 2.0](LICENSE) — 企业友好，商业使用无限制。
