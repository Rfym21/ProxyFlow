# ProxyFlow

<div align="center">

![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey?style=for-the-badge)
[![Telegram](https://img.shields.io/badge/Telegram-blue?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/nodejs_project)

**Language:** [中文](../README.md) | [English](README.en.md)

</div>

## 🚀 Features

- ✅ **Dual Protocol Support** - Complete HTTP/HTTPS proxy support
- ✅ **Upstream Proxy Authentication** - Support for Basic Auth authenticated upstream proxies
- ✅ **Intelligent Load Balancing** - Round-robin algorithm for automatic request distribution
- ✅ **Connection Pool Management** - Efficient connection reuse mechanism

## 📦 Quick Start

### 1️⃣ Clone the Project

```bash
git clone https://github.com/Rfym21/ProxyFlow.git
cd ProxyFlow
```

### 2️⃣ Configure Proxy List

Edit the `proxy.txt` file, one proxy per line:

```txt
# Proxy list file
# Format: http://username:password@ip:port
# Empty lines and lines starting with # will be ignored

http://user1:pass1@192.168.1.100:8080
http://user2:pass2@192.168.1.101:8080
http://user3:pass3@192.168.1.102:8080
```

### 3️⃣ Configure Environment Variables

Edit the `.env` file:

```env
# Proxy service listening port
PROXY_PORT=8282

# Proxy file path
PROXY_FILE=proxy.txt

# Maximum total idle connections
MAX_IDLE_CONNS=1000

# Maximum idle connections per host
MAX_IDLE_CONNS_PER_HOST=100

# Request timeout in seconds
REQUEST_TIMEOUT=30

# Proxy server authentication (leave empty for no authentication)
AUTH_USERNAME=admin
AUTH_PASSWORD=123456
```

### 4️⃣ Build and Run

```bash
# Install dependencies
go mod tidy

# Build
go build -o proxyflow ./cmd/proxyflow

# Run
./proxyflow
```

### 5️⃣ Use the Proxy

Configure your HTTP client proxy to:

- **No Authentication Mode**: `http://localhost:8282`
- **Authentication Mode**: `http://admin:123456@localhost:8282`

## 📋 Configuration

| Config Item | Description | Default | Example |
|--------|------|--------|------|
| `PROXY_PORT` | Proxy service listening port | `8282` | `8080` |
| `PROXY_FILE` | Proxy list file path | `proxy.txt` | `proxies.txt` |
| `MAX_IDLE_CONNS` | Maximum total idle connections | `1000` | `2000` |
| `MAX_IDLE_CONNS_PER_HOST` | Maximum idle connections per host | `100` | `200` |
| `REQUEST_TIMEOUT` | Request timeout in seconds | `30` | `60` |
| `AUTH_USERNAME` | Authentication username | Empty (no auth) | `admin` |
| `AUTH_PASSWORD` | Authentication password | Empty (no auth) | `123456` |

## 🐳 Docker Deployment

### Method 1: Using docker-compose (Recommended)

1. **Create project directory**
```bash
mkdir proxy-flow && cd proxy-flow
```

2. **Create proxy list file**
```bash
catxy.txt << EOF
# Proxy list file
# Format: http://username:password@ip:port
http://user1:pass1@proxy1.example.com:8080
http://user2:pass2@proxy2.example.com:8080
EOF
```

3. **Create docker-compose.yml**
```yaml
version: '3.8'

services:
  proxy-flow:
    image: ghcr.io/rfym21/proxy-flow:lt
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
      # Authentication config (optional)
      - AUTH_USERNAME=
      - AUTH_PASSWORD=
    restart: unless-stopped
```

4. **Start service**
```bash
# Start service
docker compose up -d

# View logs
docker compose logs -f proxy-flow

# Stop service
docker compose down
```

### Method 2: Direct Run

```bash
# Create proxy list file
echo "http://user:pa.example.com:8080" > proxy.txt

# Run container
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

## 💡 Usage Examples

### cURL Examples

```bash
# HTTP request
curl.exe -v -x http://127.0.0.1:8282 http://httpbin.org/ip

# HTTPS request
curl.exe -v -x http://127.0.0.1:8282 https://httpbin.org/ip
```

## 🧪 Connectivity Testing

The project provides a cross-platform testing tool written in Go to verify that the proxy service is working properly:

### Usage

```bash
# Run directly
go run scripts/test-proxy.go

# Or compile and run
go build -o test-proxy scripts/test-proxy.go
./test-proxy

# Windows
go build -o test-proxy.exe scripts/test-proxy.go
test-proxy.exe
```

## 🏗️ Project Structure

```
ProxyFlow/
├── cmd/
│   └── proxyflow/
│       └── main.go          # Program entry point, handles initialization and startup
├── internal/               # Internal packages, not exposed externally
│   ├── auth/
│   │   └── auth.go         # HTTP Basic authentication handling
│   ├── client/
│   │   └── client.go       # HTTP client connection pool management
│   ├── config/
│   │   └── config.go       # Environment variable configuration loading
│   ├── models/
│   │   └── proxy.go        # Proxy information data structures
│   ├── pool/
│   │   └── pool.go         # Proxy pool round-robin management
│   └── server/
│       └── server.go       # TCP proxy server core implementation
├── scripts/                # Testing tools
│   └── test-proxy.go       # Go testing tool (cross-platform)
├── docs/
│   └── README.en.md        # English documentation
├── .github/
│   └── workflows/
│       ├── docker-build-push.yml  # Docker image build and publish
│       └── release.yml             # Version release process
├── proxy.txt               # Proxy list configuration file
├── .env                    # Environment variables configuration file
├── docker-compose.yml      # Docker Compose deployment configuration
├── go.mod                  # Go module dependency management
└── README.md               # Project documentation (Chinese)
```