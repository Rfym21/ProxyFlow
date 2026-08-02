# ProxyFlow

<div align="center">

![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey?style=for-the-badge)
[![Telegram](https://img.shields.io/badge/Telegram-blue?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/nodejs_project)

**Language:** [中文](README.md) | [English](docs/README.en.md)

</div>

## 🚀 功能特性

- ✅ **多协议支持** - HTTP/HTTPS/SOCKS5 代理完整支持
- ✅ **统一端口入口** - 同一端口自动识别 HTTP 和 SOCKS5 协议
- ✅ **热重载代理列表** - 监听 proxy.txt 文件变化，自动重载代理列表
- ✅ **API 接口** - 提供 HTTP API 获取代理，支持有效性检查
- ✅ **失效代理管理** - 自动检测并移除失效代理，记录到独立文件
- ✅ **上游代理认证** - 支持 Basic Auth 认证的上游代理
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
# 支持格式:
#   HTTP/HTTPS: http://username:password@ip:port
#   SOCKS5:     socks5://username:password@ip:port
# 空行和以#开头的行将被忽略

# HTTP 代理
http://user1:pass1@192.168.1.100:8080
http://user2:pass2@192.168.1.101:8080

# SOCKS5 代理
socks5://user3:pass3@192.168.1.102:1080
socks5://192.168.1.103:1080
```

### 3️⃣ 配置环境变量

编辑 `.env` 文件：

```env
# ==================== 代理服务配置 ====================

# 代理服务监听端口（HTTP/SOCKS5 统一入口）
PROXY_PORT=8280

# 代理文件路径
PROXY_FILE=proxy.txt

# 失效代理文件路径
FAILURE_PROXY_FILE=failure-proxy.txt

# 总的最大空闲连接数
MAX_IDLE_CONNS=1000

# 每个主机的最大空闲连接数
MAX_IDLE_CONNS_PER_HOST=100

# 单个请求的最大代理重试次数
MAX_RETRY_COUNT=3

# 请求超时时间（秒）
REQUEST_TIMEOUT=30

# 代理服务器认证（留空则不需要认证，HTTP和SOCKS5共用）
AUTH_USERNAME=
AUTH_PASSWORD=

# 是否输出访问日志（每连接、每请求各一条）
# 设为 false 后仅保留启动、热重载、失效剔除与错误日志
ACCESS_LOG=true

# ==================== API 服务配置 ====================

# 是否启用 API 服务
API_ENABLED=false

# API 服务监听端口
API_PORT=8281

# API 访问密钥（留空则不需要认证）
API_KEY=

# 代理检查 URL
CHECK_URL=https://www.google.com

# 代理检查失败重试次数
CHECK_RETRY=5
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

将你的客户端代理设置为（同一端口，自动识别协议）：

**HTTP 代理：**
- 无认证模式：`http://localhost:8280`
- 认证模式：`http://admin:123456@localhost:8280`

**SOCKS5 代理：**
- 无认证模式：`socks5://localhost:8280`
- 认证模式：`socks5://admin:123456@localhost:8280`

## 📋 配置说明

| 配置项 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `PROXY_PORT` | 代理服务监听端口（HTTP/SOCKS5 统一入口） | `8280` | `8080` |
| `PROXY_FILE` | 代理列表文件路径 | `proxy.txt` | `proxies.txt` |
| `FAILURE_PROXY_FILE` | 失效代理文件路径 | `failure-proxy.txt` | `failed.txt` |
| `MAX_IDLE_CONNS` | 总的最大空闲连接数 | `1000` | `2000` |
| `MAX_IDLE_CONNS_PER_HOST` | 每个主机的最大空闲连接数 | `100` | `200` |
| `REQUEST_TIMEOUT` | 请求超时时间(秒) | `30` | `60` |
| `AUTH_USERNAME` | 认证用户名 | 空(无认证) | `admin` |
| `AUTH_PASSWORD` | 认证密码 | 空(无认证) | `123456` |
| `ACCESS_LOG` | 是否输出访问日志（每连接、每请求各一条） | `true` | `false` |

### API 配置

| 配置项 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `API_ENABLED` | 是否启用 API 服务 | `false` | `true` |
| `API_PORT` | API 服务监听端口 | `8281` | `8080` |
| `API_KEY` | API 访问密钥 | 空(无认证) | `your-secret-key` |
| `CHECK_URL` | 代理检查 URL | `https://www.google.com` | `https://httpbin.org/ip` |
| `CHECK_RETRY` | 代理检查失败重试次数 | `5` | `3` |

## 🔌 API 接口

### 获取代理

```
GET /api/get-proxy?key=xxx&number=xxx&check=true/false
```

**参数：**
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `key` | string | 是* | API 密钥（如果配置了 `API_KEY`） |
| `number` | int | 否 | 获取代理数量，默认 1 |
| `check` | bool | 否 | 是否检查代理有效性，默认 false |

**响应示例：**
```json
{
  "success": true,
  "data": [
    {
      "proxy": "http://user:pass@192.168.1.100:8080",
      "checked": true,
      "valid": true
    }
  ]
}
```

**说明：**
- 当 `check=true` 时，会对代理进行有效性检查
- 无效代理会被自动移至 `failure-proxy.txt` 并从 `proxy.txt` 中移除
- 最多重试 `CHECK_RETRY` 次获取有效代理

### 状态查询

```
GET /api/status?key=xxx
```

**参数：**
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `key` | string | 是* | API 密钥（如果配置了 `API_KEY`） |

**响应示例：**
```json
{
  "success": true,
  "data": {
    "pool_size": 10,
    "check_url": "https://www.google.com"
  }
}
```

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
# 支持 HTTP 和 SOCKS5 代理
http://user1:pass1@proxy1.example.com:8080
socks5://user2:pass2@proxy2.example.com:1080
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
      - "8280:8280"   # HTTP/SOCKS5 统一端口
      - "8281:8281"   # API 服务端口（可选）
    volumes:
      - ./proxy.txt:/app/proxy.txt
      - ./failure-proxy.txt:/app/failure-proxy.txt
    environment:
      # 代理服务配置
      - PROXY_PORT=8280
      - PROXY_FILE=proxy.txt
      - FAILURE_PROXY_FILE=failure-proxy.txt
      - MAX_IDLE_CONNS=1000
      - MAX_IDLE_CONNS_PER_HOST=100
      - MAX_RETRY_COUNT=3
      - REQUEST_TIMEOUT=30
      # 认证配置（可选，HTTP和SOCKS5共用）
      - AUTH_USERNAME=
      - AUTH_PASSWORD=
      # 访问日志（设为 false 关闭每连接/每请求日志）
      - ACCESS_LOG=true
      # API 服务配置（可选）
      - API_ENABLED=false
      - API_PORT=8281
      - API_KEY=
      - CHECK_URL=https://www.google.com
      - CHECK_RETRY=5
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
  -p 8280:8280 \
  -p 8281:8281 \
  -v $(pwd)/proxy.txt:/app/proxy.txt \
  -v $(pwd)/failure-proxy.txt:/app/failure-proxy.txt \
  -e PROXY_PORT=8280 \
  -e PROXY_FILE=/app/proxy.txt \
  -e FAILURE_PROXY_FILE=/app/failure-proxy.txt \
  -e MAX_IDLE_CONNS=1000 \
  -e MAX_IDLE_CONNS_PER_HOST=100 \
  -e REQUEST_TIMEOUT=30 \
  -e AUTH_USERNAME= \
  -e AUTH_PASSWORD= \
  -e ACCESS_LOG=true \
  -e API_ENABLED=false \
  -e API_PORT=8281 \
  -e API_KEY= \
  -e CHECK_URL=https://www.google.com \
  -e CHECK_RETRY=5 \
  --restart unless-stopped \
  ghcr.io/rfym21/proxy-flow:latest
```

## 💡 使用示例

### cURL 示例

```bash
# HTTP 代理 - HTTP 请求
curl -v -x http://127.0.0.1:8280 http://httpbin.org/ip

# HTTP 代理 - HTTPS 请求
curl -v -x http://127.0.0.1:8280 https://httpbin.org/ip

# SOCKS5 代理 - HTTP 请求（同一端口）
curl -v -x socks5://127.0.0.1:8280 http://httpbin.org/ip

# SOCKS5 代理 - HTTPS 请求（同一端口）
curl -v -x socks5://127.0.0.1:8280 https://httpbin.org/ip
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