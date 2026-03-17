# UPSTREAM.md — HAMi Fork Tracking

本文档追踪 PhoenixGPU 与上游 HAMi 项目的分叉关系。

**上游项目**: https://github.com/Project-HAMi/HAMi  
**上游协议**: Apache 2.0  
**分叉基准版本**: HAMi v2.4.0 (commit: 待填入)  
**分叉日期**: 2025-03-17

---

## 文件策略说明

| 标记 | 含义 |
|------|------|
| `[KEEP]` | 直接保留，追踪上游更新 |
| `[MODIFIED]` | 基于 HAMi 修改，需标注 diff |
| `[REPLACED]` | 完全替换为 PhoenixGPU 自研实现 |
| `[DELETED]` | 在 PhoenixGPU 中删除 |
| `[NEW]` | PhoenixGPU 新增，不存在于 HAMi |

---

## 文件分类清单

### libvgpu（CUDA 拦截层）

| 文件 | 策略 | 说明 |
|------|------|------|
| `libvgpu/src/hook.c` | `[MODIFIED]` | 保留拦截框架，新增 TFlops 计量逻辑 |
| `libvgpu/src/cuda/hook.c` | `[MODIFIED]` | 新增 cuLaunchKernel 计时统计 |
| `libvgpu/src/nvml/hook.c` | `[KEEP]` | NVML 伪装逻辑基本不变 |
| `libvgpu/include/libvgpu.h` | `[MODIFIED]` | 新增 Phoenix 扩展接口 |

### cmd/device-plugin

| 文件 | 策略 | 说明 |
|------|------|------|
| `cmd/device-plugin/main.go` | `[MODIFIED]` | 新增 Checkpoint 注解注入 |
| `cmd/device-plugin/device.go` | `[MODIFIED]` | 新增 PhoenixJob 资源识别 |
| `cmd/device-plugin/server.go` | `[KEEP]` | gRPC server 基本不变 |

### cmd/scheduler-extender

| 文件 | 策略 | 说明 |
|------|------|------|
| `cmd/scheduler-extender/filter.go` | `[MODIFIED]` | 新增 HA 感知过滤（排除已故障节点）|
| `cmd/scheduler-extender/score.go` | `[MODIFIED]` | 新增 Checkpoint 存储亲和性评分 |

### cmd/webhook

| 文件 | 策略 | 说明 |
|------|------|------|
| `cmd/webhook/handler.go` | `[MODIFIED]` | 新增 PhoenixJob annotation 注入 |

---

## 新增模块（PhoenixGPU 独有）

| 模块 | 路径 | 说明 |
|------|------|------|
| Phoenix HA Controller | `pkg/hacontroller/` | 故障检测 + 自动重调度 |
| Checkpoint Engine | `pkg/checkpoint/` | CRIU 封装 + Snapshot 管理 |
| Billing Engine | `pkg/billing/` | TFlops·h 计量 + 配额管理 |
| WebUI | `webui/` | React 控制台 |
| PhoenixJob CRD | `deploy/manifests/crd/` | 自定义资源定义 |

---

## 同步上游的流程

```bash
# 1. 添加上游 remote（首次）
git remote add upstream https://github.com/Project-HAMi/HAMi.git

# 2. 获取上游更新
git fetch upstream

# 3. 查看上游变更
git log upstream/main --oneline | head -20

# 4. 对 [KEEP] 文件进行 cherry-pick 或 merge
# 注意：[REPLACED] 和 [NEW] 文件不要同步

# 5. 更新本文档的 "上游基准版本"
```

---

## 重要说明

> PhoenixGPU 遵循 Apache 2.0 协议使用 HAMi 代码。  
> 所有修改均在文件头部标注 `// Modified by PhoenixGPU`。  
> 原始版权声明 `// Copyright HAMi Authors` 予以保留。
