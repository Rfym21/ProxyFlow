# 使用官方 Golang 镜像作为构建环境
FROM golang:1.24-alpine AS build

# 设置工作目录
WORKDIR /app

# 先复制 go.mod 和 go.sum 文件以便更好地利用缓存
COPY go.mod go.sum* ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用程序
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/proxyflow

# 创建最小化的生产镜像
FROM alpine:latest

# 创建应用目录并设置权限
WORKDIR /app
COPY --from=build /app/main .

# 创建空的 .env 文件
RUN touch .env

# 运行可执行文件
CMD ["./main"]