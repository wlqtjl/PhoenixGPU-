# Sprint 4 计划书 — API Server + Helm + 本地部署

**周期**: 月3 第1-2周
**目标**: PhoenixGPU 在 Kind 集群上一键可运行，WebUI 对接真实 API
**验收标准**:
  1. `make kind-up && make helm-install` 后 WebUI 可访问
  2. `/api/v1/cluster/summary` 返回真实数据
  3. `/api/v1/jobs` 返回真实 PhoenixJob 列表
  4. Helm Chart `helm lint` 通过，`helm template` 无错

---

## 任务列表

| # | 任务 | 工时 | 说明 |
|---|------|------|------|
| T31 | REST API Server 骨架（Gin + 路由注册）| 3h | cmd/api-server/main.go |
| T32 | Cluster & Nodes API handler | 4h | GET /cluster/summary, /nodes |
| T33 | Jobs API handler | 4h | GET /jobs, GET /jobs/:ns/:name, POST checkpoint |
| T34 | Billing API handler | 3h | GET /billing/departments, /billing/records |
| T35 | Alerts API handler | 2h | GET /alerts, POST /alerts/:id/resolve |
| T36 | Helm Chart 完整模板 | 5h | Deployment × 4 + Service + RBAC + ConfigMap |
| T37 | Kind 本地开发脚本 | 2h | hack/kind-setup.sh，完整可运行 |
| T38 | Dockerfile × 4 | 3h | 多阶段构建，镜像最小化 |
| T39 | API 集成测试 | 3h | httptest，覆盖所有 handler |
| T40 | Code Review（两阶段）| 2h | 规范 + 安全 |

**Sprint 4 总工时: 31h**

---

## 关键设计决策

### API 认证（MVP阶段）
Bearer Token 静态认证（Kubernetes ServiceAccount Token）
不做 OAuth/OIDC（YAGNI — V2 功能）

### K8s 数据源
API Server 通过 in-cluster config 读取真实 K8s 资源：
- PhoenixJob → CRD List
- Nodes → corev1.NodeList + DCGM metrics
- Jobs status → PhoenixJob.Status

### 工程规约新增约束（Sprint 4）
- 所有 HTTP handler 必须有请求超时（默认 10s）
- API 响应统一结构：`{ data, error, meta }`
- 所有 handler 必须记录 zap 结构化日志
- Prometheus metrics：请求数、延迟、错误率
