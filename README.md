# ProxyFlow

<div align="center">

![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey?style=for-the-badge)
[![Telegram](https://img.shields.io/badge/Telegram-blue?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/nodejs_project)

**Language:** [中文](README.md) | [English](docs/README.en.md)

</div>

## 🚀 功能特性

- ✅ **双协议支持** - HTTP/HTTPS代理完整支持
- ✅ **上游代理认证** - 支持Basic Auth认证的上游代理
- ✅ **智能负载均衡** - 轮询算法自动分配请求
- ✅ **连接池管理** - 高效的连接复用机制

## 📦 快速开始

### 1️⃣ 克隆项目

```bash
git clone https://github.com/Rfym21/ProxyFlow.git
cd ProxyFlow
```

### 2️⃣ 配置代理列表

编辑 `proxy.txt` 文件，每行一个代理：

```txt
# 代理列表文件
# 格式: http://username:password@ip:port
# 空行和以#开头的行将被忽略

http://user1:pass1@192.168.1.100:8080
http://user2:pass2@192.168.1.101:8080
http://user3:pass3@192.168.1.102:8080
```

### 3️⃣ 配置环境变量

编辑 `.env` 文件：

```env
# 代理服务监听端口
PROXY_PORT=8282

# 代理文件路径
PROXY_FILE=proxy.txt

# 总的最大空闲连接数
MAX_IDLE_CONNS=1000

# 每个主机的最大空闲连接数
MAX_IDLE_CONNS_PER_HOST=100

# 请求超时时间（秒）
REQUEST_TIMEOUT=30

# 代理服务器认证（留空则不需要认证）
AUTH_USERNAME=admin
AUTH_PASSWORD=123456
```

### 4️⃣ 编译运行

```bash
# 安装依赖
go mod tidy

# 编译
go build -o proxyflow ./cmd/proxyflow

# 运行
./proxyflow
```

### 5️⃣ 使用代理

将你的HTTP客户端代理设置为：

- **无认证模式**：`http://localhost:8282`
- **认证模式**：`http://admin:123456@localhost:8282`

## 📋 配置说明

| 配置项 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `PROXY_PORT` | 代理服务监听端口 | `8282` | `8080` |
| `PROXY_FILE` | 代理列表文件路径 | `proxy.txt` | `proxies.txt` |
| `MAX_IDLE_CONNS` | 总的最大空闲连接数 | `1000` | `2000` |
| `MAX_IDLE_CONNS_PER_HOST` | 每个主机的最大空闲连接数 | `100` | `200` |
| `REQUEST_TIMEOUT` | 请求超时时间(秒) | `30` | `60` |
| `AUTH_USERNAME` | 认证用户名 | 空(无认证) | `admin` |
| `AUTH_PASSWORD` | 认证密码 | 空(无认证) | `123456` |

## 🐳 Docker 部署

### 方式一：使用 docker-compose（推荐）

1. **创建项目目录**
```bash
mkdir proxy-flow && cd proxy-flow
```

2. **创建代理列表文件**
```bash
cat > proxy.txt << EOF
# 代理列表文件
# 格式: http://username:password@ip:port
http://user1:pass1@proxy1.example.com:8080
http://user2:pass2@proxy2.example.com:8080
EOF
```

3. **创建 docker-compose.yml**
```yaml
version: '3.8'

services:
  proxy-flow:
    image: ghcr.io/rfym21/proxy-flow:latest
    container_name: proxy-flow
    ports:
      - "8282:8282"
    volumes:
      - ./proxy.txt:/app/proxy.txt
    environment:
      - PROXY_PORT=8282
      - PROXY_FILE=proxy.txt
      - MAX_IDLE_CONNS=1000
      - MAX_IDLE_CONNS_PER_HOST=100
      - REQUEST_TIMEOUT=30
      # 认证配置（可选）
      - AUTH_USERNAME=
      - AUTH_PASSWORD=
    restart: unless-stopped
```

4. **启动服务**
```bash
# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f proxy-flow

# 停止服务
docker compose down
```

### 方式二：直接运行

```bash
# 创建代理列表文件
echo "http://user:pass@proxy.example.com:8080" > proxy.txt

# 运行容器
docker run -d \
  --name proxyflow \
  -p 8282:8282 \
  -v $(pwd)/proxy.txt:/app/proxy.txt \
  -e PROXY_PORT=8282 \
  -e PROXY_FILE=/app/proxy.txt \
  -e MAX_IDLE_CONNS=1000 \
  -e MAX_IDLE_CONNS_PER_HOST=100 \
  -e REQUEST_TIMEOUT=30 \
  -e AUTH_USERNAME= \
  -e AUTH_PASSWORD= \
  --restart unless-stopped \
  ghcr.io/rfym21/proxy-flow:latest
```

## 💡 使用示例

### cURL 示例

```bash
# HTTP请求
curl.exe -v -x http://127.0.0.1:8282 http://httpbin.org/ip

# HTTPS请求
curl.exe -v -x http://127.0.0.1:8282 https://httpbin.org/ip
```

## 🧪 连通性测试

项目提供了Go语言编写的跨平台测试工具，用于验证代理服务是否正常工作：

### 使用方法

```bash
# 直接运行
go run scripts/test-proxy.go

# 或编译后运行
go build -o test-proxy scripts/test-proxy.go
./test-proxy

# Windows
go build -o test-proxy.exe scripts/test-proxy.go
test-proxy.exe
```

## 🏗️ 项目结构

```
ProxyFlow/
├── cmd/
│   └── proxyflow/
│       └── main.go          # 程序入口点，负责初始化和启动服务
├── internal/               # 内部包，不对外暴露
│   ├── auth/
│   │   └── auth.go         # HTTP Basic认证处理
│   ├── client/
│   │   └── client.go       # HTTP客户端连接池管理
│   ├── config/
│   │   └── config.go       # 环境变量配置加载
│   ├── models/
│   │   └── proxy.go        # 代理信息数据结构
│   ├── pool/
│   │   └── pool.go         # 代理池轮询管理
│   └── server/
│       └── server.go       # TCP代理服务器核心实现
├── scripts/                # 测试工具
│   └── test-proxy.go       # Go语言测试工具 (跨平台)
├── docs/
│   └── README.en.md        # 英文文档
├── .github/
│   └── workflows/
│       ├── docker-build-push.yml  # Docker镜像构建发布
│       └── release.yml             # 版本发布流程
├── proxy.txt               # 代理列表配置文件
├── .env                    # 环境变量配置文件
├── docker-compose.yml      # Docker Compose部署配置
├── go.mod                  # Go模块依赖管理
└── README.md               # 项目文档
```