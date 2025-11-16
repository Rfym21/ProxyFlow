package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
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
	log.Printf("启动 ProxyFlow，配置信息: 端口=%s, 代理文件=%s, MaxIdleConns=%d, MaxIdleConnsPerHost=%d",
		cfg.ProxyPort, cfg.ProxyFile, cfg.MaxIdleConns, cfg.MaxIdleConnsPerHost)

	// 创建代理池
	proxyPool, err := pool.NewPool(cfg.ProxyFile)
	if err != nil {
		log.Fatalf("创建代理池失败: %v", err)
	}

	// 创建代理服务器
	proxyServer := server.NewServer(proxyPool, cfg.RequestTimeout, cfg.MaxIdleConns, cfg.MaxIdleConnsPerHost, cfg.AuthUsername, cfg.AuthPassword)

	// 设置优雅关闭
	setupGracefulShutdown(proxyServer)

	// 启动服务器
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
//   - server: 代理服务器实例
func setupGracefulShutdown(server *server.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("收到关闭信号，正在关闭 ProxyFlow...")
		if err := server.Shutdown(); err != nil {
			log.Printf("关闭服务器时出错: %v", err)
		}
		os.Exit(0)
	}()
}
