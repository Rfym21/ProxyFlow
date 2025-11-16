// Package client 提供HTTP客户端连接池管理功能。
//
// 本包实现了基于代理池的HTTP客户端管理器，自动处理上游代理认证、
// 连接复用和负载均衡等功能。为代理服务器提供高效的HTTP请求转发，
// 支持多种代理协议和认证方式。
package client

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rfym21/ProxyFlow/internal/auth"
	"github.com/rfym21/ProxyFlow/internal/models"
	"github.com/rfym21/ProxyFlow/internal/pool"
)

// proxyAuthTransport 代理认证传输层。
//
// 实现http.RoundTripper接口，自动为HTTP请求添加
// Proxy-Authorization头部，用于代理服务器认证。
type proxyAuthTransport struct {
	base      http.RoundTripper
	proxyAuth string
}

// RoundTrip 执行HTTP请求并添加代理认证头。
//
// 参数：
//   - req: HTTP请求实例
//
// 返回值：
//   - *http.Response: HTTP响应实例
//   - error: 请求执行错误，成功时为nil
func (t *proxyAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Proxy-Authorization", t.proxyAuth)
	return t.base.RoundTrip(req)
}

// Client HTTP客户端连接池管理器。
//
// 管理多个代理服务器的HTTP客户端实例，提供连接复用、
// 负载均衡和认证管理等功能。每个代理对应一个独立的
// HTTP客户端实例，含有专门的连接池配置。
type Client struct {
	pool                *pool.Pool              // 代理池
	clients             map[string]*http.Client // 每个代理的HTTP客户端
	clientsMux          sync.RWMutex            // 客户端映射锁
	timeout             time.Duration           // 请求超时时间
	maxIdleConns        int                     // 总的最大空闲连接数
	maxIdleConnsPerHost int                     // 每个主机的最大空闲连接数
	maxRetryCount       int                     // 单个请求的最大代理重试次数
}

// NewClient 创建新的HTTP客户端管理器实例。
//
// 参数：
//   - proxyPool: 代理池实例，用于提供可用的代理服务器
//   - timeout: HTTP请求超时时间
//   - maxIdleConns: 总的最大空闲连接数
//   - maxIdleConnsPerHost: 每个主机的最大空闲连接数
//   - maxRetryCount: 单个请求的最大代理重试次数
//
// 返回值：
//   - *Client: 初始化完成的客户端管理器实例
func NewClient(proxyPool *pool.Pool, timeout time.Duration, maxIdleConns, maxIdleConnsPerHost, maxRetryCount int) *Client {
	return &Client{
		pool:                proxyPool,
		clients:             make(map[string]*http.Client),
		timeout:             timeout,
		maxIdleConns:        maxIdleConns,
		maxIdleConnsPerHost: maxIdleConnsPerHost,
		maxRetryCount:       maxRetryCount,
	}
}

// Do 通过代理服务器执行HTTP请求。
//
// 尝试使用代理池中的代理服务器执行请求，最多重试maxRetryCount次。
// 使用轮询机制选择代理，确保负载均衡。
//
// 参数：
//   - req: 要执行的HTTP请求
//
// 返回值：
//   - *http.Response: HTTP响应实例
//   - models.ProxyInfo: 成功使用的代理服务器信息
//   - error: 请求执行错误，成功时为nil
func (c *Client) Do(req *http.Request) (*http.Response, models.ProxyInfo, error) {
	if c.pool.Size() == 0 {
		return nil, models.ProxyInfo{}, fmt.Errorf("没有可用的代理")
	}

	// 计算实际重试次数：取maxRetryCount和代理池大小的较小值
	maxRetries := c.maxRetryCount
	if c.pool.Size() < maxRetries {
		maxRetries = c.pool.Size()
	}

	// 尝试指定次数的代理
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		proxy := c.pool.NextProxy()
		if proxy.Host == "" {
			continue
		}

		// 获取或创建对应的HTTP客户端
		client := c.getClient(proxy)

		// 执行请求
		resp, err := client.Do(req)
		if err == nil {
			return resp, proxy, nil
		}
		lastErr = err
	}

	return nil, models.ProxyInfo{}, fmt.Errorf("尝试了 %d 个代理后失败，最后错误: %v", maxRetries, lastErr)
}

// getClient 获取或创建指定代理的HTTP客户端。
//
// 使用双重检查锁定模式确保线程安全，避免重复创建客户端。
// 如果对应的客户端不存在，则创建新的客户端实例。
//
// 参数：
//   - proxy: 代理服务器信息
//
// 返回值：
//   - *http.Client: 对应的HTTP客户端实例
func (c *Client) getClient(proxy models.ProxyInfo) *http.Client {
	proxyKey := proxy.Host

	// 先尝试读锁获取现有客户端
	c.clientsMux.RLock()
	if client, exists := c.clients[proxyKey]; exists {
		c.clientsMux.RUnlock()
		return client
	}
	c.clientsMux.RUnlock()

	// 使用写锁创建新客户端
	c.clientsMux.Lock()
	defer c.clientsMux.Unlock()

	// 双重检查，防止并发创建
	if client, exists := c.clients[proxyKey]; exists {
		return client
	}

	// 创建新的HTTP客户端
	client := c.createClient(proxy)
	c.clients[proxyKey] = client

	return client
}

// createClient 创建新的HTTP客户端实例。
//
// 根据代理信息配置客户端，设置代理URL、认证信息、
// 连接池参数和超时配置。支持HTTP和HTTPS代理。
//
// 参数：
//   - proxy: 代理服务器信息
//
// 返回值：
//   - *http.Client: 配置完成的HTTP客户端实例
func (c *Client) createClient(proxy models.ProxyInfo) *http.Client {
	// 创建代理URL
	proxyURL := &url.URL{
		Scheme: proxy.URL.Scheme,
		Host:   proxy.Host,
	}

	// 添加认证信息
	if proxy.Username != "" {
		proxyURL.User = url.UserPassword(proxy.Username, proxy.Password)
	}

	// 创建传输层配置
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		MaxIdleConns:        c.maxIdleConns,
		MaxIdleConnsPerHost: c.maxIdleConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	// 如果需要认证，包一层添加Proxy-Authorization
	var rt http.RoundTripper = transport
	if proxy.Username != "" {
		authValue := auth.EncodeBasicAuth(proxy.Username, proxy.Password)
		rt = &proxyAuthTransport{base: transport, proxyAuth: authValue}
	}

	// 创建HTTP客户端
	return &http.Client{
		Transport: rt,
		Timeout:   c.timeout,
	}
}

// Close 清理所有客户端连接池。
//
// 关闭所有缓存的HTTP客户端的空闲连接，释放资源。
// 在服务关闭或重新初始化时调用。
func (c *Client) Close() {
	c.clientsMux.Lock()
	defer c.clientsMux.Unlock()

	for _, client := range c.clients {
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}

	c.clients = make(map[string]*http.Client)
}
