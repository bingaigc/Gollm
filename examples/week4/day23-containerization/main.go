// Package main 演示 Go 应用容器化的核心概念
//
// 本示例用纯 Go 标准库模拟容器化相关的编程模式：
// 1. 多阶段构建模拟 - Dockerfile 多阶段构建概念与镜像大小对比
// 2. 环境变量配置 - 从环境变量读取配置，提供默认值和类型转换
// 3. 信号处理与优雅关闭 - 监听 SIGTERM/SIGINT，实现 graceful shutdown
// 4. 健康检查端点 - 模拟 /healthz 和 /readyz 端点
// 5. 容器资源限制感知 - cgroup 信息读取与 GOMAXPROCS 设置
// 6. 12-Factor App 原则 - 配置外部化、无状态进程、日志流等
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ========================================
// 一、多阶段构建模拟
// ========================================

// BuildStage 表示 Dockerfile 中的一个构建阶段
type BuildStage struct {
	Name      string   // 阶段名称
	BaseImage string   // 基础镜像
	Commands  []string // 执行的命令
	TotalSize int64    // 阶段总大小（模拟，单位 MB）
}

// demoMultiStageBuild 模拟多阶段构建过程
func demoMultiStageBuild() {
	stages := []BuildStage{
		{
			Name:      "builder",
			BaseImage: "golang:1.22-alpine",
			Commands: []string{
				"WORKDIR /app",
				"COPY go.mod go.sum ./",
				"RUN go mod download",
				"COPY . .",
				"RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /app/server",
			},
			TotalSize: 850,
		},
		{
			Name:      "runtime",
			BaseImage: "gcr.io/distroless/static:nonroot",
			Commands: []string{
				"COPY --from=builder /app/server /server",
				"USER nonroot:nonroot",
				"ENTRYPOINT [\"/server\"]",
			},
			TotalSize: 14,
		},
	}

	fmt.Println("📋 模拟 Dockerfile 多阶段构建:")
	fmt.Println(strings.Repeat("-", 55))
	for i, s := range stages {
		fmt.Printf("\n  🔨 阶段 %d: %s (镜像: %s)\n", i+1, s.Name, s.BaseImage)
		for _, cmd := range s.Commands {
			fmt.Printf("     %s\n", cmd)
		}
		fmt.Printf("     → 阶段大小: %d MB\n", s.TotalSize)
	}
	reduction := float64(stages[0].TotalSize-stages[1].TotalSize) / float64(stages[0].TotalSize) * 100
	fmt.Println()
	fmt.Println("  📊 镜像大小对比:")
	fmt.Printf("     单阶段: %d MB → 多阶段: %d MB (缩减 %.1f%%)\n",
		stages[0].TotalSize, stages[1].TotalSize, reduction)
	fmt.Println("  💡 多阶段构建去除编译工具链，大幅缩小镜像体积")
}

// ========================================
// 二、环境变量配置
// ========================================

// AppConfig 应用配置，所有字段可通过环境变量覆盖
type AppConfig struct {
	Port     int
	Host     string
	LogLevel string
	Debug    bool
}

// getEnv 读取环境变量，若不存在则返回默认值
func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

// getEnvInt 读取环境变量并转换为整数
func getEnvInt(key string, defaultVal int) int {
	s := getEnv(key, "")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// getEnvBool 读取环境变量并转换为布尔值
func getEnvBool(key string, defaultVal bool) bool {
	s := getEnv(key, "")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// LoadConfig 从环境变量加载配置
func LoadConfig() *AppConfig {
	return &AppConfig{
		Port:     getEnvInt("APP_PORT", 8080),
		Host:     getEnv("APP_HOST", "0.0.0.0"),
		LogLevel: getEnv("APP_LOG_LEVEL", "info"),
		Debug:    getEnvBool("APP_DEBUG", false),
	}
}

// demoEnvConfig 演示环境变量配置
func demoEnvConfig() {
	os.Setenv("APP_PORT", "3000")
	os.Setenv("APP_LOG_LEVEL", "debug")
	os.Setenv("APP_DEBUG", "true")
	defer func() {
		os.Unsetenv("APP_PORT")
		os.Unsetenv("APP_LOG_LEVEL")
		os.Unsetenv("APP_DEBUG")
	}()

	cfg := LoadConfig()
	fmt.Println("📋 从环境变量加载配置:")
	fmt.Println(strings.Repeat("-", 55))
	fmt.Printf("  APP_PORT      = %d    (环境变量)\n", cfg.Port)
	fmt.Printf("  APP_HOST      = %s  (默认值)\n", cfg.Host)
	fmt.Printf("  APP_LOG_LEVEL = %s  (环境变量)\n", cfg.LogLevel)
	fmt.Printf("  APP_DEBUG     = %v   (环境变量)\n", cfg.Debug)
	fmt.Println()
	fmt.Println("  💡 配置通过环境变量注入，每项提供合理默认值")
	fmt.Println("     敏感信息（密码、密钥）通过 K8s Secret 挂载")
}

// ========================================
// 三、信号处理与优雅关闭
// ========================================

// GracefulServer 支持优雅关闭的服务器模拟
type GracefulServer struct {
	wg         sync.WaitGroup
	connCount  int32
	shutdownCh chan struct{}
}

func NewGracefulServer() *GracefulServer {
	return &GracefulServer{shutdownCh: make(chan struct{})}
}

// SimulateConnection 模拟一个活跃连接
func (s *GracefulServer) SimulateConnection(id int, dur time.Duration) {
	s.wg.Add(1)
	atomic.AddInt32(&s.connCount, 1)
	fmt.Printf("     📥 连接 #%d 已建立 (活跃: %d)\n", id, atomic.LoadInt32(&s.connCount))
	go func() {
		defer s.wg.Done()
		defer atomic.AddInt32(&s.connCount, -1)
		select {
		case <-time.After(dur):
			fmt.Printf("     📤 连接 #%d 正常完成\n", id)
		case <-s.shutdownCh:
			time.Sleep(30 * time.Millisecond)
			fmt.Printf("     ⏳ 连接 #%d 排空完成\n", id)
		}
	}()
}

// Shutdown 优雅关闭
func (s *GracefulServer) Shutdown(timeout time.Duration) error {
	fmt.Printf("     🛑 开始优雅关闭 (超时: %v)\n", timeout)
	close(s.shutdownCh)
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		fmt.Println("     ✅ 所有连接已安全关闭")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("关闭超时")
	}
}

func demoGracefulShutdown() {
	fmt.Println("📋 信号处理与优雅关闭模拟:")
	fmt.Println(strings.Repeat("-", 55))
	fmt.Println()
	fmt.Println("  🔔 信号监听代码模式:")
	fmt.Println("     sigCh := make(chan os.Signal, 1)")
	fmt.Println("     signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)")
	fmt.Println("     <-sigCh  // 阻塞等待信号")
	fmt.Println()
	fmt.Println("  🔄 模拟连接排空流程:")
	srv := NewGracefulServer()
	srv.SimulateConnection(1, 200*time.Millisecond)
	srv.SimulateConnection(2, 300*time.Millisecond)
	srv.SimulateConnection(3, 100*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	fmt.Println()
	fmt.Println("     📨 收到 SIGTERM!")
	srv.Shutdown(2 * time.Second)
	fmt.Println()
	fmt.Println("  💡 先停止接受新请求，再排空已有连接")
	fmt.Println("     K8s 默认宽限期 30s，需在此期间完成关闭")
}

// ========================================
// 四、健康检查端点
// ========================================

// HealthChecker 健康检查管理器
type HealthChecker struct {
	startTime    time.Time
	startDelay   time.Duration
	isReady      int32
	mu           sync.RWMutex
	dependencies map[string]bool
}

// HealthResponse 健康检查响应体
type HealthResponse struct {
	Status string            `json:"status"`
	Uptime string            `json:"uptime"`
	Checks map[string]string `json:"checks"`
}

func NewHealthChecker(delay time.Duration) *HealthChecker {
	return &HealthChecker{
		startTime:    time.Now(),
		startDelay:   delay,
		dependencies: map[string]bool{"database": true, "cache": true},
	}
}

// LivenessHandler 存活探针 /healthz — 进程是否存活
func (h *HealthChecker) LivenessHandler(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Status: "healthy",
		Uptime: time.Since(h.startTime).Truncate(time.Millisecond).String(),
		Checks: map[string]string{"process": "alive"},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("healthz encode error: %v", err)
	}
}

// ReadinessHandler 就绪探针 /readyz — 是否可接收流量
func (h *HealthChecker) ReadinessHandler(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Uptime: time.Since(h.startTime).Truncate(time.Millisecond).String(),
		Checks: make(map[string]string),
	}
	// 检查启动延迟
	if time.Since(h.startTime) < h.startDelay {
		resp.Status = "not_ready"
		resp.Checks["startup"] = "initializing"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("readyz encode error: %v", err)
		}
		return
	}
	// 检查依赖
	h.mu.RLock()
	allOK := true
	for name, ok := range h.dependencies {
		if ok {
			resp.Checks[name] = "connected"
		} else {
			resp.Checks[name] = "disconnected"
			allOK = false
		}
	}
	h.mu.RUnlock()

	if allOK && atomic.LoadInt32(&h.isReady) == 1 {
		resp.Status = "ready"
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("readyz encode error: %v", err)
		}
	} else {
		resp.Status = "not_ready"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("readyz encode error: %v", err)
		}
	}
}

func testEndpoint(mux *http.ServeMux, path, label string) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)
	icon := "✅"
	if w.Code != http.StatusOK {
		icon = "❌"
	}
	fmt.Printf("     %s %s (%s): HTTP %d — %s\n", icon, label, path, w.Code, resp.Status)
}

func demoHealthCheck() {
	fmt.Println("📋 健康检查端点模拟:")
	fmt.Println(strings.Repeat("-", 55))

	hc := NewHealthChecker(500 * time.Millisecond)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hc.LivenessHandler)
	mux.HandleFunc("/readyz", hc.ReadinessHandler)

	fmt.Println()
	fmt.Println("  场景 1: 启动中（未就绪）")
	testEndpoint(mux, "/healthz", "存活探针")
	testEndpoint(mux, "/readyz", "就绪探针")

	// 模拟就绪
	hc.startTime = time.Now().Add(-1 * time.Second)
	atomic.StoreInt32(&hc.isReady, 1)

	fmt.Println()
	fmt.Println("  场景 2: 服务已就绪")
	testEndpoint(mux, "/healthz", "存活探针")
	testEndpoint(mux, "/readyz", "就绪探针")

	// 模拟依赖故障
	hc.mu.Lock()
	hc.dependencies["database"] = false
	hc.mu.Unlock()

	fmt.Println()
	fmt.Println("  场景 3: 数据库断开")
	testEndpoint(mux, "/readyz", "就绪探针")

	fmt.Println()
	fmt.Println("  💡 /healthz 失败 → 重启容器; /readyz 失败 → 摘除流量")
}

// ========================================
// 五、容器资源限制感知
// ========================================

func readCgroupCPU() (quota, period int64) {
	// cgroup v2: /sys/fs/cgroup/cpu.max 格式 "quota period"
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(data)))
		if len(parts) == 2 && parts[0] != "max" {
			q, e1 := strconv.ParseInt(parts[0], 10, 64)
			p, e2 := strconv.ParseInt(parts[1], 10, 64)
			if e1 == nil && e2 == nil {
				return q, p
			}
		}
	}
	return -1, 100000 // 未检测到限制
}

func readCgroupMemory() int64 {
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(data))
		if s != "max" {
			if v, e := strconv.ParseInt(s, 10, 64); e == nil {
				return v
			}
		}
	}
	return 512 * 1024 * 1024 // 模拟 512MB
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	default:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	}
}

func demoContainerResources() {
	fmt.Println("📋 容器资源限制感知:")
	fmt.Println(strings.Repeat("-", 55))

	fmt.Println()
	fmt.Println("  🖥️  CPU 资源:")
	fmt.Printf("     主机 CPU 核心: %d\n", runtime.NumCPU())
	fmt.Printf("     GOMAXPROCS:    %d\n", runtime.GOMAXPROCS(0))
	quota, period := readCgroupCPU()
	if quota > 0 {
		fmt.Printf("     cgroup 配额: %d/%d (≈%.1f 核)\n", quota, period, float64(quota)/float64(period))
	} else {
		fmt.Println("     cgroup CPU: 无限制或非容器环境")
	}

	fmt.Println()
	fmt.Println("  🧠 内存资源:")
	memLimit := readCgroupMemory()
	fmt.Printf("     cgroup 内存限制: %s\n", formatBytes(memLimit))
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("     Go 堆分配: %s, GC 次数: %d\n", formatBytes(int64(m.Alloc)), m.NumGC)

	fmt.Println()
	fmt.Println("  💡 容器中 NumCPU() 返回主机值，可能导致过度调度")
	fmt.Println("     建议根据 cgroup 设置 GOMAXPROCS 和 GOMEMLIMIT")
}

// ========================================
// 六、12-Factor App 原则
// ========================================

type Factor struct {
	Num        int
	Name       string
	Desc       string
	GoPractice string
}

func demoTwelveFactorApp() {
	factors := []Factor{
		{1, "Codebase 基准代码", "一份代码，多份部署", "Git 管理，CI/CD 多环境部署"},
		{2, "Dependencies 依赖", "显式声明依赖", "go.mod 声明，go.sum 锁定"},
		{3, "Config 配置", "环境中存储配置", "os.Getenv() 读取，不硬编码"},
		{4, "Backing Services 后端服务", "后端服务当附加资源", "DATABASE_URL 环境变量注入"},
		{5, "Build/Release/Run 构建发布运行", "严格分离阶段", "Docker 多阶段构建"},
		{6, "Processes 进程", "无状态进程", "会话存 Redis，不依赖本地文件"},
		{7, "Port Binding 端口绑定", "通过端口提供服务", "http.ListenAndServe(\":8080\", h)"},
		{8, "Concurrency 并发", "进程模型扩展", "水平扩展 Pod 副本数"},
		{9, "Disposability 易处理", "快速启动优雅终止", "graceful shutdown + SIGTERM"},
		{10, "Dev/Prod Parity 环境等价", "开发线上保持一致", "Docker Compose 本地重现"},
		{11, "Logs 日志", "日志当事件流", "JSON 输出到 stdout，平台采集"},
		{12, "Admin Processes 管理进程", "一次性进程运行", "DB 迁移作为 Job 运行"},
	}

	fmt.Println("📋 12-Factor App 原则与 Go 实践:")
	fmt.Println(strings.Repeat("-", 55))
	for _, f := range factors {
		fmt.Printf("  %2d. %s\n", f.Num, f.Name)
		fmt.Printf("      原则: %s\n", f.Desc)
		fmt.Printf("      实践: %s\n", f.GoPractice)
	}

	// 演示结构化日志
	fmt.Println()
	fmt.Println("  📝 结构化日志示范 (原则 11):")
	type LogEntry struct {
		Time    string `json:"time"`
		Level   string `json:"level"`
		Msg     string `json:"msg"`
		Service string `json:"service"`
	}
	entries := []LogEntry{
		{time.Now().Format(time.RFC3339), "INFO", "服务启动", "gollm-demo"},
		{time.Now().Format(time.RFC3339), "INFO", "处理请求", "gollm-demo"},
	}
	for _, e := range entries {
		data, _ := json.Marshal(e)
		fmt.Printf("     %s\n", data)
	}
	fmt.Println("     → JSON 输出到 stdout，便于日志平台采集")
}

// ========================================
// 主函数
// ========================================

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║        Day 23: 容器化实践 - Docker 与 Go            ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()

	fmt.Println("🐳 === 第一部分：多阶段构建模拟 ===")
	fmt.Println()
	demoMultiStageBuild()
	fmt.Println()

	fmt.Println("🔧 === 第二部分：环境变量配置 ===")
	fmt.Println()
	demoEnvConfig()
	fmt.Println()

	fmt.Println("🛑 === 第三部分：信号处理与优雅关闭 ===")
	fmt.Println()
	demoGracefulShutdown()
	fmt.Println()

	fmt.Println("💚 === 第四部分：健康检查端点 ===")
	fmt.Println()
	demoHealthCheck()
	fmt.Println()

	fmt.Println("📦 === 第五部分：容器资源限制感知 ===")
	fmt.Println()
	demoContainerResources()
	fmt.Println()

	fmt.Println("📖 === 第六部分：12-Factor App 原则 ===")
	fmt.Println()
	demoTwelveFactorApp()
	fmt.Println()

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║                   学习总结                          ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  📌 容器化 Go 应用的关键要点:")
	fmt.Println("     1. 多阶段构建，最终镜像尽量小")
	fmt.Println("     2. 配置通过环境变量注入，遵循 12-Factor")
	fmt.Println("     3. 处理 SIGTERM，实现优雅关闭")
	fmt.Println("     4. 提供 /healthz 和 /readyz 健康检查端点")
	fmt.Println("     5. 感知 cgroup 资源限制，正确设置 GOMAXPROCS")
	fmt.Println("     6. 日志输出到 stdout，JSON 格式便于采集")
	fmt.Println()
}
