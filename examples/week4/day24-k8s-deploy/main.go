// Package main 演示 Kubernetes 部署相关的 Go 编程模式
//
// 本示例涵盖以下 K8s 部署核心概念：
//   - HTTP 健康检查服务器（存活探针 + 就绪探针）
//   - 优雅关闭（监听 SIGTERM/SIGINT，排空连接，关闭服务器）
//   - 通过环境变量读取 ConfigMap 配置
//   - 完整生命周期演示：启动中 → 就绪 → 服务中 → 关闭中
//
// 对应的 Kubernetes 部署 YAML 示例：
//
//	apiVersion: apps/v1
//	kind: Deployment
//	metadata:
//	  name: gollm-app
//	  labels:
//	    app: gollm
//	spec:
//	  replicas: 3
//	  selector:
//	    matchLabels:
//	      app: gollm
//	  template:
//	    metadata:
//	      labels:
//	        app: gollm
//	    spec:
//	      containers:
//	      - name: gollm
//	        image: gollm-app:latest
//	        ports:
//	        - containerPort: 8080
//	        env:
//	        - name: APP_NAME
//	          valueFrom:
//	            configMapKeyRef:
//	              name: gollm-config
//	              key: app_name
//	        - name: LOG_LEVEL
//	          valueFrom:
//	            configMapKeyRef:
//	              name: gollm-config
//	              key: log_level
//	        livenessProbe:            # 存活探针：检测容器是否还在运行
//	          httpGet:
//	            path: /healthz
//	            port: 8080
//	          initialDelaySeconds: 5
//	          periodSeconds: 10
//	        readinessProbe:           # 就绪探针：检测容器是否准备好接收流量
//	          httpGet:
//	            path: /readyz
//	            port: 8080
//	          initialDelaySeconds: 3
//	          periodSeconds: 5
//	        resources:
//	          requests:
//	            memory: "64Mi"
//	            cpu: "100m"
//	          limits:
//	            memory: "128Mi"
//	            cpu: "250m"
//	---
//	apiVersion: v1
//	kind: ConfigMap
//	metadata:
//	  name: gollm-config
//	data:
//	  app_name: "gollm-k8s-demo"
//	  log_level: "info"
//	---
//	apiVersion: v1
//	kind: Service
//	metadata:
//	  name: gollm-service
//	spec:
//	  selector:
//	    app: gollm
//	  ports:
//	  - port: 80
//	    targetPort: 8080
//	  type: ClusterIP
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// AppConfig 保存从环境变量（对应 K8s ConfigMap）读取的应用配置
type AppConfig struct {
	AppName  string // 应用名称，来自 ConfigMap
	LogLevel string // 日志级别，来自 ConfigMap
	Port     string // 服务端口
}

// HealthChecker 管理应用的健康检查状态
// 在 K8s 中，存活探针和就绪探针有不同的用途：
//   - 存活探针（Liveness）：失败时 K8s 会重启容器
//   - 就绪探针（Readiness）：失败时 K8s 会从 Service 端点移除该 Pod
type HealthChecker struct {
	alive int32 // 原子操作：1 表示存活，0 表示不存活
	ready int32 // 原子操作：1 表示就绪，0 表示未就绪
}

// NewHealthChecker 创建一个新的健康检查器，初始状态为存活但未就绪
func NewHealthChecker() *HealthChecker {
	hc := &HealthChecker{}
	atomic.StoreInt32(&hc.alive, 1) // 启动后立即标记为存活
	atomic.StoreInt32(&hc.ready, 0) // 启动时尚未就绪（需要完成初始化）
	return hc
}

func (hc *HealthChecker) IsAlive() bool {
	return atomic.LoadInt32(&hc.alive) == 1
}

func (hc *HealthChecker) IsReady() bool {
	return atomic.LoadInt32(&hc.ready) == 1
}

func (hc *HealthChecker) SetReady(ready bool) {
	if ready {
		atomic.StoreInt32(&hc.ready, 1)
	} else {
		atomic.StoreInt32(&hc.ready, 0)
	}
}

func (hc *HealthChecker) SetAlive(alive bool) {
	if alive {
		atomic.StoreInt32(&hc.alive, 1)
	} else {
		atomic.StoreInt32(&hc.alive, 0)
	}
}

// ConnectionTracker 追踪活跃连接数，用于优雅关闭时等待连接排空
type ConnectionTracker struct {
	mu     sync.Mutex
	count  int
	waitCh chan struct{} // 当所有连接关闭时通知
}

func NewConnectionTracker() *ConnectionTracker {
	return &ConnectionTracker{
		waitCh: make(chan struct{}),
	}
}

// Add 记录新连接
func (ct *ConnectionTracker) Add() {
	ct.mu.Lock()
	ct.count++
	ct.mu.Unlock()
}

// Done 记录连接完成
func (ct *ConnectionTracker) Done() {
	ct.mu.Lock()
	ct.count--
	if ct.count == 0 {
		select {
		case ct.waitCh <- struct{}{}:
		default:
		}
	}
	ct.mu.Unlock()
}

// Count 返回当前活跃连接数
func (ct *ConnectionTracker) Count() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.count
}

// WaitForDrain 等待所有连接排空或超时
func (ct *ConnectionTracker) WaitForDrain(timeout time.Duration) bool {
	ct.mu.Lock()
	if ct.count == 0 {
		ct.mu.Unlock()
		return true
	}
	ct.mu.Unlock()

	select {
	case <-ct.waitCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

// loadConfig 从环境变量加载配置
// 在 K8s 中，ConfigMap 的值会被注入到容器的环境变量中
func loadConfig() AppConfig {
	cfg := AppConfig{
		AppName:  getEnvOrDefault("APP_NAME", "gollm-k8s-demo"),
		LogLevel: getEnvOrDefault("LOG_LEVEL", "info"),
		Port:     getEnvOrDefault("PORT", "8080"),
	}
	return cfg
}

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// simulateStartup 模拟应用启动过程（如加载缓存、建立数据库连接等）
func simulateStartup(hc *HealthChecker, duration time.Duration) {
	log.Println("[启动] 正在初始化应用...")
	log.Println("[启动] 加载缓存数据...")
	time.Sleep(duration / 3)
	log.Println("[启动] 建立数据库连接...")
	time.Sleep(duration / 3)
	log.Println("[启动] 预热服务组件...")
	time.Sleep(duration / 3)

	// 初始化完成，标记为就绪
	hc.SetReady(true)
	log.Println("[启动] ✅ 应用初始化完成，标记为就绪状态")
}

func main() {
	fmt.Println("=== Kubernetes 部署模式演示 ===")
	fmt.Println()

	// ===== 第一步：加载配置（模拟从 ConfigMap 读取） =====
	cfg := loadConfig()
	log.Printf("[配置] 应用名称: %s", cfg.AppName)
	log.Printf("[配置] 日志级别: %s", cfg.LogLevel)
	log.Printf("[配置] 服务端口: %s", cfg.Port)
	fmt.Println()

	// ===== 第二步：初始化健康检查和连接追踪 =====
	health := NewHealthChecker()
	tracker := NewConnectionTracker()

	// ===== 第三步：设置 HTTP 路由 =====
	mux := http.NewServeMux()

	// 存活探针端点：只要进程正常运行就返回 200
	// K8s 用此判断是否需要重启容器
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if health.IsAlive() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"alive","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"dead","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
		}
	})

	// 就绪探针端点：只有应用准备好处理请求时才返回 200
	// K8s 用此判断是否将流量路由到此 Pod
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if health.IsReady() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ready","connections":%d,"timestamp":"%s"}`,
				tracker.Count(), time.Now().Format(time.RFC3339))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"not_ready","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
		}
	})

	// 业务端点：模拟实际请求处理
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tracker.Add()
		defer tracker.Done()

		// 模拟请求处理耗时
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintf(w, `{"app":"%s","message":"Hello from K8s!","connections":%d}`,
			cfg.AppName, tracker.Count())
	})

	// ===== 第四步：创建 HTTP 服务器 =====
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,  // 读取超时
		WriteTimeout: 10 * time.Second, // 写入超时
		IdleTimeout:  60 * time.Second, // 空闲连接超时
	}

	// ===== 第五步：启动服务器（后台协程） =====
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("[服务器] 正在监听端口 :%s ...", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// ===== 第六步：模拟启动初始化（异步） =====
	go simulateStartup(health, 1*time.Second)

	// ===== 第七步：监听终止信号实现优雅关闭 =====
	// K8s 在停止 Pod 时会发送 SIGTERM 信号
	// 应用需要在 terminationGracePeriodSeconds（默认30秒）内完成关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	// ===== 模拟完整生命周期 =====
	go func() {
		// 等待服务就绪
		for !health.IsReady() {
			time.Sleep(100 * time.Millisecond)
		}

		fmt.Println()
		log.Println("[生命周期] === 模拟请求处理阶段 ===")

		// 模拟几个请求
		for i := 1; i <= 3; i++ {
			tracker.Add()
			log.Printf("[生命周期] 处理请求 #%d（活跃连接: %d）", i, tracker.Count())
			time.Sleep(200 * time.Millisecond)
			tracker.Done()
		}

		fmt.Println()
		log.Println("[生命周期] === 模拟接收 SIGTERM 信号 ===")
		// 模拟 K8s 发送终止信号
		quit <- syscall.SIGTERM
	}()

	// ===== 等待终止信号 =====
	select {
	case err := <-serverErr:
		log.Fatalf("[服务器] 启动失败: %v", err)
	case sig := <-quit:
		log.Printf("[关闭] 收到信号: %v，开始优雅关闭...", sig)
	}

	// ===== 第八步：优雅关闭流程 =====
	fmt.Println()
	log.Println("[关闭] 步骤 1: 标记为未就绪（停止接收新流量）")
	health.SetReady(false)

	log.Println("[关闭] 步骤 2: 等待现有连接排空...")
	if tracker.WaitForDrain(10 * time.Second) {
		log.Println("[关闭] ✅ 所有连接已排空")
	} else {
		log.Printf("[关闭] ⚠️  超时，仍有 %d 个连接未完成", tracker.Count())
	}

	log.Println("[关闭] 步骤 3: 关闭 HTTP 服务器...")
	// 使用带超时的 context 确保关闭不会无限等待
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("[关闭] ⚠️  服务器关闭出错: %v", err)
	} else {
		log.Println("[关闭] ✅ HTTP 服务器已关闭")
	}

	log.Println("[关闭] 步骤 4: 清理资源（关闭数据库连接、刷新缓冲区等）...")
	time.Sleep(200 * time.Millisecond) // 模拟资源清理

	health.SetAlive(false)
	fmt.Println()
	log.Println("[关闭] ✅ 应用已完全停止")

	// ===== 打印生命周期总结 =====
	fmt.Println()
	fmt.Println("=== K8s 部署生命周期总结 ===")
	fmt.Println()
	fmt.Println("  Pod 生命周期阶段:")
	fmt.Println("  ┌─────────────┐")
	fmt.Println("  │   Pending   │  ← Pod 被调度到节点")
	fmt.Println("  └──────┬──────┘")
	fmt.Println("         │")
	fmt.Println("  ┌──────▼──────┐")
	fmt.Println("  │  Starting   │  ← 容器启动，存活探针开始检查")
	fmt.Println("  └──────┬──────┘")
	fmt.Println("         │")
	fmt.Println("  ┌──────▼──────┐")
	fmt.Println("  │   Ready     │  ← 就绪探针通过，开始接收流量")
	fmt.Println("  └──────┬──────┘")
	fmt.Println("         │")
	fmt.Println("  ┌──────▼──────┐")
	fmt.Println("  │  Stopping   │  ← 收到 SIGTERM，停止接收新请求")
	fmt.Println("  └──────┬──────┘")
	fmt.Println("         │")
	fmt.Println("  ┌──────▼──────┐")
	fmt.Println("  │ Terminated  │  ← 连接排空，资源清理完成")
	fmt.Println("  └─────────────┘")
	fmt.Println()
	fmt.Println("  关键配置项:")
	fmt.Println("  • livenessProbe  → /healthz  （失败则重启容器）")
	fmt.Println("  • readinessProbe → /readyz   （失败则移除端点）")
	fmt.Println("  • SIGTERM 处理   → 优雅关闭   （排空连接后退出）")
	fmt.Println("  • ConfigMap      → 环境变量   （解耦配置与代码）")
}
