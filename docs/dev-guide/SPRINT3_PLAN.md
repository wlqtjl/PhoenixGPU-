# Sprint 3 计划书 — Phoenix WebUI

**周期**: 月2 第3-4周
**目标**: 可运行的 React 控制台，覆盖 Dashboard / Jobs / Billing 三个核心页面
**验收标准**: 管理员通过 UI 能看到集群 GPU 状态、任务 Checkpoint 历史、部门费用报表

---

## 技术选型（YAGNI 原则约束）

| 层次 | 选型 | 理由 |
|------|------|------|
| 框架 | React 18 + TypeScript | 类型安全 + 生态最成熟 |
| 构建 | Vite | 比 CRA 快 10×，零配置 |
| 路由 | React Router v6 | 行业标准 |
| 状态 | Zustand | 比 Redux 轻 90%，够用 |
| 图表 | Recharts | 与 React 深度集成 |
| HTTP | Axios + React Query | 请求缓存 + 自动刷新 |
| 样式 | CSS Modules + CSS Variables | 无运行时开销 |
| 测试 | Vitest + Testing Library | 与 Vite 原生集成 |

**不引入**（YAGNI）：Redux、GraphQL、Storybook、i18n（V2）

---

## 任务列表

| # | 任务 | 工时 | 优先级 |
|---|------|------|--------|
| T23 | 项目脚手架 + 设计系统（CSS变量/主题）| 3h | P0 |
| T24 | API Client 层（Axios + React Query + 类型定义）| 3h | P0 |
| T25 | 布局框架（Sidebar + Header + 路由）| 2h | P0 |
| T26 | Dashboard 页面（集群总览 + GPU 利用率图表）| 6h | P0 |
| T27 | Jobs 页面（PhoenixJob 列表 + Checkpoint 历史）| 6h | P1 |
| T28 | Billing 页面（部门费用 + 配额进度条）| 5h | P1 |
| T29 | 组件单元测试（Dashboard + Jobs 关键组件）| 3h | P1 |
| T30 | 代码审查 + 可访问性检查 | 2h | P0 |

**Sprint 3 总工时: 30h**

---

## 审美规约（Engineering Covenant 补充）

- **主题**：暗色工业精密风（Dark Industrial Precision）
- **主色**：`#0D0F12` 背景，`#F59E0B` 琥珀色强调，`#10B981` 成功绿
- **字体**：`JetBrains Mono` 显示数字/代码，`DM Sans` 正文
- **原则**：数据密度优先，无装饰性元素，动效只服务于状态变化
- **禁止**：Inter / Roboto / purple-gradient-on-white
