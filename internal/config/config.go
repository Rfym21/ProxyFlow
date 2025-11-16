// Package config 提供应用程序配置管理功能。
//
// 本包负责从环境变量加载应用配置，包括代理服务端口、代理文件路径、
// 连接池大小、超时设置和认证参数等。提供了类型安全的配置访问接口，
// 支持默认值设置和环境变量覆盖。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 应用程序配置结构。
//
// 包含了代理服务器运行所需的所有配置参数，包括网络设置、
// 资源配置和认证参数等。
type Config struct {
	ProxyPort            string        // 代理服务监听端口
	ProxyFile            string        // 代理文件路径
	MaxIdleConns         int           // 总的最大空闲连接数
	MaxIdleConnsPerHost  int           // 每个主机的最大空闲连接数
	RequestTimeout       time.Duration // 请求超时时间
	AuthUsername         string        // 代理服务器认证用户名
	AuthPassword         string        // 代理服务器认证密码
}

// Load 从环境变量加载应用配置。
//
// 读取环境变量并返回填充了默认值的配置实例。
// 如果环境变量不存在或格式不正确，将使用默认值。
//
// 返回值：
//   - *Config: 配置实例指针
func Load() *Config {
	return &Config{
		ProxyPort:            getEnv("PROXY_PORT", "8282"),
		ProxyFile:            getEnv("PROXY_FILE", "proxy.txt"),
		MaxIdleConns:         getEnvInt("MAX_IDLE_CONNS", 1000),
		MaxIdleConnsPerHost:  getEnvInt("MAX_IDLE_CONNS_PER_HOST", 100),
		RequestTimeout:       time.Duration(getEnvInt("REQUEST_TIMEOUT", 30)) * time.Second,
		AuthUsername:         getEnv("AUTH_USERNAME", ""),
		AuthPassword:         getEnv("AUTH_PASSWORD", ""),
	}
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
