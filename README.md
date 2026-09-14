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

```text
cmd/server/      服务端启动入口
internal/
  handshake/     [L1] 协议升级、Ticket 核销、Origin 校验
  gateway/       [L2] 32 分片连接池、读写泵、心跳、背压、硬件截断
  router/        [L3] 消息调度、Relay/P2P 策略、拦截器链
  logic/         [L4] 纯业务领域服务 (auth/user/friend/group/media/
                 message/profile/session/admin)
  store/         [L5] Repository、SQLite WAL、异步批量落盘
  middleware/    HTTP 中间件 (JWT, AdminAuth, CORS, Recovery)
  config/        config.toml 解析与种子管理员注入
proto/           消息协议 Protobuf 定义及生成代码 (SSOT)
esp32/           ESP-IDF 硬件客户端原型工程
frontend/        React / TypeScript 桌面 Web 工程
mobile/          Capacitor 移动端工程
nginx/           Nginx 防护网关配置
landing/         项目介绍页 (GitHub Pages)
```
