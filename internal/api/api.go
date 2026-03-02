// Package api 提供代理获取 API 服务。
//
// 本包实现了 HTTP API 服务，支持通过接口获取代理列表，
// 可选的代理有效性检查和失效代理自动移除功能。
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/proxy"

	"github.com/rfym21/ProxyFlow/internal/models"
	"github.com/rfym21/ProxyFlow/internal/pool"
)

// Server API 服务器。
type Server struct {
	pool             *pool.Pool
	apiKey           string
	checkURL         string
	checkRetry       int
	checkTimeout     time.Duration
	server           *http.Server
}

// Response API 响应结构。
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// ProxyResult 代理结果。
type ProxyResult struct {
	Proxy   string `json:"proxy"`
	Checked bool   `json:"checked"`
	Valid   bool   `json:"valid,omitempty"`
}

// NewServer 创建新的 API 服务器实例。
//
// 参数：
//   - proxyPool: 代理池实例
//   - apiKey: API 访问密钥
//   - checkURL: 代理检查 URL
//   - checkRetry: 检查失败重试次数
//
// 返回值：
//   - *Server: API 服务器实例
func NewServer(proxyPool *pool.Pool, apiKey, checkURL string, checkRetry int) *Server {
	return &Server{
		pool:         proxyPool,
		apiKey:       apiKey,
		checkURL:     checkURL,
		checkRetry:   checkRetry,
		checkTimeout: 10 * time.Second,
	}
}

// Start 启动 API 服务器。
//
// 参数：
//   - port: 监听端口号
//
// 返回值：
//   - error: 服务器启动错误
func (s *Server) Start(port string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/get-proxy", s.handleGetProxy)
	mux.HandleFunc("/api/status", s.handleStatus)

	s.server = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("API 服务器正在端口 %s 上启动", port)
	return s.server.ListenAndServe()
}

// Shutdown 关闭 API 服务器。
func (s *Server) Shutdown() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleGetProxy 处理获取代理请求。
//
// 请求参数：
//   - key: API 密钥（必需，如果配置了 API_KEY）
//   - number: 获取代理数量，默认 1
//   - check: 是否检查代理有效性，默认 false
func (s *Server) handleGetProxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 验证 API 密钥
	if s.apiKey != "" {
		key := r.URL.Query().Get("key")
		if key != s.apiKey {
			s.writeJSON(w, http.StatusUnauthorized, Response{
				Success: false,
				Message: "无效的 API 密钥",
			})
			return
		}
	}

	// 解析参数
	numberStr := r.URL.Query().Get("number")
	number := 1
	if numberStr != "" {
		if n, err := strconv.Atoi(numberStr); err == nil && n > 0 {
			number = n
		}
	}

	checkStr := r.URL.Query().Get("check")
	check := checkStr == "true" || checkStr == "1"

	// 获取代理
	results := make([]ProxyResult, 0, number)
	usedProxies := make(map[string]bool)

	for i := 0; i < number; i++ {
		var result ProxyResult
		var proxyInfo models.ProxyInfo
		var valid bool

		if check {
			// 需要检查代理有效性
			proxyInfo, valid = s.getValidProxy(usedProxies)
			if proxyInfo.Host == "" {
				break // 没有更多可用代理
			}
			result = ProxyResult{
				Proxy:   s.formatProxyURL(proxyInfo),
				Checked: true,
				Valid:   valid,
			}
		} else {
			// 不检查，直接获取
			for retry := 0; retry < s.pool.Size(); retry++ {
				proxyInfo = s.pool.NextProxy()
				proxyURL := s.formatProxyURL(proxyInfo)
				if !usedProxies[proxyURL] {
					usedProxies[proxyURL] = true
					break
				}
			}
			if proxyInfo.Host == "" {
				break
			}
			result = ProxyResult{
				Proxy:   s.formatProxyURL(proxyInfo),
				Checked: false,
			}
		}

		results = append(results, result)
	}

	if len(results) == 0 {
		s.writeJSON(w, http.StatusOK, Response{
			Success: false,
			Message: "没有可用的代理",
		})
		return
	}

	s.writeJSON(w, http.StatusOK, Response{
		Success: true,
		Data:    results,
	})
}

// handleStatus 处理状态查询请求。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 验证 API 密钥
	if s.apiKey != "" {
		key := r.URL.Query().Get("key")
		if key != s.apiKey {
			s.writeJSON(w, http.StatusUnauthorized, Response{
				Success: false,
				Message: "无效的 API 密钥",
			})
			return
		}
	}

	s.writeJSON(w, http.StatusOK, Response{
		Success: true,
		Data: map[string]interface{}{
			"pool_size": s.pool.Size(),
			"check_url": s.checkURL,
		},
	})
}

// getValidProxy 获取一个有效的代理。
//
// 会尝试最多 checkRetry 次获取有效代理，
// 无效代理会被标记并从代理池移除。
//
// 参数：
//   - usedProxies: 已使用的代理集合
//
// 返回值：
//   - models.ProxyInfo: 代理信息
//   - bool: 是否有效
func (s *Server) getValidProxy(usedProxies map[string]bool) (models.ProxyInfo, bool) {
	tried := 0
	maxTries := s.checkRetry * s.pool.Size() // 最多尝试次数

	for tried < maxTries {
		proxyInfo := s.pool.NextProxy()
		if proxyInfo.Host == "" {
			return proxyInfo, false
		}

		proxyURL := s.formatProxyURL(proxyInfo)

		// 跳过已使用的代理
		if usedProxies[proxyURL] {
			tried++
			continue
		}

		// 检查代理有效性
		if s.checkProxy(proxyInfo) {
			usedProxies[proxyURL] = true
			return proxyInfo, true
		}

		// 代理无效，标记为失效
		log.Printf("代理 %s 检查失败，标记为失效", proxyURL)
		s.pool.MarkFailed(proxyURL)

		tried++
	}

	return models.ProxyInfo{}, false
}

// checkProxy 检查代理是否有效。
//
// 参数：
//   - proxyInfo: 代理信息
//
// 返回值：
//   - bool: 代理是否有效
func (s *Server) checkProxy(proxyInfo models.ProxyInfo) bool {
	var httpClient *http.Client

	if proxyInfo.IsSOCKS5() {
		// SOCKS5 代理
		var auth *proxy.Auth
		if proxyInfo.Username != "" {
			auth = &proxy.Auth{
				User:     proxyInfo.Username,
				Password: proxyInfo.Password,
			}
		}

		dialer, err := proxy.SOCKS5("tcp", proxyInfo.Host, auth, proxy.Direct)
		if err != nil {
			return false
		}

		httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: s.checkTimeout,
		}
	} else {
		// HTTP 代理
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   proxyInfo.Host,
		}
		if proxyInfo.Username != "" {
			proxyURL.User = url.UserPassword(proxyInfo.Username, proxyInfo.Password)
		}

		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: s.checkTimeout,
		}
	}

	// 发送请求检查
	resp, err := httpClient.Get(s.checkURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// formatProxyURL 格式化代理 URL。
func (s *Server) formatProxyURL(proxyInfo models.ProxyInfo) string {
	scheme := "http"
	if proxyInfo.IsSOCKS5() {
		scheme = "socks5"
	}

	if proxyInfo.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s", scheme, proxyInfo.Username, proxyInfo.Password, proxyInfo.Host)
	}
	return fmt.Sprintf("%s://%s", scheme, proxyInfo.Host)
}

// writeJSON 写入 JSON 响应。
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
