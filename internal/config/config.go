// Package config 提供应用程序配置管理功能。
//
// 本包负责从环境变量加载应用配置，包括代理服务端口、代理文件路径、
// 连接池大小、超时设置和认证参数等。提供了类型安全的配置访问接口，
// 支持默认值设置和环境变量覆盖。
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 应用程序配置结构。
//
// 包含了代理服务器运行所需的所有配置参数，包括网络设置、
// 资源配置和认证参数等。
type Config struct {
	ProxyPort            string        // 代理服务监听端口（HTTP/SOCKS5 统一入口）
	ProxyFile            string        // 代理文件路径原始值（逗号分隔，用于日志展示）
	ProxyFiles           []string      // 代理文件路径列表（解析自 PROXY_FILE）
	ProxyURLs            []string      // 远程代理列表地址（解析自 PROXY_URLS）
	ProxyURLInterval     time.Duration // 远程代理列表刷新间隔，0 表示只在启动时拉取一次
	FailureProxyFile     string        // 失效代理文件路径
	MaxIdleConns         int           // 总的最大空闲连接数
	MaxIdleConnsPerHost  int           // 每个主机的最大空闲连接数
	MaxRetryCount        int           // 单个请求的最大代理重试次数
	RequestTimeout       time.Duration // 请求超时时间
	AuthUsername         string        // 代理服务器认证用户名
	AuthPassword         string        // 代理服务器认证密码
	AccessLog            bool          // 是否输出访问日志（每连接、每请求各一条）

	// API 配置
	APIEnabled  bool   // 是否启用 API
	APIPort     string // API 服务端口
	APIKey      string // API 访问密钥
	CheckURL    string // 代理检查 URL
	CheckRetry  int    // 代理检查失败重试次数
}

// Load 从环境变量加载应用配置。
//
// 读取环境变量并返回填充了默认值的配置实例。
// 如果环境变量不存在或格式不正确，将使用默认值。
//
// 返回值：
//   - *Config: 配置实例指针
func Load() *Config {
	proxyURLs := splitList(getEnv("PROXY_URLS", ""))

	// 配置了远程来源时不再默认加载 proxy.txt：
	// 纯远程部署通常没有本地文件，否则每次加载都会产生找不到文件的告警。
	// 需要两者并用时显式设置 PROXY_FILE 即可。
	defaultProxyFile := "proxy.txt"
	if len(proxyURLs) > 0 {
		defaultProxyFile = ""
	}
	proxyFile := getEnv("PROXY_FILE", defaultProxyFile)

	return &Config{
		ProxyPort:            getEnv("PROXY_PORT", "8280"),
		ProxyFile:            proxyFile,
		ProxyFiles:           splitList(proxyFile),
		ProxyURLs:            proxyURLs,
		ProxyURLInterval:     time.Duration(getEnvInt("PROXY_URL_INTERVAL", 300)) * time.Second,
		FailureProxyFile:     getEnv("FAILURE_PROXY_FILE", "failure-proxy.txt"),
		MaxIdleConns:         getEnvInt("MAX_IDLE_CONNS", 1000),
		MaxIdleConnsPerHost:  getEnvInt("MAX_IDLE_CONNS_PER_HOST", 100),
		MaxRetryCount:        getEnvInt("MAX_RETRY_COUNT", 3),
		RequestTimeout:       time.Duration(getEnvInt("REQUEST_TIMEOUT", 30)) * time.Second,
		AuthUsername:         getEnv("AUTH_USERNAME", ""),
		AuthPassword:         getEnv("AUTH_PASSWORD", ""),
		AccessLog:            getEnvBool("ACCESS_LOG", true),

		// API 配置
		APIEnabled:  getEnvBool("API_ENABLED", false),
		APIPort:     getEnv("API_PORT", "8281"),
		APIKey:      getEnv("API_KEY", ""),
		CheckURL:    getEnv("CHECK_URL", "https://www.google.com"),
		CheckRetry:  getEnvInt("CHECK_RETRY", 5),
	}
}

// splitList 按逗号切分配置值为字符串列表。
//
// 自动去除每项两端空白并丢弃空项，便于 PROXY_FILE、PROXY_URLS
// 这类「单值或多值」配置沿用同一个环境变量。
//
// 参数：
//   - value: 逗号分隔的原始配置值
//
// 返回值：
//   - []string: 切分后的非空项列表，输入为空时返回 nil
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getEnvBool 获取环境变量布尔值。
//
// 参数：
//   - key: 环境变量名称
//   - defaultValue: 默认值，当环境变量不存在时使用
//
// 返回值：
//   - bool: 解析后的布尔值或默认值
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

// getEnv 获取环境变量字符串值。
//
// 参数：
//   - key: 环境变量名称
//   - defaultValue: 默认值，当环境变量不存在时使用
//
// 返回值：
//   - string: 环境变量值或默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt 获取环境变量整数值。
//
// 参数：
//   - key: 环境变量名称
//   - defaultValue: 默认值，当环境变量不存在或解析失败时使用
//
// 返回值：
//   - int: 解析后的整数值或默认值
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
