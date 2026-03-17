# Sprint 7-8-9 计划书

## Sprint 7 — 热迁移完整实现（月4 第3-4周）
**验收**: 同集群两节点间迁移运行中 CUDA 进程，E2E 测试通过，v0.2.0-alpha 发布

| # | 任务 | 工时 |
|---|------|------|
| T57 | K8s exec API 替换 kubectl（remotecommand.SPDY）| 5h |
| T58 | 热迁移 E2E 测试（mock + 真实 Kind 路径）| 6h |
| T59 | LiveMigration API handler + WebUI 触发按钮 | 4h |
| T60 | v0.2.0-alpha release notes + tag | 2h |
| T61 | Code Review（两阶段）| 2h |

## Sprint 8 — 社区增长（月5 第1-2周）
**验收**: 技术博客草稿 + KubeCon 摘要 + 高校 PoC 方案文档

| # | 任务 | 工时 |
|---|------|------|
| T62 | 技术博客：《GPU 故障零中断 — PhoenixGPU 的 Checkpoint 原理》| 3h |
| T63 | KubeCon Talk 提案（摘要 + 大纲）| 2h |
| T64 | 高校 PoC 部署方案文档（一键安装 + 验收脚本）| 3h |
| T65 | MAINTAINERS.md + CODE_OF_CONDUCT.md | 1h |
| T66 | Adopters.md 首批用户模板 + 招募邮件 | 1h |

## Sprint 9 — VRAM 超分配（月5 第3-4周）
**验收**: 同张 GPU 上两个任务共用 VRAM，总申请超出物理容量，LRU 换页透明工作

| # | 任务 | 工时 |
|---|------|------|
| T67 | VRAM 超分配架构设计 + TDD 测试 | 4h |
| T68 | libvgpu 换页引擎（cuMemAlloc Swap to CPU）| 8h |
| T69 | LRU 页面置换策略 | 4h |
| T70 | 性能基准测试（密度 vs 延迟权衡）| 3h |
| T71 | 全链路 Code Review | 3h |

**工程规约新增约束（Sprint 9）**:
- Swap 操作不得持有全局锁（会阻塞所有 CUDA 调用）
- LRU 元数据使用 atomic 操作，不用 Mutex
- 换页失败必须回退，不能导致应用 SIGSEGV
