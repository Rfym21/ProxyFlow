// Package server 提供HTTP代理服务器核心功能。
//
// 本包实现了完整的HTTP/HTTPS代理服务器，支持TCP连接处理、代理认证、
// 上游代理连接管理和双向数据转发等功能。服务器支持CONNECT隧道方式
// 和标准HTTP代理方式，能够处理HTTP和HTTPS流量。
package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

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
)

// Server HTTP代理服务器。
//
// 代理服务器核心实现，支持HTTP和HTTPS流量代理。
// 提供认证、连接池管理和上游代理负载均衡等功能。
type Server struct {
	pool         *pool.Pool     // 代理池
	client       *client.Client // HTTP客户端
	timeout      time.Duration  // 请求超时时间
	authUsername string         // 认证用户名
	authPassword string         // 认证密码
	listener     net.Listener   // TCP监听器
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
//
// 返回值：
//   - *Server: 配置完成的代理服务器实例
func NewServer(proxyPool *pool.Pool, timeout time.Duration, maxIdleConns, maxIdleConnsPerHost, maxRetryCount int, authUsername, authPassword string) *Server {
	return &Server{
		pool:         proxyPool,
		client:       client.NewClient(proxyPool, timeout, maxIdleConns, maxIdleConnsPerHost, maxRetryCount),
		timeout:      timeout,
		authUsername: authUsername,
		authPassword: authPassword,
	}
}

// Start 启动代理服务器并监听指定端口。
//
// 创建TCP监听器并开始接收客户端连接。每个连接
// 在独立的goroutine中处理，支持并发请求。
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

	log.Printf("代理服务器正在端口 %s 上启动", port)
	log.Printf("使用 %d 个代理进行轮询", s.pool.Size())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接时出错: %v", err)
			return err
		}

		go s.handleConnection(conn)
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

// handleConnection 处理单个TCP连接。
//
// 分析连接的第一行数据来判断请求类型：
// - CONNECT方法：处理HTTPS隧道连接
// - 其他方法：处理标准HTTP请求
//
// 参数：
//   - conn: 客户端TCP连接
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// 获取客户端IP地址
	clientIP := conn.RemoteAddr().String()
	log.Printf("新连接来自: %s", clientIP)

	reader := bufio.NewReader(conn)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		// EOF错误通常表示客户端正常断开连接，不需要记录为错误
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
			log.Printf("CONNECT %s -> 代理: %s", destAddr, s.formatProxyURL(proxy))
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
		log.Printf("%s %s -> 代理: %s", method, url, s.formatProxyURL(usedProxy))
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
// 建立到上游代理的连接，发送CONNECT请求以建立隧道。
// 支持代理认证和响应验证。
//
// 参数：
//   - destAddr: 目标地址（host:port格式）
//   - proxy: 代理服务器信息
//
// 返回值：
//   - net.Conn: 建立的代理连接
//   - error: 连接错误，成功时为nil
func (s *Server) connectThroughProxy(destAddr string, proxy models.ProxyInfo) (net.Conn, error) {
	// 连接到代理服务器
	proxyConn, err := net.Dial("tcp", proxy.Host)
	if err != nil {
		return nil, err
	}

	// 构建CONNECT请求
	// Host 头应该指向目标主机
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", destAddr, destAddr)
	connectReq += "Proxy-Connection: Keep-Alive\r\n"
	connectReq += "Content-Length: 0\r\n"

	// 添加代理认证
	if proxy.Username != "" {
		authValue := auth.EncodeBasicAuth(proxy.Username, proxy.Password)
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
