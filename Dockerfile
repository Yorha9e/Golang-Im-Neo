# ==========================================
# 阶段 1: 编译构建阶段 (Build Stage)
# ==========================================
FROM golang:1.22-alpine AS builder

# 国内网络加速：配置 Alpine 国内镜像源与 Go 模块国内代理
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache gcc musl-dev

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /build

# 优先拷贝 go.mod 和 go.sum 预热依赖缓存
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源代码并编译
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# ==========================================
# 阶段 2: 极简安全运行阶段 (Runtime Stage)
# ==========================================
FROM alpine:3.19

# 国内网络加速：配置 Alpine 国内镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

WORKDIR /app

# 从构建阶段复制编译好的独立二进制程序
COPY --from=builder /build/server /app/server
COPY config.toml /app/config.toml

# 预创建持久化数据目录与媒体目录
RUN mkdir -p /app/data/media

# 暴露容器内私有端口
EXPOSE 8080

# 默认启动命令
CMD ["/app/server", "-config", "/app/config.toml"]
