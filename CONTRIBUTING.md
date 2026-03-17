# Contributing to PhoenixGPU

首先，感谢你考虑为 PhoenixGPU 做贡献！

## 快速开始

### 开发环境

```bash
# 1. Fork 并 clone 仓库
git clone https://github.com/YOUR_USERNAME/PhoenixGPU-.git
cd PhoenixGPU-

# 2. 安装依赖
go mod download

# 3. 运行测试（确保 CI 绿灯）
go test ./...

# 4. 在新分支上开发
git checkout -b feat/your-feature
```

### 提交规范

遵循 [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(checkpoint): add pre-dump support for large models
fix(device-plugin): handle missing GPU annotation gracefully
docs(readme): add KinD local dev setup guide
test(fault-detector): add recovery cycle test
```

### Pull Request 流程

1. 确保所有测试通过：`go test ./... -race`
2. 运行 lint：`golangci-lint run`
3. 更新相关文档
4. 在 PR 描述中说明改动原因和测试方法

## 架构原则

- **TDD**：先写测试，再写实现
- **YAGNI**：不实现"可能将来会用到"的功能
- **DRY**：抽象重复逻辑，但不过度抽象
- **自研 vs 复用**：核心壁垒（Checkpoint、HA、Billing）自研；基础设施复用开源项目

## 需要帮助？

- 📖 [开发指南](docs/dev-guide/)
- 💬 提 Issue 讨论
