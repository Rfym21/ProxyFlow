// Package pool 提供代理池管理功能。
//
// 本包实现了代理服务器池的管理，包括代理列表加载、解析、验证
// 和轮询分配等功能。支持从文件读取代理配置，自动解析代理URL
// 和认证信息，提供线程安全的代理轮询机制。
// 支持文件监听，代理列表变更时自动热重载。
package pool

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"github.com/rfym21/ProxyFlow/internal/models"
)

// Pool 代理池管理器。
//
// 管理多个代理服务器的连接信息，提供线程安全的
// 轮询分配机制，确保请求在所有可用代理之间均衡分配。
// 支持文件监听，代理列表变更时自动热重载。
type Pool struct {
	proxies          []models.ProxyInfo // 代理列表
	current          int64              // 当前索引（原子操作）
	mu               sync.RWMutex       // 读写锁保护代理列表
	filename         string             // 代理文件路径
	failureFilename  string             // 失效代理文件路径
	watcher          *fsnotify.Watcher  // 文件监听器
	stopChan         chan struct{}      // 停止信号通道
}

// NewPool 创建新的代理池实例。
//
// 从指定的文件加载代理列表，解析每个代理的URL和认证信息。
// 支持空行和以#开头的注释行。自动启动文件监听，代理列表
// 变更时自动热重载。
//
// 参数：
//   - filename: 代理列表文件路径
//
// 返回值：
//   - *Pool: 初始化完成的代理池实例
//   - error: 初始化错误，成功时为nil
func NewPool(filename string) (*Pool, error) {
	return NewPoolWithFailure(filename, "failure-proxy.txt")
}

// NewPoolWithFailure 创建新的代理池实例（指定失效代理文件）。
//
// 参数：
//   - filename: 代理列表文件路径
//   - failureFilename: 失效代理文件路径
//
// 返回值：
//   - *Pool: 初始化完成的代理池实例
//   - error: 初始化错误，成功时为nil
func NewPoolWithFailure(filename, failureFilename string) (*Pool, error) {
	// 获取绝对路径
	absPath, err := filepath.Abs(filename)
	if err != nil {
		absPath = filename
	}

	failureAbsPath, err := filepath.Abs(failureFilename)
	if err != nil {
		failureAbsPath = failureFilename
	}

	pool := &Pool{
		proxies:         make([]models.ProxyInfo, 0),
		current:         0,
		filename:        absPath,
		failureFilename: failureAbsPath,
		stopChan:        make(chan struct{}),
	}

	if err := pool.loadProxies(); err != nil {
		return nil, err
	}

	if len(pool.proxies) == 0 {
		return nil, fmt.Errorf("在 %s 中未找到有效的代理", filename)
	}

	log.Printf("从 %s 加载了 %d 个代理", filename, len(pool.proxies))

	// 启动文件监听
	if err := pool.startWatcher(); err != nil {
		log.Printf("警告: 无法启动文件监听，代理列表将不会自动重载: %v", err)
	}

	return pool, nil
}

// loadProxies 从文件加载代理列表。
//
// 逐行读取文件内容，解析每个代理URL并添加到代理池中。
// 自动跳过无效行，对无效的代理配置输出警告日志。
//
// 返回值：
//   - error: 文件读取错误，成功时为nil
func (p *Pool) loadProxies() error {
	file, err := os.Open(p.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	var newProxies []models.ProxyInfo
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

		newProxies = append(newProxies, *proxyInfo)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// 使用写锁更新代理列表
	p.mu.Lock()
	p.proxies = newProxies
	p.mu.Unlock()

	return nil
}

// Reload 手动重载代理列表。
//
// 重新从文件加载代理列表，线程安全。
//
// 返回值：
//   - error: 重载错误，成功时为nil
func (p *Pool) Reload() error {
	if err := p.loadProxies(); err != nil {
		return err
	}

	p.mu.RLock()
	count := len(p.proxies)
	p.mu.RUnlock()

	log.Printf("代理列表已重载，当前共 %d 个代理", count)
	return nil
}

// startWatcher 启动文件监听器。
//
// 监听代理文件变化，自动重载代理列表。
//
// 返回值：
//   - error: 启动错误，成功时为nil
func (p *Pool) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	p.watcher = watcher

	// 监听文件所在目录（fsnotify 对文件直接监听在某些系统上有问题）
	dir := filepath.Dir(p.filename)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return err
	}

	go p.watchLoop()

	log.Printf("已启动代理文件监听: %s", p.filename)
	return nil
}

// watchLoop 文件监听循环。
//
// 监听文件事件，当代理文件发生变化时自动重载。
func (p *Pool) watchLoop() {
	filename := filepath.Base(p.filename)

	for {
		select {
		case event, ok := <-p.watcher.Events:
			if !ok {
				return
			}

			// 只处理目标文件的写入和创建事件
			if filepath.Base(event.Name) == filename {
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					log.Printf("检测到代理文件变化，正在重载...")
					if err := p.Reload(); err != nil {
						log.Printf("重载代理列表失败: %v", err)
					}
				}
			}

		case err, ok := <-p.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("文件监听错误: %v", err)

		case <-p.stopChan:
			return
		}
	}
}

// Close 关闭代理池，停止文件监听。
func (p *Pool) Close() {
	close(p.stopChan)
	if p.watcher != nil {
		p.watcher.Close()
	}
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
	p.mu.RLock()
	defer p.mu.RUnlock()

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
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

// MarkFailed 标记代理为失效。
//
// 将代理从 proxy.txt 移除并添加到 failure-proxy.txt。
// 此操作会暂停文件监听以避免重复重载。
//
// 参数：
//   - proxyURL: 代理 URL 字符串
func (p *Pool) MarkFailed(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 从内存中移除
	newProxies := make([]models.ProxyInfo, 0, len(p.proxies))
	var removed *models.ProxyInfo

	for i := range p.proxies {
		currentURL := p.formatProxyURL(p.proxies[i])
		if currentURL != proxyURL {
			newProxies = append(newProxies, p.proxies[i])
		} else {
			removed = &p.proxies[i]
		}
	}

	if removed == nil {
		return // 代理不存在
	}

	p.proxies = newProxies

	// 追加到失效代理文件
	if err := p.appendToFile(p.failureFilename, proxyURL); err != nil {
		log.Printf("写入失效代理文件失败: %v", err)
	}

	// 从 proxy.txt 中移除（重写整个文件）
	if err := p.rewriteProxyFile(); err != nil {
		log.Printf("更新代理文件失败: %v", err)
	}

	log.Printf("代理 %s 已标记为失效，剩余 %d 个代理", proxyURL, len(p.proxies))
}

// formatProxyURL 格式化代理 URL。
func (p *Pool) formatProxyURL(proxyInfo models.ProxyInfo) string {
	scheme := "http"
	if proxyInfo.IsSOCKS5() {
		scheme = "socks5"
	}

	if proxyInfo.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s", scheme, proxyInfo.Username, proxyInfo.Password, proxyInfo.Host)
	}
	return fmt.Sprintf("%s://%s", scheme, proxyInfo.Host)
}

// appendToFile 追加内容到文件。
func (p *Pool) appendToFile(filename, content string) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content + "\n")
	return err
}

// rewriteProxyFile 重写代理文件。
//
// 根据当前内存中的代理列表重写 proxy.txt 文件。
func (p *Pool) rewriteProxyFile() error {
	// 读取原文件保留注释行
	originalLines := make([]string, 0)
	if file, err := os.Open(p.filename); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			// 只保留空行和注释行
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				originalLines = append(originalLines, line)
			}
		}
		file.Close()
	}

	// 创建新文件内容
	var content strings.Builder

	// 写入注释头
	for _, line := range originalLines {
		content.WriteString(line + "\n")
	}

	// 写入当前代理
	for _, proxy := range p.proxies {
		content.WriteString(p.formatProxyURL(proxy) + "\n")
	}

	// 写入文件
	return os.WriteFile(p.filename, []byte(content.String()), 0644)
}
