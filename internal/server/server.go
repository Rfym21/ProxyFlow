// Package server 提供HTTP代理服务器核心功能。
//
// 本包实现了完整的HTTP/HTTPS代理服务器，支持TCP连接处理、代理认证、
// 上游代理连接管理和双向数据转发等功能。服务器支持CONNECT隧道方式
// 和标准HTTP代理方式，能够处理HTTP和HTTPS流量。
package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/rfym21/ProxyFlow/internal/auth"
	"github.com/rfym21/ProxyFlow/internal/client"
	"github.com/rfym21/ProxyFlow/internal/models"
	"github.com/rfym21/ProxyFlow/internal/pool"
)

const (
	// DefaultHTTPSPort HTTPS默认端口
	DefaultHTTPSPort = "443"
	// ProxyResponseBufferSize 代理响应缓冲区大小
	ProxyResponseBufferSize = 1024

	// SOCKS5 协议常量
	socks5Version          = 0x05
	socks5AuthNone         = 0x00
	socks5AuthPassword     = 0x02
	socks5AuthNoAcceptable = 0xFF
	socks5CmdConnect       = 0x01
	socks5AddrTypeIPv4     = 0x01
	socks5AddrTypeDomain   = 0x03
	socks5AddrTypeIPv6     = 0x04
	socks5ReplySuccess     = 0x00
	socks5ReplyFailure     = 0x01
)

// Server HTTP/SOCKS5 代理服务器。
//
// 代理服务器核心实现，支持HTTP、HTTPS和SOCKS5流量代理。
// 同一端口自动识别协议类型，提供认证、连接池管理和上游代理负载均衡等功能。
type Server struct {
	pool            *pool.Pool     // 代理池
	client          *client.Client // HTTP客户端
	timeout         time.Duration  // 请求超时时间
	authUsername    string         // 认证用户名
	authPassword    string         // 认证密码
	accessLog       bool           // 是否输出访问日志
	listener        net.Listener   // TCP监听器
}

// NewServer 创建新的代理服务器实例。
//
// 参数：
//   - proxyPool: 代理池实例，用于管理上游代理
//   - timeout: HTTP请求超时时间
//   - maxIdleConns: 总的最大空闲连接数
//   - maxIdleConnsPerHost: 每个主机的最大空闲连接数
//   - maxRetryCount: 单个请求的最大代理重试次数
//   - authUsername: 代理服务器认证用户名，为空则不需要认证
//   - authPassword: 代理服务器认证密码
//   - accessLog: 是否输出访问日志（每连接、每请求各一条），关闭后仅保留启动与错误日志
//
// 返回值：
//   - *Server: 配置完成的代理服务器实例
func NewServer(proxyPool *pool.Pool, timeout time.Duration, maxIdleConns, maxIdleConnsPerHost, maxRetryCount int, authUsername, authPassword string, accessLog bool) *Server {
	return &Server{
		pool:         proxyPool,
		client:       client.NewClient(proxyPool, timeout, maxIdleConns, maxIdleConnsPerHost, maxRetryCount),
		timeout:      timeout,
		authUsername: authUsername,
		authPassword: authPassword,
		accessLog:    accessLog,
	}
}

// logAccess 输出访问日志。
//
// 仅在 accessLog 启用时打印，用于每连接、每请求级别的高频日志。
// 启动、关闭和错误日志不走此方法，始终输出。
//
// 参数：
//   - format: 格式化字符串
//   - v: 格式化参数
func (s *Server) logAccess(format string, v ...any) {
	if s.accessLog {
		log.Printf(format, v...)
	}
}

// Start 启动代理服务器并监听指定端口。
//
// 创建TCP监听器并开始接收客户端连接。每个连接
// 在独立的goroutine中处理，支持并发请求。
// 自动识别HTTP和SOCKS5协议，统一端口处理。
//
// 参数：
//   - port: 监听端口号
//
// 返回值：
//   - error: 服务器启动错误，成功时为nil
func (s *Server) Start(port string) error {
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	s.listener = listener

	log.Printf("代理服务器正在端口 %s 上启动 (HTTP/SOCKS5 统一入口)", port)
	log.Printf("使用 %d 个代理进行轮询", s.pool.Size())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接时出错: %v", err)
			return err
		}

		go s.handleConnectionWithProtocolDetect(conn)
	}
}

// handleConnectionWithProtocolDetect 处理连接并自动识别协议类型。
//
// 通过 peek 第一个字节判断是 SOCKS5 (0x05) 还是 HTTP 协议，
// 然后分发到对应的处理函数。
//
// 参数：
//   - conn: 客户端TCP连接
func (s *Server) handleConnectionWithProtocolDetect(conn net.Conn) {
	defer conn.Close()

	// 获取客户端IP地址
	clientIP := conn.RemoteAddr().String()

	// 创建带缓冲的读取器，用于 peek 第一个字节
	reader := bufio.NewReader(conn)

	// Peek 第一个字节判断协议类型
	firstByte, err := reader.Peek(1)
	if err != nil {
		if err != io.EOF {
			log.Printf("读取第一个字节时出错: %v", err)
		}
		return
	}

	if firstByte[0] == socks5Version {
		// SOCKS5 协议
		s.logAccess("SOCKS5 新连接来自: %s", clientIP)
		s.handleSOCKS5WithReader(conn, reader)
	} else {
		// HTTP 协议
		s.logAccess("HTTP 新连接来自: %s", clientIP)
		s.handleConnectionWithReader(conn, reader)
	}
}

// handleConnectionWithReader 处理HTTP连接（使用已有的reader）。
//
// 参数：
//   - conn: 客户端TCP连接
//   - reader: 已创建的缓冲读取器
func (s *Server) handleConnectionWithReader(conn net.Conn, reader *bufio.Reader) {
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			log.Printf("读取第一行时出错: %v", err)
		}
		return
	}

	if strings.HasPrefix(firstLine, "CONNECT ") {
		s.handleConnectTCP(conn, reader, firstLine)
	} else {
		s.handleHTTPTCP(conn, reader, firstLine)
	}
}

// Shutdown 优雅关闭代理服务器。
//
// 关闭TCP监听器并清理HTTP客户端连接池资源。
// 此方法是线程安全的，可以从其他goroutine调用。
//
// 返回值：
//   - error: 关闭过程中的错误，成功时为nil
func (s *Server) Shutdown() error {
	log.Printf("正在关闭代理服务器...")

	// 关闭TCP监听器
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Printf("关闭监听器时出错: %v", err)
		}
	}

	// 清理HTTP客户端连接池
	s.client.Close()

	log.Printf("代理服务器已成功关闭")
	return nil
}

// handleConnection 处理单个TCP连接（兼容旧接口）。
//
// 分析连接的第一行数据来判断请求类型：
// - CONNECT方法：处理HTTPS隧道连接
// - 其他方法：处理标准HTTP请求
//
// 参数：
//   - conn: 客户端TCP连接
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	s.handleConnectionWithReader(conn, reader)
}

// handleConnectTCP 处理TCP CONNECT请求。
//
// 处理HTTPS隧道连接，解析CONNECT请求并建立到目标服务器的隧道。
// 支持代理认证和自动的双向数据转发。
//
// 参数：
//   - conn: 客户端连接
//   - reader: 缓冲读取器
//   - firstLine: 已读取的第一行数据
func (s *Server) handleConnectTCP(conn net.Conn, reader *bufio.Reader, firstLine string) {
	// 解析CONNECT请求
	parts := strings.Fields(firstLine)
	if len(parts) < 2 {
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	destAddr := strings.TrimSpace(parts[1])
	if !strings.Contains(destAddr, ":") {
		destAddr += ":" + DefaultHTTPSPort
	}

	// 读取请求头并检查认证
	var authHeader string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF错误通常表示客户端正常断开连接
			if err != io.EOF {
				log.Printf("读取CONNECT请求头时出错: %v", err)
			}
			return
		}

		// 检查Proxy-Authorization头
		if strings.HasPrefix(strings.ToLower(line), "proxy-authorization:") {
			authHeader = strings.TrimSpace(line[len("proxy-authorization:"):])
		}

		// 空行表示请求头结束
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// 检查认证
	if !s.checkAuthTCP(conn, authHeader) {
		return
	}

	// 尝试通过代理连接
	var upstreamConn net.Conn
	var err error

	// 尝试通过代理连接
	for i := 0; i < s.pool.Size(); i++ {
		proxy := s.pool.NextProxy()
		upstreamConn, err = s.connectThroughProxy(destAddr, proxy)
		if err == nil {
			s.logAccess("CONNECT %s -> 代理: %s", destAddr, s.formatProxyURL(proxy))
			break
		}
	}

	if err != nil {
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer upstreamConn.Close()

	// 发送200 Connection Established响应
	_, err = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return
	}

	// 双向数据转发
	go s.copyData(upstreamConn, conn)
	s.copyData(conn, upstreamConn)
}

// handleHTTPTCP 处理TCP HTTP请求。
//
// 处理标准HTTP请求，包括请求解析、认证验证、
// 代理转发和响应返回。支持各种HTTP方法。
//
// 参数：
//   - conn: 客户端连接
//   - reader: 缓冲读取器
//   - firstLine: 已读取的第一行数据
func (s *Server) handleHTTPTCP(conn net.Conn, reader *bufio.Reader, firstLine string) {
	// 解析HTTP请求行
	parts := strings.Fields(firstLine)
	if len(parts) < 3 {
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	method := parts[0]
	url := parts[1]

	// 读取请求头并检查认证
	headers := make(map[string]string)
	var authHeader string
	var contentLength int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF错误通常表示客户端正常断开连接
			if err != io.EOF {
				log.Printf("读取HTTP请求头时出错: %v", err)
			}
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		// 解析头部
		if colonIndex := strings.Index(line, ":"); colonIndex > 0 {
			key := strings.TrimSpace(line[:colonIndex])
			value := strings.TrimSpace(line[colonIndex+1:])
			headers[strings.ToLower(key)] = value

			// 检查特殊头部
			if strings.ToLower(key) == "proxy-authorization" {
				authHeader = value
			} else if strings.ToLower(key) == "content-length" {
				if cl, err := fmt.Sscanf(value, "%d", &contentLength); cl != 1 || err != nil {
					contentLength = 0
				}
			}
		}
	}

	// 检查认证
	if !s.checkAuthTCP(conn, authHeader) {
		return
	}

	// 读取请求体
	var body []byte
	if contentLength > 0 {
		body = make([]byte, contentLength)
		_, err := io.ReadFull(reader, body)
		if err != nil {
			conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			return
		}
	}

	// 创建HTTP请求
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	// 设置请求头（排除代理相关头部）
	for key, value := range headers {
		if key != "proxy-authorization" && key != "proxy-connection" {
			req.Header.Set(key, value)
		}
	}

	// 通过代理发送请求
	resp, usedProxy, err := s.client.Do(req)
	if err == nil {
		s.logAccess("%s %s -> 代理: %s", method, url, s.formatProxyURL(usedProxy))
	}

	if err != nil {
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer resp.Body.Close()

	// 发送响应状态行
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", resp.StatusCode, resp.Status[4:])
	conn.Write([]byte(statusLine))

	// 发送响应头
	for key, values := range resp.Header {
		for _, value := range values {
			headerLine := fmt.Sprintf("%s: %s\r\n", key, value)
			conn.Write([]byte(headerLine))
		}
	}

	// 发送空行分隔头部和正文
	conn.Write([]byte("\r\n"))

	// 发送响应体
	io.Copy(conn, resp.Body)
}

// connectThroughProxy 通过代理服务器连接到目标地址。
//
// 建立到上游代理的连接，根据代理类型使用不同的隧道协议。
// HTTP代理使用CONNECT方法，SOCKS5使用原生SOCKS5协议。
//
// 参数：
//   - destAddr: 目标地址（host:port格式）
//   - proxyInfo: 代理服务器信息
//
// 返回值：
//   - net.Conn: 建立的代理连接
//   - error: 连接错误，成功时为nil
func (s *Server) connectThroughProxy(destAddr string, proxyInfo models.ProxyInfo) (net.Conn, error) {
	if proxyInfo.IsSOCKS5() {
		return s.connectThroughSOCKS5(destAddr, proxyInfo)
	}
	return s.connectThroughHTTPProxy(destAddr, proxyInfo)
}

// connectThroughHTTPProxy 通过 HTTP 代理建立 CONNECT 隧道。
//
// 参数：
//   - destAddr: 目标地址（host:port格式）
//   - proxyInfo: 代理服务器信息
//
// 返回值：
//   - net.Conn: 建立的代理连接
//   - error: 连接错误，成功时为nil
func (s *Server) connectThroughHTTPProxy(destAddr string, proxyInfo models.ProxyInfo) (net.Conn, error) {
	// 连接到代理服务器。https 上游代理要求先与代理完成 TLS 握手，
	// 否则向其发送明文 CONNECT 会失败；自签证书场景跳过校验。
	var proxyConn net.Conn
	var err error
	if proxyInfo.URL.Scheme == "https" {
		proxyConn, err = tls.DialWithDialer(
			&net.Dialer{Timeout: s.timeout},
			"tcp",
			proxyInfo.Host,
			&tls.Config{
				InsecureSkipVerify: true,
				ServerName:         proxyInfo.URL.Hostname(),
			},
		)
	} else {
		proxyConn, err = net.Dial("tcp", proxyInfo.Host)
	}
	if err != nil {
		return nil, err
	}

	// 构建CONNECT请求
	// Host 头应该指向目标主机
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", destAddr, destAddr)
	connectReq += "Proxy-Connection: Keep-Alive\r\n"
	connectReq += "Content-Length: 0\r\n"

	// 添加代理认证
	if proxyInfo.Username != "" {
		authValue := auth.EncodeBasicAuth(proxyInfo.Username, proxyInfo.Password)
		authHeader := fmt.Sprintf("Proxy-Authorization: %s\r\n", authValue)
		connectReq += authHeader
	}

	connectReq += "\r\n"

	// 发送CONNECT请求
	_, err = proxyConn.Write([]byte(connectReq))
	if err != nil {
		proxyConn.Close()
		return nil, err
	}

	// 读取代理响应
	buffer := make([]byte, ProxyResponseBufferSize)
	n, err := proxyConn.Read(buffer)
	if err != nil {
		proxyConn.Close()
		return nil, err
	}

	response := string(buffer[:n])
	if !strings.Contains(response, "200") {
		proxyConn.Close()
		return nil, fmt.Errorf("代理连接失败: %s", response)
	}

	return proxyConn, nil
}

// connectThroughSOCKS5 通过 SOCKS5 代理建立隧道。
//
// 参数：
//   - destAddr: 目标地址（host:port格式）
//   - proxyInfo: 代理服务器信息
//
// 返回值：
//   - net.Conn: 建立的代理连接
//   - error: 连接错误，成功时为nil
func (s *Server) connectThroughSOCKS5(destAddr string, proxyInfo models.ProxyInfo) (net.Conn, error) {
	// 配置 SOCKS5 认证
	var socks5Auth *proxy.Auth
	if proxyInfo.Username != "" {
		socks5Auth = &proxy.Auth{
			User:     proxyInfo.Username,
			Password: proxyInfo.Password,
		}
	}

	// 创建 SOCKS5 dialer
	dialer, err := proxy.SOCKS5("tcp", proxyInfo.Host, socks5Auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("创建SOCKS5 dialer失败: %v", err)
	}

	// 通过 SOCKS5 代理连接到目标
	conn, err := dialer.Dial("tcp", destAddr)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5代理连接失败: %v", err)
	}

	return conn, nil
}

// copyData 在两个连接间复制数据。
//
// 用于隧道模式下的双向数据转发，直到数据传输完成
// 或发生错误。该函数会阻塞直到数据传输结束。
//
// 参数：
//   - dst: 目标写入器
//   - src: 源读取器
func (s *Server) copyData(dst io.Writer, src io.Reader) {
	io.Copy(dst, src)
}

// checkAuthTCP 检查TCP连接的代理认证。
//
// 验证客户端提供的认证凭据是否正确。如果未配置认证，
// 则跳过验证。认证失败时发送407响应。
//
// 参数：
//   - conn: 客户端连接
//   - authHeader: 认证头字符串
//
// 返回值：
//   - bool: 认证是否通过
func (s *Server) checkAuthTCP(conn net.Conn, authHeader string) bool {
	// 如果没有设置认证，则跳过检查
	if s.authUsername == "" && s.authPassword == "" {
		return true
	}

	// 检查是否有认证头
	if authHeader == "" {
		s.sendAuthRequiredTCP(conn)
		return false
	}

	// 解析Basic认证
	username, password, err := auth.DecodeBasicAuth(authHeader)
	if err != nil {
		s.sendAuthRequiredTCP(conn)
		return false
	}

	// 验证用户名和密码
	if username != s.authUsername || password != s.authPassword {
		s.sendAuthRequiredTCP(conn)
		return false
	}

	return true
}

// sendAuthRequiredTCP 发送TCP认证要求响应。
//
// 向客户端发送407 Proxy Authentication Required响应，
// 要求客户端提供认证信息。
//
// 参数：
//   - conn: 客户端连接
func (s *Server) sendAuthRequiredTCP(conn net.Conn) {
	response := "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"ProxyFlow\"\r\n\r\n"
	conn.Write([]byte(response))
}

// formatProxyURL 格式化代理URL用于日志显示。
//
// 构建包含协议和主机信息的完整代理URL，如果包含认证信息则隐藏密码。
//
// 参数：
//   - proxy: 代理服务器信息
//
// 返回值：
//   - string: 格式化后的代理URL
func (s *Server) formatProxyURL(proxy models.ProxyInfo) string {
	if proxy.Username != "" {
		return fmt.Sprintf("%s://%s:***@%s", proxy.URL.Scheme, proxy.Username, proxy.Host)
	}
	return fmt.Sprintf("%s://%s", proxy.URL.Scheme, proxy.Host)
}

// handleSOCKS5WithReader 处理 SOCKS5 客户端连接（使用已有的reader）。
//
// 实现完整的 SOCKS5 协议握手、认证和隧道建立流程。
//
// 参数：
//   - conn: 客户端 TCP 连接
//   - reader: 已创建的缓冲读取器
func (s *Server) handleSOCKS5WithReader(conn net.Conn, reader *bufio.Reader) {
	// 1. 握手阶段：读取客户端支持的认证方法
	// 格式: VER | NMETHODS | METHODS
	// 注意：第一个字节 (VER=0x05) 已经被 peek 过，需要消费掉
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}

	if header[0] != socks5Version {
		return
	}

	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}

	// 2. 选择认证方法
	var selectedMethod byte
	if s.authUsername == "" && s.authPassword == "" {
		// 无需认证
		selectedMethod = socks5AuthNone
	} else {
		// 需要用户名密码认证
		hasPasswordAuth := false
		for _, m := range methods {
			if m == socks5AuthPassword {
				hasPasswordAuth = true
				break
			}
		}
		if hasPasswordAuth {
			selectedMethod = socks5AuthPassword
		} else {
			// 客户端不支持密码认证
			conn.Write([]byte{socks5Version, socks5AuthNoAcceptable})
			return
		}
	}

	// 发送认证方法选择响应
	conn.Write([]byte{socks5Version, selectedMethod})

	// 3. 用户名密码认证（如果需要）
	if selectedMethod == socks5AuthPassword {
		if !s.handleSOCKS5AuthWithReader(conn, reader) {
			return
		}
	}

	// 4. 读取请求
	// 格式: VER | CMD | RSV | ATYP | DST.ADDR | DST.PORT
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil {
		return
	}

	if request[0] != socks5Version {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	cmd := request[1]
	if cmd != socks5CmdConnect {
		// 只支持 CONNECT 命令
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	// 5. 解析目标地址
	destAddr, err := s.parseSOCKS5AddressWithReader(reader, request[3])
	if err != nil {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	// 6. 通过代理池连接到目标
	var upstreamConn net.Conn

	for i := 0; i < s.pool.Size(); i++ {
		proxyInfo := s.pool.NextProxy()
		upstreamConn, err = s.connectThroughProxy(destAddr, proxyInfo)
		if err == nil {
			s.logAccess("SOCKS5 CONNECT %s -> 代理: %s", destAddr, s.formatProxyURL(proxyInfo))
			break
		}
	}

	if err != nil {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}
	defer upstreamConn.Close()

	// 7. 发送成功响应
	// 使用 0.0.0.0:0 作为绑定地址
	s.sendSOCKS5Reply(conn, socks5ReplySuccess, &net.TCPAddr{IP: net.IPv4zero, Port: 0})

	// 8. 双向数据转发
	go s.copyData(upstreamConn, reader)
	s.copyData(conn, upstreamConn)
}

// handleSOCKS5AuthWithReader 处理 SOCKS5 用户名密码认证（使用reader）。
//
// 参数：
//   - conn: 客户端连接
//   - reader: 缓冲读取器
//
// 返回值：
//   - bool: 认证是否成功
func (s *Server) handleSOCKS5AuthWithReader(conn net.Conn, reader *bufio.Reader) bool {
	// 格式: VER | ULEN | UNAME | PLEN | PASSWD
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return false
	}

	// 认证子协商版本应为 0x01
	if header[0] != 0x01 {
		conn.Write([]byte{0x01, 0x01}) // 认证失败
		return false
	}

	usernameLen := int(header[1])
	username := make([]byte, usernameLen)
	if _, err := io.ReadFull(reader, username); err != nil {
		return false
	}

	passwordLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(reader, passwordLenBuf); err != nil {
		return false
	}

	passwordLen := int(passwordLenBuf[0])
	password := make([]byte, passwordLen)
	if _, err := io.ReadFull(reader, password); err != nil {
		return false
	}

	// 验证用户名密码
	if string(username) == s.authUsername && string(password) == s.authPassword {
		conn.Write([]byte{0x01, 0x00}) // 认证成功
		return true
	}

	conn.Write([]byte{0x01, 0x01}) // 认证失败
	return false
}

// parseSOCKS5AddressWithReader 解析 SOCKS5 目标地址（使用reader）。
//
// 参数：
//   - reader: 缓冲读取器
//   - addrType: 地址类型
//
// 返回值：
//   - string: 目标地址（host:port 格式）
//   - error: 解析错误
func (s *Server) parseSOCKS5AddressWithReader(reader *bufio.Reader, addrType byte) (string, error) {
	var host string

	switch addrType {
	case socks5AddrTypeIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(reader, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()

	case socks5AddrTypeDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			return "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(reader, domain); err != nil {
			return "", err
		}
		host = string(domain)

	case socks5AddrTypeIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(reader, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()

	default:
		return "", fmt.Errorf("不支持的地址类型: %d", addrType)
	}

	// 读取端口（2字节，大端序）
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	return fmt.Sprintf("%s:%d", host, port), nil
}

// StartSOCKS5 启动 SOCKS5 代理服务器并监听指定端口。
// Deprecated: 使用 Start() 方法，已支持 HTTP/SOCKS5 协议自动识别。
//
// 参数：
//   - port: 监听端口号
//
// 返回值：
//   - error: 服务器启动错误，成功时为nil
func (s *Server) StartSOCKS5(port string) error {
	log.Printf("警告: StartSOCKS5 已废弃，请使用 Start() 方法（统一端口自动识别协议）")
	return s.Start(port)
}

// handleSOCKS5 处理 SOCKS5 客户端连接。
//
// 实现完整的 SOCKS5 协议握手、认证和隧道建立流程。
//
// 参数：
//   - conn: 客户端 TCP 连接
func (s *Server) handleSOCKS5(conn net.Conn) {
	defer conn.Close()

	clientIP := conn.RemoteAddr().String()
	s.logAccess("SOCKS5 新连接来自: %s", clientIP)

	// 1. 握手阶段：读取客户端支持的认证方法
	// 格式: VER | NMETHODS | METHODS
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}

	if header[0] != socks5Version {
		return
	}

	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	// 2. 选择认证方法
	var selectedMethod byte
	if s.authUsername == "" && s.authPassword == "" {
		// 无需认证
		selectedMethod = socks5AuthNone
	} else {
		// 需要用户名密码认证
		hasPasswordAuth := false
		for _, m := range methods {
			if m == socks5AuthPassword {
				hasPasswordAuth = true
				break
			}
		}
		if hasPasswordAuth {
			selectedMethod = socks5AuthPassword
		} else {
			// 客户端不支持密码认证
			conn.Write([]byte{socks5Version, socks5AuthNoAcceptable})
			return
		}
	}

	// 发送认证方法选择响应
	conn.Write([]byte{socks5Version, selectedMethod})

	// 3. 用户名密码认证（如果需要）
	if selectedMethod == socks5AuthPassword {
		if !s.handleSOCKS5Auth(conn) {
			return
		}
	}

	// 4. 读取请求
	// 格式: VER | CMD | RSV | ATYP | DST.ADDR | DST.PORT
	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return
	}

	if request[0] != socks5Version {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	cmd := request[1]
	if cmd != socks5CmdConnect {
		// 只支持 CONNECT 命令
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	// 5. 解析目标地址
	destAddr, err := s.parseSOCKS5Address(conn, request[3])
	if err != nil {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}

	// 6. 通过代理池连接到目标
	var upstreamConn net.Conn

	for i := 0; i < s.pool.Size(); i++ {
		proxyInfo := s.pool.NextProxy()
		upstreamConn, err = s.connectThroughProxy(destAddr, proxyInfo)
		if err == nil {
			s.logAccess("SOCKS5 CONNECT %s -> 代理: %s", destAddr, s.formatProxyURL(proxyInfo))
			break
		}
	}

	if err != nil {
		s.sendSOCKS5Reply(conn, socks5ReplyFailure, nil)
		return
	}
	defer upstreamConn.Close()

	// 7. 发送成功响应
	// 使用 0.0.0.0:0 作为绑定地址
	s.sendSOCKS5Reply(conn, socks5ReplySuccess, &net.TCPAddr{IP: net.IPv4zero, Port: 0})

	// 8. 双向数据转发
	go s.copyData(upstreamConn, conn)
	s.copyData(conn, upstreamConn)
}

// handleSOCKS5Auth 处理 SOCKS5 用户名密码认证。
//
// 参数：
//   - conn: 客户端连接
//
// 返回值：
//   - bool: 认证是否成功
func (s *Server) handleSOCKS5Auth(conn net.Conn) bool {
	// 格式: VER | ULEN | UNAME | PLEN | PASSWD
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return false
	}

	// 认证子协商版本应为 0x01
	if header[0] != 0x01 {
		conn.Write([]byte{0x01, 0x01}) // 认证失败
		return false
	}

	usernameLen := int(header[1])
	username := make([]byte, usernameLen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return false
	}

	passwordLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, passwordLenBuf); err != nil {
		return false
	}

	passwordLen := int(passwordLenBuf[0])
	password := make([]byte, passwordLen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return false
	}

	// 验证用户名密码
	if string(username) == s.authUsername && string(password) == s.authPassword {
		conn.Write([]byte{0x01, 0x00}) // 认证成功
		return true
	}

	conn.Write([]byte{0x01, 0x01}) // 认证失败
	return false
}

// parseSOCKS5Address 解析 SOCKS5 目标地址。
//
// 参数：
//   - conn: 客户端连接
//   - addrType: 地址类型
//
// 返回值：
//   - string: 目标地址（host:port 格式）
//   - error: 解析错误
func (s *Server) parseSOCKS5Address(conn net.Conn, addrType byte) (string, error) {
	var host string

	switch addrType {
	case socks5AddrTypeIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()

	case socks5AddrTypeDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", err
		}
		host = string(domain)

	case socks5AddrTypeIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()

	default:
		return "", fmt.Errorf("不支持的地址类型: %d", addrType)
	}

	// 读取端口（2字节，大端序）
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	return fmt.Sprintf("%s:%d", host, port), nil
}

// sendSOCKS5Reply 发送 SOCKS5 响应。
//
// 参数：
//   - conn: 客户端连接
//   - reply: 响应状态码
//   - addr: 绑定地址（可为 nil）
func (s *Server) sendSOCKS5Reply(conn net.Conn, reply byte, addr *net.TCPAddr) {
	// 格式: VER | REP | RSV | ATYP | BND.ADDR | BND.PORT
	response := []byte{socks5Version, reply, 0x00}

	if addr == nil {
		// 使用 0.0.0.0:0
		response = append(response, socks5AddrTypeIPv4)
		response = append(response, 0, 0, 0, 0) // 0.0.0.0
		response = append(response, 0, 0)       // port 0
	} else {
		ip := addr.IP.To4()
		if ip != nil {
			response = append(response, socks5AddrTypeIPv4)
			response = append(response, ip...)
		} else {
			response = append(response, socks5AddrTypeIPv6)
			response = append(response, addr.IP.To16()...)
		}
		portBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(portBytes, uint16(addr.Port))
		response = append(response, portBytes...)
	}

	conn.Write(response)
}
