# Sprint 5 计划书 — 真实数据 + v0.1.0 发布

**周期**: 月3 第3-4周
**目标**: PhoenixGPU v0.1.0 正式发布，真实 K8s 集群可运行
**验收标准**:
  1. RealK8sClient 从 K8s API 读取真实 Node / PhoenixJob 数据
  2. DCGM Exporter 指标接入（GPU 利用率、显存、温度）
  3. GitHub Actions 自动构建并推送镜像到 GHCR
  4. `git tag v0.1.0 && git push --tags` 触发全自动发布流水线
  5. Helm Chart 发布到 GitHub Pages（artifact hub 可安装）
  6. README 发布公告级质量，可直接贴到技术论坛

---

## 任务列表

| # | 任务 | 工时 | 说明 |
|---|------|------|------|
| T41 | RealK8sClient — Node + Job 数据 | 5h | in-cluster config + CRD list |
| T42 | DCGM Prometheus 指标接入 | 4h | GPU util / VRAM / temp 从 Prometheus 查 |
| T43 | Nodes 页面真实数据对接 | 2h | WebUI Nodes 页完整实现 |
| T44 | GitHub Actions release 流水线 | 4h | tag 触发 → 构建 → push GHCR → Helm release |
| T45 | Helm Chart Pages 发布 | 2h | gh-pages 分支 + chart index |
| T46 | v0.1.0 CHANGELOG + Release Notes | 2h | GitHub Release 草稿 |
| T47 | README 发布级重写 | 3h | demo GIF 占位 + 安装步骤可运行 |
| T48 | 全链路集成测试 + Code Review | 4h | 覆盖真实 K8s 路径 |

**Sprint 5 总工时: 26h**

---

## 工程规约新增（Sprint 5）

- RealK8sClient 必须实现 graceful degradation：
  单个指标获取失败不能让整个 `/cluster/summary` 503
- DCGM 查询必须有缓存（TTL=15s），避免每个请求都打 Prometheus
- 所有对外 API 响应时间 P99 < 500ms（通过 Prometheus histogram 验证）
