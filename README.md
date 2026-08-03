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
- ✅ **多来源代理池** - 同时从多个本地文件与远程 HTTP(S) 列表加载，自动跨来源去重
- ✅ **热重载代理列表** - 监听本地文件变化自动重载，远程列表按间隔定时刷新
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

也可以同时使用多个来源，`PROXY_FILE` 用逗号分隔多个本地文件，`PROXY_URLS` 配置远程列表地址：

```env
PROXY_FILE=data/cn.txt,data/us.txt
PROXY_URLS=https://example.com/proxies.txt,https://backup.example.com/list.txt
```

远程列表的响应体格式与本地文件完全一致（每行一个代理，支持空行与 `#` 注释）。
详见 [多来源代理池](#-多来源代理池)。

### 3️⃣ 配置环境变量

编辑 `.env` 文件：

```env
# ==================== 代理服务配置 ====================

# 代理服务监听端口（HTTP/SOCKS5 统一入口）
PROXY_PORT=8280

# 代理文件路径，支持逗号分隔多个文件
PROXY_FILE=proxy.txt

# 远程代理列表地址，支持逗号分隔多个（http/https），留空表示不使用
PROXY_URLS=

# 远程代理列表刷新间隔（秒），0 表示只在启动时拉取一次
PROXY_URL_INTERVAL=300

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
| `PROXY_FILE` | 代理列表文件路径，逗号分隔可配多个 | `proxy.txt` | `data/cn.txt,data/us.txt` |
| `PROXY_URLS` | 远程代理列表地址，逗号分隔可配多个 | 空(不使用) | `https://example.com/proxies.txt` |
| `PROXY_URL_INTERVAL` | 远程列表刷新间隔(秒)，`0` 为只拉取一次 | `300` | `60` |
| `FAILURE_PROXY_FILE` | 失效代理文件路径 | `failure-proxy.txt` | `failed.txt` |
| `MAX_IDLE_CONNS` | 总的最大空闲连接数 | `1000` | `2000` |
| `MAX_IDLE_CONNS_PER_HOST` | 每个主机的最大空闲连接数 | `100` | `200` |
| `MAX_RETRY_COUNT` | 单个请求的最大代理重试次数 | `3` | `5` |
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

## 🗂️ 多来源代理池

`PROXY_FILE`（本地文件）与 `PROXY_URLS`（远程列表）可以单独使用，也可以同时使用，
两者的加载结果会合并成一个代理池。

### 加载与去重

- 所有来源按配置顺序依次加载，合并后按完整代理 URL（含协议与认证信息）去重
- 重复的代理归属**先出现的来源**，因此来源顺序会影响失效剔除时改写哪个文件
- 单个来源加载失败（文件不存在、远程 502 等）只输出一次警告并跳过，其余来源照常工作
- 所有来源都失败时保留原有代理池，不会被清空

> 只配置了 `PROXY_URLS` 时，`PROXY_FILE` 不再回落到默认的 `proxy.txt`，
> 纯远程部署无需准备本地文件。若要两者并用，显式写出 `PROXY_FILE=proxy.txt` 即可。

### 更新机制

| 来源类型 | 更新方式 | 说明 |
|---------|---------|------|
| 本地文件 | fsnotify 监听 | 文件内容变化时立即重载，实时生效 |
| 远程列表 | 定时轮询 | 按 `PROXY_URL_INTERVAL` 拉取，内容无变化时不输出日志 |

远程列表的响应体格式与本地文件一致，读取上限 10MB，单次拉取超时 30 秒。

### 失效代理的处理差异

代理被判定失效时（API 的 `check=true`），两类来源的处理方式不同：

- **本地文件来源**：从内存移除，并**只重写它所属的那个文件**（保留该文件的注释与空行），
  其它来源的文件不受影响
- **远程列表来源**：远端不可写，改为记入失效集合，在后续每次刷新时过滤掉。
  该集合启动时从 `failure-proxy.txt` 载入，因此重启后依然生效

两类来源的失效代理都会追加到 `failure-proxy.txt`。若要让某个远程代理重新参与轮询，
把它从 `failure-proxy.txt` 中删除并重启即可。

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

> ⚠️ 代理文件必须放在一个**目录**里整体挂载。若直接挂载单个文件（`./proxy.txt:/app/proxy.txt`），
> 宿主机的修改不会触发容器内的 inotify 事件，代理文件**热重载会失效**。

```bash
mkdir -p data && cat > data/proxy.txt << EOF
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
      # 挂载数据目录（必须挂目录而非单个文件，否则热重载失效）
      - ./data:/app/data
    environment:
      # 代理服务配置
      - PROXY_PORT=8280
      - PROXY_FILE=/app/data/proxy.txt
      # 远程代理列表（可选，逗号分隔多个）
      - PROXY_URLS=
      - PROXY_URL_INTERVAL=300
      - FAILURE_PROXY_FILE=/app/data/failure-proxy.txt
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
# 创建数据目录与代理列表文件（挂目录而非单个文件，否则热重载失效）
mkdir -p data
echo "http://user:pass@proxy.example.com:8080" > data/proxy.txt

# 运行容器
docker run -d \
  --name proxyflow \
  -p 8280:8280 \
  -p 8281:8281 \
  -v $(pwd)/data:/app/data \
  -e PROXY_PORT=8280 \
  -e PROXY_FILE=/app/data/proxy.txt \
  -e PROXY_URLS= \
  -e PROXY_URL_INTERVAL=300 \
  -e FAILURE_PROXY_FILE=/app/data/failure-proxy.txt \
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