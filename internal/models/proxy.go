// Package models 定义项目中使用的数据结构。
//
// 本包包含了代理服务器项目中使用的所有数据模型和结构定义，
// 主要用于表示代理信息、配置参数等核心数据类型。
package models

import (
	"net/url"
	"strings"
)

// ProxyType 代理协议类型。
type ProxyType int

const (
	// ProxyTypeHTTP HTTP/HTTPS 代理协议
	ProxyTypeHTTP ProxyType = iota
	// ProxyTypeSOCKS5 SOCKS5 代理协议
	ProxyTypeSOCKS5
)

// ProxyInfo 代理服务器信息结构。
//
// 存储单个代理服务器的连接信息，包括网络地址、
// 认证凭据和连接参数等。
type ProxyInfo struct {
	URL      *url.URL  // 代理URL
	Host     string    // 代理主机地址
	Username string    // 认证用户名
	Password string    // 认证密码
	Type     ProxyType // 代理协议类型
	Source   string    // 来源标识：本地文件绝对路径，或远程列表 URL
}

// IsRemoteSource 判断代理是否来自远程列表。
//
// 远程来源的代理无法回写，失效时只能记入内存失效集合。
//
// 返回值：
//   - bool: 来源为 http/https 远程列表时返回 true
func (p *ProxyInfo) IsRemoteSource() bool {
	return strings.HasPrefix(p.Source, "http://") || strings.HasPrefix(p.Source, "https://")
}

// IsSOCKS5 判断是否为 SOCKS5 代理。
func (p *ProxyInfo) IsSOCKS5() bool {
	return p.Type == ProxyTypeSOCKS5
}

// IsHTTP 判断是否为 HTTP/HTTPS 代理。
func (p *ProxyInfo) IsHTTP() bool {
	return p.Type == ProxyTypeHTTP
}
