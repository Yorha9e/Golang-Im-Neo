# Golang IM Neo System

工业级分层架构 IM 系统 — 中心化高并发代理 + STUN/P2P 预留 + ESP32-C3 极限制约适配。

## Gate-0 已完成 (Day 1 铁律)

- [x] Go module `golang-im-neo-system` 与核心依赖 (gin, gorilla/websocket, protobuf, gorm, sqlite, jwt, toml, zap, bcrypt, uuid)
- [x] `proto/message.proto` 与 `proto/message.options` (Nanopb Zero-malloc, 9字段含 stanza_id, 12种 MsgType, max_size bounds)
- [x] `config.toml` 配置体系与 SuperAdmin 种子注入 (bcrypt Cost=10, Root vs Admin 分离)
- [x] 内存会话发号器 `internal/logic/session.Allocator` (Sync-DB-First, 步长1000持久化水线, 原子发号 <1µs)
- [x] 持久化与安全埋点: `PRAGMA synchronous=FULL` + WAL + 单写池 `MaxOpenConns=1` + 网关 `select default` 背压熔断 + `SetReadLimit(64KB)`

## 快速开始

```bash
go mod tidy
go run ./cmd/server
```

## 协议

`proto/message.proto` 为 SSOT, 编译:

```bash
protoc --go_out=. --go_opt=paths=source_relative proto/message.proto
```

## 配置

`config.toml` 包含 `[server]`, `[security]`, `[database]`, `[admin]` 四区块, 启动时自动加载并通过 bcrypt 注入 SuperAdmin。

## 目录结构

见 `docs/TECH_STACK_SPECIFICATION.md` 与 `docs/DATABASE_DESIGN_SPEC.md`
