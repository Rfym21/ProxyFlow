# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

ProxyFlow 是一个用 Go 编写的**上游代理负载均衡器**(代理池管理器)。它本身不是代理,而是在单一 TCP 端口上对外提供 HTTP/SOCKS5 入口,把客户端流量按轮询策略转发到一组上游代理(HTTP/HTTPS/SOCKS5),并管理代理池生命周期(多来源加载、热重载、失效剔除、有效性检查)。代理来源可以是多个本地文件(`PROXY_FILE`,逗号分隔)与多个远程 HTTP(S) 列表(`PROXY_URLS`,逗号分隔),两者可混用。

模块路径 `github.com/rfym21/ProxyFlow`,Go 1.24。版本号在 `.version` 文件维护(当前 1.4.4)。

## 常用命令

```bash
go mod tidy                              # 同步依赖
go build -o proxyflow ./cmd/proxyflow    # 编译(产物 proxyflow)
./proxyflow                              # 运行,读取同目录 .env 与 proxy.txt
go run scripts/test-proxy.go             # 跨平台连通性测试工具(无测试套件)
```

Docker:多阶段构建,`CGO_ENABLED=0 GOOS=linux`(见 Dockerfile);`docker compose up -d`(见 docker-compose.yml)。

仓库**没有任何测试文件或 lint 配置**——修改后只能靠 `scripts/test-proxy.go` 或手动 curl 验证,不要假设存在 `go test`。

## 架构:统一端口的协议自动识别(核心设计)

这是理解整个项目最关键的一点:**同一 TCP 监听器(默认 8280)同时服务 HTTP 和 SOCKS5**。

`server.handleConnectionWithProtocolDetect` 用 `bufio.Reader.Peek(1)` 读首字节:
- `0x05` → SOCKS5 路径(`handleSOCKS5WithReader`)
- 其它 → HTTP 路径(`handleConnectionWithReader`,再按首行是否 `CONNECT` 分流到隧道或普通 HTTP)

**关键约束**:peek 后必须把同一个 `*bufio.Reader` 透传给下游处理函数,不能 `conn` 直接重读——已经 buffer 进 reader 的字节会丢失。所有 `*WithReader` 后缀的方法都源于此。修改协议处理时务必保持这个 reader 传递链。

`internal/server/server.go` 是**纯 TCP 层手写协议解析**,没有用 `net/http.Serve`。CONNECT 隧道和 SOCKS5 建链成功后,都用 `copyData`(即 `io.Copy`)做客户端↔上游的双向裸字节转发。

## 两条上游数据路径

请求到达上游代理有两条完全不同的代码路径,改动时不要混淆:

1. **普通 HTTP 请求**(`handleHTTPTCP` → `client.Do`):走标准库 `http.Client` + `http.Transport`。`internal/client/client.go` 为**每个上游代理缓存一个独立的 `http.Client`**(以 `scheme://user:pass@host` 完整标识为 key,双重检查锁创建),复用连接池。**注意 key 必须含协议与认证**:住宅代理常以「同一 host:port + 不同 username」区分出口,若仅用 `proxy.Host` 作 key,不同凭据的代理会复用首个 client、认证头被固化,导致出口 IP 永远停在第一个、轮换失效。
2. **CONNECT 隧道 / SOCKS5**(`handleConnectTCP`、`handleSOCKS5WithReader` → `connectThroughProxy`):不走 client 包,直接 `net.Dial`/`tls.Dial`/`proxy.SOCKS5` 拿到一条到上游的 `net.Conn`,再裸转发。

`connectThroughProxy` 按 `ProxyInfo.IsSOCKS5()` 分发到 `connectThroughSOCKS5`(用 `golang.org/x/net/proxy`)或 `connectThroughHTTPProxy`(手写 CONNECT 请求)。

## 关键约定与坑点

- **上游 HTTPS 代理的自签证书**:当上游代理 scheme 是 `https` 时,代码对"到代理"的 TLS 握手设置 `InsecureSkipVerify: true`(见 `client.createHTTPClient` 与 `server.connectThroughHTTPProxy`)。这是刻意为之(最近一次 fix `cdaffa3` 专门解决 502),**不要"顺手"收紧**;且仅在 scheme=https 时设置,避免影响"到目标服务器"的正常 TLS 校验。
- **代理池热重载**:`internal/pool/pool.go` 用 fsnotify 监听**所有本地代理文件所在的目录**(而非文件本身,规避部分系统问题),目录去重后逐个 `watcher.Add`,事件按**绝对路径**匹配(避免不同目录下同名文件互相误触发)后在 Write/Create 触发重载。远程列表走独立的 `refreshLoop`,按 `PROXY_URL_INTERVAL` 定时拉取。轮询索引 `current` 用 `sync/atomic` 原子自增,代理列表本身用 `sync.RWMutex` 保护。
- **多来源合并的三条约定**(改 `loadProxies` 时必须保持):① 合并后按 `formatProxyURL` 去重,**先出现的来源胜出**,归属决定后续回写目标;② 单来源失败只警告并跳过,**全部失败才返回 error 并保留旧列表**;③ 新列表为空但旧列表非空时拒绝更新——否则远程来源一次抖动就会清空整个代理池。
- **失效代理剔除**:`pool.MarkFailed` 把代理从内存移除、追加到 `failure-proxy.txt`,然后**按 `ProxyInfo.Source` 只重写它所属的那一个本地文件**(`rewriteProxyFile(filename)` 只写回 `Source == filename` 的代理)。绝不能改回"重写单一 proxy.txt":多来源下那会把 A 文件的代理写进 B 文件。远程来源无法回写,改为记入内存 `failed` 集合,在 `dedupe` 中过滤;该集合启动时由 `loadFailedSet` 从 `failure-proxy.txt` 载入,所以重启后仍然生效。
- **自写自触与日志噪音**:`rewriteProxyFile` 会触发 fsnotify 的 Write 事件,导致一次幂等的冗余重载。因此 watcher 路径走 `reloadFromWatch`(仅在 `loadProxies` 返回 `changed` 时打日志),而导出的 `Reload` 保持每次都打。来源持续故障时的重复告警由 `lastFailures`(`atomic.Value`)抑制,`refreshLoop` 另有一份局部 `lastErr` 抑制整体失败的重复输出。
- **锁序**:`p.mu` 保护 `proxies`,`p.failedMu` 保护 `failed`。唯一的嵌套是 `MarkFailed` 中的 `mu → failedMu`;`loadProxies` 在 `dedupe` 里取 `failedMu` 时**尚未持有 `mu`**,新增代码不要制造反向嵌套。
- **认证共用**:`AUTH_USERNAME`/`AUTH_PASSWORD` 对 HTTP(407 Basic)和 SOCKS5(用户名/密码子协商)是同一套凭据,实现在 `internal/auth/auth.go` 与 server 的两套 `checkAuth*` / `handleSOCKS5Auth*` 中。
- **配置**:`internal/config/config.go` 全部从环境变量读取(经 `godotenv` 从 `.env` 加载),缺失或非法时回落默认值。注意 `PROXY_FILE` 的默认值是**动态**的:配置了 `PROXY_URLS` 时默认为空,以免纯远程部署因缺少 `proxy.txt` 反复告警。`.env.example` 是完整字段清单,改配置项要同步更新它和 README。
- **优雅关闭**:`main.go` 的 `setupGracefulShutdown` 监听 SIGINT/SIGTERM,顺序关 API server → pool watcher → proxy server。`Server.Start` 的 `Accept` 循环在 listener 关闭后返回错误退出。

## 包职责速查

| 包 | 职责 |
|---|---|
| `cmd/proxyflow` | 入口:加载配置、装配 pool/server/api、注册信号 |
| `internal/server` | TCP 层 HTTP/SOCKS5 协议识别与转发(手写解析) |
| `internal/client` | 普通 HTTP 请求的上游代理客户端池(每代理一个 http.Client) |
| `internal/pool` | 代理列表多来源加载(本地文件 + 远程 HTTP 列表)/解析/去重/轮询/热重载/定时刷新/失效剔除 |
| `internal/api` | 可选的 HTTP API(`/api/get-proxy`、`/api/status`,带有效性检查) |
| `internal/auth` | HTTP Basic Auth 编解码 |
| `internal/models` | `ProxyInfo` 数据结构(代理类型、URL、Host、凭据、来源标识 `Source`) |

代码注释已是完整 godoc 风格(包注释 + 所有导出符号注释),新增导出符号须保持同样标准。
