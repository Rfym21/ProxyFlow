// Package pool 提供代理池管理功能。
//
// 本包实现了代理服务器池的管理，包括代理列表加载、解析、验证
// 和轮询分配等功能。支持从文件读取代理配置，自动解析代理URL
// 和认证信息，提供线程安全的代理轮询机制。
package pool

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/rfym21/ProxyFlow/internal/models"
)

// Pool 代理池管理器。
//
// 管理多个代理服务器的连接信息，提供线程安全的
// 轮询分配机制，确保请求在所有可用代理之间均衡分配。
type Pool struct {
	proxies []models.ProxyInfo // 代理列表
	current int64              // 当前索引（原子操作）
}

// NewPool 创建新的代理池实例。
//
// 从指定的文件加载代理列表，解析每个代理的URL和认证信息。
// 支持空行和以#开头的注释行。
//
// 参数：
//   - filename: 代理列表文件路径
//
// 返回值：
//   - *Pool: 初始化完成的代理池实例
//   - error: 初始化错误，成功时为nil
func NewPool(filename string) (*Pool, error) {
	pool := &Pool{
		proxies: make([]models.ProxyInfo, 0),
		current: 0,
	}

	if err := pool.loadProxies(filename); err != nil {
		return nil, err
	}

	if len(pool.proxies) == 0 {
		return nil, fmt.Errorf("在 %s 中未找到有效的代理", filename)
	}

	log.Printf("从 %s 加载了 %d 个代理", filename, len(pool.proxies))
	return pool, nil
}

// loadProxies 从文件加载代理列表。
//
// 逐行读取文件内容，解析每个代理URL并添加到代理池中。
// 自动跳过无效行，对无效的代理配置输出警告日志。
//
// 参数：
//   - filename: 代理列表文件路径
//
// 返回值：
//   - error: 文件读取错误，成功时为nil
func (p *Pool) loadProxies(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		proxyInfo, err := p.parseProxy(line)
		if err != nil {
			log.Printf("警告: 第 %d 行代理配置无效: %v", lineNum, err)
			continue
		}

		p.proxies = append(p.proxies, *proxyInfo)
	}

	return scanner.Err()
}

// parseProxy 解析代理字符串。
//
// 将代理URL字符串解析为ProxyInfo结构，提取协议、
// 主机地址和认证信息。支持 HTTP、HTTPS 和 SOCKS5 协议。
//
// 参数：
//   - proxyStr: 代理URL字符串，格式为scheme://[user:pass@]host:port
//
// 返回值：
//   - *ProxyInfo: 解析后的代理信息结构
//   - error: 解析错误，成功时为nil
func (p *Pool) parseProxy(proxyStr string) (*models.ProxyInfo, error) {
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil, fmt.Errorf("无效的代理URL: %v", err)
	}

	// 确定代理类型
	var proxyType models.ProxyType
	switch proxyURL.Scheme {
	case "http", "https":
		proxyType = models.ProxyTypeHTTP
	case "socks5":
		proxyType = models.ProxyTypeSOCKS5
	default:
		return nil, fmt.Errorf("不支持的代理协议: %s (支持: http, https, socks5)", proxyURL.Scheme)
	}

	proxyInfo := &models.ProxyInfo{
		URL:  proxyURL,
		Host: proxyURL.Host,
		Type: proxyType,
	}

	// 提取认证信息
	if proxyURL.User != nil {
		proxyInfo.Username = proxyURL.User.Username()
		if password, ok := proxyURL.User.Password(); ok {
			proxyInfo.Password = password
		}
	}

	return proxyInfo, nil
}

// NextProxy 获取下一个代理服务器信息。
//
// 使用原子操作实现线程安全的轮询算法，确保在多并发
// 环境下正确分配代理。如果代理池为空，返回空的ProxyInfo。
//
// 返回值：
//   - ProxyInfo: 下一个可用的代理服务器信息
func (p *Pool) NextProxy() models.ProxyInfo {
	if len(p.proxies) == 0 {
		return models.ProxyInfo{}
	}

	// 原子操作获取下一个索引
	index := atomic.AddInt64(&p.current, 1) % int64(len(p.proxies))
	return p.proxies[index]
}

// Size 获取代理池中的代理数量。
//
// 返回值：
//   - int: 当前代理池中的代理服务器数量
func (p *Pool) Size() int {
	return len(p.proxies)
}
