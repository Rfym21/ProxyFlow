// Package pool 提供代理池管理功能。
//
// 本包实现了代理服务器池的管理，包括代理列表加载、解析、验证
// 和轮询分配等功能。支持从多个本地文件和远程 HTTP(S) 列表读取
// 代理配置，自动解析代理URL和认证信息，跨来源去重，提供线程安全
// 的代理轮询机制。本地文件通过 fsnotify 监听变更自动热重载，
// 远程列表按固定间隔定时刷新。
package pool

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rfym21/ProxyFlow/internal/models"
)

const (
	// maxRemoteBodySize 远程代理列表响应体的最大读取字节数，防止异常响应打爆内存
	maxRemoteBodySize = 10 << 20 // 10MB

	// remoteFetchTimeout 拉取远程代理列表的超时时间
	remoteFetchTimeout = 30 * time.Second
)

// Options 代理池配置选项。
//
// Files 与 URLs 至少需要提供一项，两者可同时使用，
// 加载结果会合并并按代理 URL 去重（先出现的来源优先）。
type Options struct {
	Files           []string      // 本地代理列表文件路径
	URLs            []string      // 远程代理列表地址（http/https）
	URLInterval     time.Duration // 远程列表刷新间隔，<=0 表示只在启动时拉取一次
	FailureFilename string        // 失效代理记录文件路径
}

// Pool 代理池管理器。
//
// 管理多个代理服务器的连接信息，提供线程安全的
// 轮询分配机制，确保请求在所有可用代理之间均衡分配。
// 支持多来源加载：本地文件变更时自动热重载，远程列表定时刷新。
type Pool struct {
	proxies          []models.ProxyInfo // 代理列表
	current          int64              // 当前索引（原子操作）
	mu               sync.RWMutex       // 读写锁保护代理列表
	files            []string           // 本地代理文件绝对路径列表
	urls             []string           // 远程代理列表地址
	urlInterval      time.Duration      // 远程列表刷新间隔
	failureFilename  string             // 失效代理文件路径
	failed           map[string]bool    // 已知失效代理集合，用于过滤远程来源
	failedMu         sync.RWMutex       // 读写锁保护失效代理集合
	lastFailures     atomic.Value       // string，上次加载失败的来源摘要，用于抑制重复告警
	httpClient       *http.Client       // 拉取远程代理列表的客户端
	watcher          *fsnotify.Watcher  // 文件监听器
	stopChan         chan struct{}      // 停止信号通道
	stopOnce         sync.Once          // 确保停止信号只关闭一次
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
	return NewPoolWithOptions(Options{
		Files:           []string{filename},
		FailureFilename: failureFilename,
	})
}

// NewPoolWithOptions 按配置选项创建代理池实例。
//
// 合并加载所有本地文件与远程列表，跨来源按代理 URL 去重。
// 单个来源加载失败只输出警告并跳过，只要至少有一个来源成功
// 且最终解析出代理即可启动。为本地文件启动 fsnotify 监听，
// 配置了刷新间隔时为远程列表启动定时刷新。
//
// 参数：
//   - opts: 代理池配置选项
//
// 返回值：
//   - *Pool: 初始化完成的代理池实例
//   - error: 未配置来源、全部来源失败或未解析出任何代理时返回错误
func NewPoolWithOptions(opts Options) (*Pool, error) {
	files := normalizeFiles(opts.Files)
	urls := normalizeURLs(opts.URLs)

	if len(files) == 0 && len(urls) == 0 {
		return nil, fmt.Errorf("未配置任何代理来源，PROXY_FILE 与 PROXY_URLS 至少需要一项")
	}

	failureAbsPath, err := filepath.Abs(opts.FailureFilename)
	if err != nil {
		failureAbsPath = opts.FailureFilename
	}

	pool := &Pool{
		proxies:         make([]models.ProxyInfo, 0),
		current:         0,
		files:           files,
		urls:            urls,
		urlInterval:     opts.URLInterval,
		failureFilename: failureAbsPath,
		failed:          make(map[string]bool),
		httpClient:      &http.Client{Timeout: remoteFetchTimeout},
		stopChan:        make(chan struct{}),
	}

	// 先载入历史失效记录，避免远程列表把已剔除的代理反复加回来
	pool.loadFailedSet()

	if _, err := pool.loadProxies(); err != nil {
		return nil, err
	}

	if len(pool.proxies) == 0 {
		return nil, fmt.Errorf("在配置的代理来源中未找到有效的代理: %s",
			strings.Join(append(append([]string{}, files...), urls...), ", "))
	}

	log.Printf("从 %d 个来源加载了 %d 个代理（本地文件 %d，远程列表 %d）",
		len(files)+len(urls), len(pool.proxies), len(files), len(urls))

	// 启动本地文件监听
	if err := pool.startWatcher(); err != nil {
		log.Printf("警告: 无法启动文件监听，本地代理文件将不会自动重载: %v", err)
	}

	// 启动远程列表定时刷新
	if len(urls) > 0 && opts.URLInterval > 0 {
		go pool.refreshLoop()
		log.Printf("已启动远程代理列表定时刷新，间隔 %s", opts.URLInterval)
	}

	return pool, nil
}

// normalizeFiles 规整本地文件路径列表。
//
// 转换为绝对路径并去除重复项，保持原有顺序。
//
// 参数：
//   - files: 原始文件路径列表
//
// 返回值：
//   - []string: 去重后的绝对路径列表
func normalizeFiles(files []string) []string {
	result := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))

	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}

		absPath, err := filepath.Abs(file)
		if err != nil {
			absPath = file
		}

		if seen[absPath] {
			continue
		}
		seen[absPath] = true
		result = append(result, absPath)
	}

	return result
}

// normalizeURLs 规整远程列表地址。
//
// 校验协议合法性（仅接受 http/https）并去除重复项，
// 对无效地址输出警告并跳过。
//
// 参数：
//   - urls: 原始地址列表
//
// 返回值：
//   - []string: 去重后的有效地址列表
func normalizeURLs(urls []string) []string {
	result := make([]string, 0, len(urls))
	seen := make(map[string]bool, len(urls))

	for _, rawURL := range urls {
		trimmed := strings.TrimSpace(rawURL)
		if trimmed == "" {
			continue
		}

		parsed, err := url.Parse(trimmed)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			log.Printf("警告: 忽略无效的远程代理列表地址（仅支持 http/https）: %s", trimmed)
			continue
		}

		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}

	return result
}

// loadProxies 从所有来源加载代理列表。
//
// 依次读取本地文件与远程列表，合并后按代理 URL 去重并过滤
// 已知失效的远程代理。单个来源失败只警告并跳过；全部来源失败
// 时返回错误并保留原有列表。新列表为空但原列表非空时同样保留
// 原有列表，避免远程来源临时异常清空代理池。
//
// 返回值：
//   - bool: 代理列表相较加载前是否发生变化
//   - error: 加载错误，成功时为nil
func (p *Pool) loadProxies() (bool, error) {
	var (
		collected []models.ProxyInfo
		failures  []string
		anySucceed bool
	)

	for _, file := range p.files {
		proxies, err := p.loadFromFile(file)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", file, err))
			continue
		}
		anySucceed = true
		collected = append(collected, proxies...)
	}

	for _, remoteURL := range p.urls {
		proxies, err := p.loadFromURL(remoteURL)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", remoteURL, err))
			continue
		}
		anySucceed = true
		collected = append(collected, proxies...)
	}

	// 定时刷新会按固定节奏反复触发，故障未变化时不重复输出，避免刷屏
	failureSummary := strings.Join(failures, "; ")
	previousSummary, _ := p.lastFailures.Load().(string)
	p.lastFailures.Store(failureSummary)

	if !anySucceed {
		return false, fmt.Errorf("所有代理来源均加载失败: %s", failureSummary)
	}

	if failureSummary != previousSummary {
		for _, failure := range failures {
			log.Printf("警告: 代理来源加载失败，已跳过: %s", failure)
		}
	}

	newProxies := p.dedupe(collected)

	p.mu.Lock()
	defer p.mu.Unlock()

	// 来源临时异常导致结果为空时保留原列表，避免代理池被清空
	if len(newProxies) == 0 && len(p.proxies) > 0 {
		return false, fmt.Errorf("新的代理列表为空，保留原有的 %d 个代理", len(p.proxies))
	}

	changed := !sameProxies(p.proxies, newProxies)
	p.proxies = newProxies

	return changed, nil
}

// loadFromFile 从本地文件加载代理列表。
//
// 参数：
//   - filename: 代理文件绝对路径
//
// 返回值：
//   - []models.ProxyInfo: 解析出的代理列表
//   - error: 文件读取错误，成功时为nil
func (p *Pool) loadFromFile(filename string) ([]models.ProxyInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseLines(file, filename)
}

// loadFromURL 从远程地址拉取代理列表。
//
// 响应体格式与本地文件一致：每行一个代理，支持空行与 # 注释行。
// 读取长度上限为 maxRemoteBodySize。
//
// 参数：
//   - rawURL: 远程列表地址
//
// 返回值：
//   - []models.ProxyInfo: 解析出的代理列表
//   - error: 请求或读取错误，成功时为nil
func (p *Pool) loadFromURL(rawURL string) ([]models.ProxyInfo, error) {
	resp, err := p.httpClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}

	return parseLines(io.LimitReader(resp.Body, maxRemoteBodySize), rawURL)
}

// parseLines 逐行解析代理列表内容。
//
// 自动跳过空行和注释行，对无效的代理配置输出警告日志并跳过，
// 并为每个代理标记来源。
//
// 参数：
//   - reader: 代理列表内容
//   - source: 来源标识（本地文件绝对路径或远程 URL）
//
// 返回值：
//   - []models.ProxyInfo: 解析出的代理列表
//   - error: 读取错误，成功时为nil
func parseLines(reader io.Reader, source string) ([]models.ProxyInfo, error) {
	var proxies []models.ProxyInfo
	scanner := bufio.NewScanner(reader)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		proxyInfo, err := parseProxy(line)
		if err != nil {
			log.Printf("警告: %s 第 %d 行代理配置无效: %v", source, lineNum, err)
			continue
		}

		proxyInfo.Source = source
		proxies = append(proxies, *proxyInfo)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return proxies, nil
}

// dedupe 跨来源去重并过滤已知失效的远程代理。
//
// 相同代理 URL 只保留首次出现的那一个，因此来源配置顺序
// 决定了重复代理归属哪个来源。本地文件来源的失效代理由
// rewriteProxyFile 物理移除，这里只过滤远程来源。
//
// 参数：
//   - proxies: 合并后的原始代理列表
//
// 返回值：
//   - []models.ProxyInfo: 去重过滤后的代理列表
func (p *Pool) dedupe(proxies []models.ProxyInfo) []models.ProxyInfo {
	p.failedMu.RLock()
	defer p.failedMu.RUnlock()

	seen := make(map[string]bool, len(proxies))
	result := make([]models.ProxyInfo, 0, len(proxies))

	for _, proxyInfo := range proxies {
		key := formatProxyURL(proxyInfo)

		if seen[key] {
			continue
		}

		// 远程列表无法回写，失效代理只能在每次加载时过滤掉
		if proxyInfo.IsRemoteSource() && p.failed[key] {
			continue
		}

		seen[key] = true
		result = append(result, proxyInfo)
	}

	return result
}

// sameProxies 比较两个代理列表是否等价。
//
// 顺序敏感：来源顺序固定，顺序变化本身即视为列表变化。
//
// 参数：
//   - a: 代理列表
//   - b: 代理列表
//
// 返回值：
//   - bool: 两个列表完全一致时返回 true
func sameProxies(a, b []models.ProxyInfo) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Host != b[i].Host ||
			a[i].Username != b[i].Username ||
			a[i].Password != b[i].Password ||
			a[i].Type != b[i].Type {
			return false
		}
	}

	return true
}

// loadFailedSet 从失效代理文件载入已知失效代理集合。
//
// 文件不存在属于正常情况（首次运行），静默跳过。
func (p *Pool) loadFailedSet() {
	file, err := os.Open(p.failureFilename)
	if err != nil {
		return
	}
	defer file.Close()

	p.failedMu.Lock()
	defer p.failedMu.Unlock()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p.failed[line] = true
	}
}

// Reload 手动重载代理列表。
//
// 重新从所有来源加载代理列表，线程安全。
//
// 返回值：
//   - error: 重载错误，成功时为nil
func (p *Pool) Reload() error {
	if _, err := p.loadProxies(); err != nil {
		return err
	}

	log.Printf("代理列表已重载，当前共 %d 个代理", p.Size())
	return nil
}

// reloadFromWatch 响应文件监听事件重载代理列表。
//
// 与 Reload 的区别在于只在列表实际发生变化时输出日志：
// MarkFailed 重写代理文件同样会触发监听事件，此时列表已经是
// 最新状态，重载虽然幂等但不应产生日志噪音。
func (p *Pool) reloadFromWatch() {
	changed, err := p.loadProxies()
	if err != nil {
		log.Printf("重载代理列表失败: %v", err)
		return
	}

	if changed {
		log.Printf("检测到代理文件变化，已重载，当前共 %d 个代理", p.Size())
	}
}

// startWatcher 启动文件监听器。
//
// 监听所有本地代理文件所在的目录，文件变化时自动重载代理列表。
// 未配置本地文件时（纯远程来源）直接返回。
//
// 返回值：
//   - error: 启动错误，成功时为nil
func (p *Pool) startWatcher() error {
	if len(p.files) == 0 {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// 监听文件所在目录（fsnotify 对文件直接监听在某些系统上有问题），
	// 多个代理文件可能位于同一目录，需要去重后再注册
	watched := make(map[string]bool, len(p.files))
	for _, file := range p.files {
		dir := filepath.Dir(file)
		if watched[dir] {
			continue
		}

		if err := watcher.Add(dir); err != nil {
			watcher.Close()
			return err
		}
		watched[dir] = true
	}

	p.watcher = watcher
	go p.watchLoop()

	log.Printf("已启动代理文件监听: %s", strings.Join(p.files, ", "))
	return nil
}

// watchLoop 文件监听循环。
//
// 监听文件事件，当任一代理文件发生变化时自动重载。
func (p *Pool) watchLoop() {
	// 按绝对路径匹配，避免不同目录下的同名文件互相误触发
	targets := make(map[string]bool, len(p.files))
	for _, file := range p.files {
		targets[file] = true
	}

	for {
		select {
		case event, ok := <-p.watcher.Events:
			if !ok {
				return
			}

			name := event.Name
			if absPath, err := filepath.Abs(name); err == nil {
				name = absPath
			}

			// 只处理目标文件的写入和创建事件
			if targets[name] && event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				p.reloadFromWatch()
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

// refreshLoop 远程代理列表定时刷新循环。
//
// 按配置的间隔重新加载全部来源，仅在列表实际发生变化时输出日志，
// 避免固定间隔的刷新产生无意义的日志噪音。来源持续故障时同样只在
// 错误内容变化时输出一次，并在恢复后给出提示。
func (p *Pool) refreshLoop() {
	ticker := time.NewTicker(p.urlInterval)
	defer ticker.Stop()

	var lastErr string

	for {
		select {
		case <-ticker.C:
			changed, err := p.loadProxies()
			if err != nil {
				if message := err.Error(); message != lastErr {
					log.Printf("刷新代理列表失败: %v", err)
					lastErr = message
				}
				continue
			}

			if lastErr != "" {
				log.Printf("代理来源已恢复，当前共 %d 个代理", p.Size())
				lastErr = ""
			}

			if changed {
				log.Printf("远程代理列表已更新，当前共 %d 个代理", p.Size())
			}

		case <-p.stopChan:
			return
		}
	}
}

// Close 关闭代理池，停止文件监听与远程刷新。
//
// 可安全重复调用。
func (p *Pool) Close() {
	p.stopOnce.Do(func() {
		close(p.stopChan)
		if p.watcher != nil {
			p.watcher.Close()
		}
	})
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
//   - *models.ProxyInfo: 解析后的代理信息结构
//   - error: 解析错误，成功时为nil
func parseProxy(proxyStr string) (*models.ProxyInfo, error) {
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

// Sources 获取代理池配置的所有来源。
//
// 返回值：
//   - []string: 本地文件绝对路径与远程列表地址
func (p *Pool) Sources() []string {
	sources := make([]string, 0, len(p.files)+len(p.urls))
	sources = append(sources, p.files...)
	sources = append(sources, p.urls...)
	return sources
}

// MarkFailed 标记代理为失效。
//
// 将代理从内存移除并追加到失效代理文件。来源为本地文件时
// 同步重写该文件（仅重写这一个文件，不影响其它来源）；来源为
// 远程列表时无法回写，改为记入内存失效集合，在后续刷新中过滤。
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
		currentURL := formatProxyURL(p.proxies[i])
		if currentURL != proxyURL {
			newProxies = append(newProxies, p.proxies[i])
		} else {
			removed = &p.proxies[i]
		}
	}

	if removed == nil {
		return // 代理不存在
	}

	source := removed.Source
	isRemote := removed.IsRemoteSource()
	p.proxies = newProxies

	// 记入失效集合：远程来源的代理只能靠它在刷新时过滤掉
	p.failedMu.Lock()
	p.failed[proxyURL] = true
	p.failedMu.Unlock()

	// 追加到失效代理文件
	if err := p.appendToFile(p.failureFilename, proxyURL); err != nil {
		log.Printf("写入失效代理文件失败: %v", err)
	}

	// 仅重写该代理所属的本地文件，避免把一个来源的内容写进另一个文件
	if !isRemote && source != "" {
		if err := p.rewriteProxyFile(source); err != nil {
			log.Printf("更新代理文件 %s 失败: %v", source, err)
		}
	}

	log.Printf("代理 %s 已标记为失效，剩余 %d 个代理", proxyURL, len(p.proxies))
}

// formatProxyURL 格式化代理 URL。
func formatProxyURL(proxyInfo models.ProxyInfo) string {
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

// rewriteProxyFile 重写指定的代理文件。
//
// 保留原文件的注释行与空行，并只写回来源为该文件的代理，
// 其它来源的代理不会被混写进来。调用方必须已持有 p.mu 写锁。
//
// 参数：
//   - filename: 待重写的代理文件绝对路径
//
// 返回值：
//   - error: 写入错误，成功时为nil
func (p *Pool) rewriteProxyFile(filename string) error {
	// 读取原文件保留注释行
	originalLines := make([]string, 0)
	if file, err := os.Open(filename); err == nil {
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

	// 只写入来源为该文件的代理
	for _, proxy := range p.proxies {
		if proxy.Source == filename {
			content.WriteString(formatProxyURL(proxy) + "\n")
		}
	}

	// 写入文件
	return os.WriteFile(filename, []byte(content.String()), 0644)
}
