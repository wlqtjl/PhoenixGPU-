# API Server 质量保障落地清单（可执行）

> 目标：把“八大检测维度 + 六层质量金字塔”转成可直接运行的命令与 CI 草案，优先覆盖 `cmd/api-server`。

## 1) 本地执行命令清单

### Level 1：静态代码分析
```bash
# Go 基础静态检查
go vet ./cmd/api-server/...

# 如本地已安装 golangci-lint
# golangci-lint run ./cmd/api-server/... --timeout=5m

# 如本地已安装 gosec
# gosec ./cmd/api-server/...
```

### Level 2：单元测试与覆盖率
```bash
# 单测 + 覆盖率
make quality-api-server

# 或直接运行
go test ./cmd/api-server -count=1 -coverprofile=coverage-api-server.out
go tool cover -func=coverage-api-server.out | tail -1
```

### Level 3：并发安全
```bash
make quality-api-server-race
# 或直接：
go test -race ./cmd/api-server -count=1
```

### Level 4：性能与压力（基准）
```bash
make quality-api-server-bench
# 或直接：
go test ./cmd/api-server -run '^$' -bench . -benchmem -count=1
```

### Level 5：集成/E2E（依赖集群）
```bash
# 需要 kind / k8s 环境
make kind-up
make test-e2e
```

### Level 6：生产可观测性与故障检测
- 日志：统一输出 request method/path/status/duration。
- 指标：建议补齐 `http_requests_total`、`http_request_duration_seconds`（按 path/method/status 标签）。
- 告警：5xx 比例、p95 延迟、关键接口错误率。

---

## 2) 八大检测维度映射（建议最小闭环）

1. **代码质量**：`go vet` + `golangci-lint`。
2. **功能正确性**：`go test ./cmd/api-server` + 覆盖率门禁。
3. **并发安全**：`-race`。
4. **性能指标**：`-bench`，记录基线。
5. **安全性**：`gosec` + 依赖扫描（CI 可加 `govulncheck`）。
6. **可靠性**：E2E + 错误注入（后续可加故障恢复场景）。
7. **兼容性**：保留历史 flag（如 `--metrics-addr`）并加入启动参数测试。
8. **可观测性**：日志字段标准化 + 指标 + tracing 预留。

---

## 3) 推荐执行顺序（快速推进）

1. `make quality-api-server`
2. `make quality-api-server-race`
3. `make quality-api-server-bench`
4. （有集群时）`make test-e2e`

