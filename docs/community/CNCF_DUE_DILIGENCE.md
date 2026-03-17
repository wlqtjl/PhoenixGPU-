# PhoenixGPU — CNCF Sandbox Due Diligence

> 申请状态：草稿 (Draft) — 目标提交时间：v0.3.0 发布后
> 参考模板：https://github.com/cncf/toc/blob/main/process/due-diligence-guide.md

---

## 项目基本信息

| 字段 | 内容 |
|------|------|
| **项目名称** | PhoenixGPU |
| **官网 / 仓库** | https://github.com/wlqtjl/PhoenixGPU- |
| **License** | Apache 2.0 |
| **语言** | Go (后端) · TypeScript/React (前端) · C (libvgpu) |
| **成立时间** | 2026年3月 |
| **申请类别** | CNCF Sandbox |

---

## 1. Alignment with CNCF Mission

PhoenixGPU 解决云原生 AI 基础设施中一个关键未满足的需求：
**GPU 工作负载在 Kubernetes 上的高可用性**。

现有 GPU 调度方案（NVIDIA Device Plugin、HAMi、KAI Scheduler）均聚焦于资源分配，
无法处理节点故障导致的训练任务中断。PhoenixGPU 通过 CRIU 检查点技术，
将 GPU 训练任务从"有状态单点故障"升级为"可恢复的弹性工作负载"，
直接推进 CNCF "Make cloud native computing ubiquitous" 的使命。

**CNCF 技术栈集成**：
- Kubernetes（核心平台，CRD + Device Plugin + Scheduler Extender）
- Prometheus（所有关键路径均有 metrics，DCGM 集成）
- 兼容 KubeRay、Kubeflow、Volcano（AI 训练框架）

---

## 2. 项目范围与价值主张

### 解决的问题
GPU 集群中，节点硬件故障、驱动崩溃、内核 OOM 等导致的训练中断：
- 典型损失：72小时训练 × 数十张 A100 × ¥35/h = **¥75,600/次中断**
- 受影响群体：高校、科研机构、AI 创业公司（中小型 GPU 集群尤甚）

### 核心差异化
| 能力 | PhoenixGPU | 竞品 |
|------|-----------|------|
| CRIU GPU Checkpoint/Restore | ✅ | ❌ (所有已知开源方案) |
| < 60s 故障自动恢复 | ✅ | ❌ |
| TFlops·h 标准化计费 | ✅ | 部分商业方案 |
| Apache 2.0 开源 | ✅ | 商业方案均闭源 |

---

## 3. 生产用户（Adopters）

> ⚠️ 当前状态：寻找早期采用者中
> CNCF Sandbox 要求：≥ 3 个独立生产用户

**目标早期用户（接触中）**：
- 国内高校 GPU 集群（计划 v0.2.0 后开始试点）
- 开源 AI 研究机构

**填充计划**：v0.2.0（热迁移稳定）后主动联系高校 HPC 中心，
提供 PoC 支持换取 Adopters.md 背书。

---

## 4. 成熟度评估

### 代码成熟度
- 测试覆盖率目标：核心包（checkpoint / billing / migration）> 80%
- CI 绿灯：lint + unit + integration
- 依赖扫描：Dependabot 已配置

### 安全态势
- RBAC 最小权限原则（ClusterRole 仅声明必要 verb）
- 无已知 CVE（Trivy 扫描镜像）
- 密钥管理：通过 K8s Secret，不硬编码
- 计划：v0.3.0 前完成 Security Self-Assessment

### 治理
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)：Contributor Covenant v2.1
- [GOVERNANCE.md](GOVERNANCE.md)：草稿中
- [SECURITY.md](SECURITY.md)：漏洞报告流程
- Maintainer 决策：PR 需 2 个 maintainer approve

---

## 5. 竞品与生态分析

### 直接竞品
| 项目 | 关系 |
|------|------|
| HAMi (CNCF Sandbox) | PhoenixGPU 的虚拟化层基于 HAMi，属于增强关系而非竞争 |
| NVIDIA KAI Scheduler | 调度层互补，PhoenixGPU 专注 HA，KAI 专注调度策略 |
| Dynamia.ai | 商业闭源竞品，PhoenixGPU 是其开源替代 |

### 与 CNCF 项目的关系
- **依赖**：Kubernetes、Prometheus、Helm
- **互补**：HAMi（共存）、Volcano（可用作调度后端）、KubeRay（目标用户重叠）
- **无冲突**：不与任何 CNCF 项目功能直接重叠

---

## 6. 路线图

| 版本 | 时间 | 主要特性 |
|------|------|----------|
| v0.1.0 | 2026-03 | 基础虚拟化 + Checkpoint/Restore MVP + 计费 |
| v0.2.0 | 2026-05 | GPU 热迁移（Live Migration） + 真实 DB |
| v0.3.0 | 2026-07 | VRAM 超分配 + 异构 GPU（昇腾）|
| v1.0.0 | 2026-12 | 生产稳定 · CNCF 正式申请 |

---

## 7. CNCF TOC Sponsor 寻找计划

> 需要 ≥ 1 位 CNCF TOC 成员作为 Sponsor

行动计划：
1. 在 KubeCon 2026 EU 提交 Talk（PhoenixGPU: GPU HA for K8s）
2. 参与 CNCF Slack #wg-batch 和 #sig-scheduling 讨论
3. 向 HAMi maintainer（已与 PhoenixGPU 有代码关联）寻求引荐
4. 发布技术博客到 CNCF Blog
