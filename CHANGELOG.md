# Changelog

All notable changes to PhoenixGPU will be documented here.
Format: [Semantic Versioning](https://semver.org/)

---

## [v0.1.0] — 2026-03-17

### 🎉 首个公开发布

PhoenixGPU 是第一个内置完整 **Checkpoint/Restore** 能力的开源 GPU 虚拟化平台。
节点宕机后，训练任务像不死鸟一样从灰烬中复活，从断点继续运行。

### ✨ 新增特性

#### PhoenixHA — GPU 故障零中断
- 基于 **CRIU v3.x** 的进程级 Checkpoint/Restore
- 节点宕机后 **< 60 秒**自动迁移到健康节点，断点续训
- 可配置 Checkpoint 间隔（默认 5 分钟）
- 支持 PVC 和 S3 两种 Snapshot 存储后端
- `io.Pipe` 零磁盘二次拷贝 S3 上传
- 上传失败时本地快照保留，下次自动重传

#### GPU 虚拟化（基于 HAMi 强化）
- VRAM 硬隔离：超限即 `CUDA_ERROR_OUT_OF_MEMORY`
- SM 占用率软限速
- NVML 伪装（应用"看到"虚拟 GPU）
- Kubernetes Device Plugin + MutatingWebhook 零侵入注入

#### 多租户计费中心
- **TFlops·h** 统一算力货币（跨 GPU 型号完全可比）
- 部门 → 项目 → 任务三级成本归因
- 配额软/硬限制 + 80% 预警
- 支持 5 种 GPU 型号定价（H800 / A100 80G / A100 40G / RTX 4090 / 昇腾 910B）

#### REST API Server
- 10 个 HTTP handler，统一 `APIResponse` 封装
- 每个 handler 独立 10s 超时
- Prometheus 指标：请求数、延迟分布、错误率
- 支持 `--mock` 模式（无 K8s 环境可运行）

#### PhoenixGPU WebUI
- Dark Industrial Precision 主题（JetBrains Mono + DM Sans）
- Dashboard：集群总览 + 24h 利用率历史 + 费用饼图
- Jobs：PhoenixJob 列表 + Checkpoint 可视化 + 手动触发
- Billing：部门配额进度条 + TFlops·h 解释
- Alerts：活跃告警列表 + 一键 Resolve

#### 部署
- Helm Chart（一键安装）
- 多阶段 Dockerfile（最终镜像 < 20MiB）
- Kind 本地开发脚本（`bash hack/kind-setup.sh`）
- GitHub Actions CI/CD（lint → test → build → release）

### 📊 项目统计
- **53 个文件** · **~7,700 行代码** · **50 个测试用例**
- Go 1.22 · React 18 · TypeScript · Kubernetes 1.26+

### ⚠️ 已知限制
- RealK8sClient 的 DCGM 指标接入处于 Beta（需要 DCGM Exporter 已部署）
- GPU 热迁移（Live Migration）计划在 v0.2.0
- VRAM 超分配（Swap to CPU）计划在 v0.3.0
- 异构 GPU（华为昇腾）调度计划在 v0.4.0

### 🙏 致谢
GPU 虚拟化层基于 [HAMi](https://github.com/Project-HAMi/HAMi)（Apache 2.0）
Checkpoint 方案参考 [SJTU PhoenixOS](https://github.com/SJTU-IPADS/PhoenixOS)

---

## [Unreleased]

### Planned for v0.2.0
- GPU 热迁移（基于 CUDA context 序列化）
- 动态 Worker Pool（自适应 S3 上传并发）
- LDAP/OIDC 认证集成
- 多集群管理面板

[v0.1.0]: https://github.com/wlqtjl/PhoenixGPU-/releases/tag/v0.1.0
