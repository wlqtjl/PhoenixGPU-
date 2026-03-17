# Sprint 6 计划书 — 真实计费存储 + 热迁移 + CNCF 申请

**周期**: 月4 第1-2周
**目标**: PhoenixGPU 从 MVP 走向生产就绪，具备参与 CNCF Sandbox 的技术门槛
**验收标准**:
  1. TimescaleDB 真实写入计费记录，WebUI Billing 页展示真实数据
  2. GPU 热迁移 PoC：同一集群内两节点间迁移运行中的 CUDA 进程（≥ 1 GPU 型号）
  3. CNCF Sandbox 申请材料草稿完成（Due Diligence 问卷 + Adopters.md）
  4. v0.2.0-alpha 打 tag，热迁移特性作为 alpha 发布

---

## 任务列表

| # | 任务 | 工时 | 优先级 |
|---|------|------|--------|
| T49 | TimescaleDB Schema + 迁移脚本 | 4h | P0 |
| T50 | BillingStore PostgreSQL 实现 | 5h | P0 |
| T51 | Billing API 接入真实 DB | 2h | P0 |
| T52 | GPU 热迁移架构设计 + PoC | 8h | P0 |
| T53 | LiveMigration Controller | 6h | P1 |
| T54 | CNCF Sandbox 申请材料 | 4h | P1 |
| T55 | Adopters.md + 社区运营材料 | 2h | P2 |
| T56 | 全链路 Code Review | 3h | P0 |

**Sprint 6 总工时: 34h**

---

## 关键设计决策

### TimescaleDB 选型原因
- 原生时序压缩（计费记录 90 天保留，压缩率 ~20×）
- 兼容 PostgreSQL 协议（无需新驱动）
- 超表（Hypertable）自动分区，按月分区匹配计费周期

### 热迁移架构（PoC 范围）
V1 仅支持同集群内节点间迁移（不跨集群）。
迁移流程：
  1. Pre-dump（不中断进程）
  2. 目标节点准备（Pod 预创建、PVC 挂载）
  3. Full dump + 进程暂停（冻结窗口 < 5s 目标）
  4. Snapshot 传输（直接节点间 rsync，不走 S3）
  5. 目标节点 CRIU Restore
  6. 源节点 Pod 清理

### CNCF Sandbox 门槛
- ≥ 1 个 Sponsor（TOC 成员）
- ≥ 3 个独立生产用户
- 完整的 Security Audit 自评
- 活跃的社区治理（CODE_OF_CONDUCT + GOVERNANCE）
