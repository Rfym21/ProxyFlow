package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/rfym21/ProxyFlow/internal/api"
	"github.com/rfym21/ProxyFlow/internal/config"
	"github.com/rfym21/ProxyFlow/internal/pool"
	"github.com/rfym21/ProxyFlow/internal/server"
)

// main 程序入口点，负责初始化配置、创建代理池和启动服务器。
func main() {
	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: 未找到 .env 文件: %v", err)
	}

	// 加载配置
	cfg := config.Load()
	log.Printf("启动 ProxyFlow，配置信息: 端口=%s (HTTP/SOCKS5 统一入口), 代理文件=%s, MaxIdleConns=%d, MaxIdleConnsPerHost=%d",
		cfg.ProxyPort, cfg.ProxyFile, cfg.MaxIdleConns, cfg.MaxIdleConnsPerHost)

	// 创建代理池（自动启动文件监听，支持热重载）
	proxyPool, err := pool.NewPoolWithFailure(cfg.ProxyFile, cfg.FailureProxyFile)
	if err != nil {
		log.Fatalf("创建代理池失败: %v", err)
	}

	// 创建代理服务器
	proxyServer := server.NewServer(proxyPool, cfg.RequestTimeout, cfg.MaxIdleConns, cfg.MaxIdleConnsPerHost, cfg.MaxRetryCount, cfg.AuthUsername, cfg.AuthPassword)

	// 创建 API 服务器（如果启用）
	var apiServer *api.Server
	if cfg.APIEnabled {
		apiServer = api.NewServer(proxyPool, cfg.APIKey, cfg.CheckURL, cfg.CheckRetry)
		go func() {
			log.Printf("API 服务已启用，密钥认证: %v", cfg.APIKey != "")
			if err := apiServer.Start(cfg.APIPort); err != nil {
				log.Printf("API 服务器关闭: %v", err)
			}
		}()
	}

	// 设置优雅关闭
	setupGracefulShutdown(proxyServer, proxyPool, apiServer)

	// 启动代理服务器（HTTP/SOCKS5 统一入口）
	log.Printf("ProxyFlow 已准备就绪，开始处理请求")
	if err := proxyServer.Start(cfg.ProxyPort); err != nil {
		log.Printf("服务器关闭: %v", err)
	}
}

// setupGracefulShutdown 设置优雅关闭处理。
//
// 监听系统中断信号（SIGINT、SIGTERM），在接收到信号时
// 执行优雅的服务关闭流程。
//
// 参数：
//   - proxyServer: 代理服务器实例
//   - proxyPool: 代理池实例
//   - apiServer: API 服务器实例（可为 nil）
func setupGracefulShutdown(proxyServer *server.Server, proxyPool *pool.Pool, apiServer *api.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("收到关闭信号，正在关闭 ProxyFlow...")

		// 关闭 API 服务器
		if apiServer != nil {
			if err := apiServer.Shutdown(); err != nil {
				log.Printf("关闭 API 服务器时出错: %v", err)
			}
		}

		// 关闭代理池（停止文件监听）
		proxyPool.Close()

		// 关闭代理服务器
		if err := proxyServer.Shutdown(); err != nil {
			log.Printf("关闭服务器时出错: %v", err)
		}
		os.Exit(0)
	}()
}
