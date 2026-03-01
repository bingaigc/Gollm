// Package main 演示可观测性（Observability）的三大支柱：
//   - 日志（Logging）：使用 Go 1.21+ 标准库 log/slog 实现结构化日志
//   - 指标（Metrics）：实现计数器、直方图、仪表盘，模拟 Prometheus 格式输出
//   - 链路追踪（Tracing）：通过 context 传播 trace ID，模拟分布式追踪
//
// 这三者共同构成了现代微服务系统的可观测性基础，
// 对应 OpenTelemetry 的三大信号：Logs、Metrics、Traces
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// 第一部分：结构化日志（Structured Logging）
// 使用 Go 1.21 引入的 log/slog 包
//
// slog 的核心概念：
//   - Handler：决定日志的输出格式（JSON / Text）
//   - Level：日志级别（Debug < Info < Warn < Error）
//   - Attr：键值对属性，实现结构化
//   - Group：属性分组
//   - Logger：日志记录器，可携带预设属性
//
// 对应 OpenTelemetry Logs 信号
// ============================================================

// setupJSONLogger 创建 JSON 格式的日志记录器
// JSON 格式适合机器解析，常用于日志采集系统（如 ELK、Loki）
func setupJSONLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug, // 设置最低输出级别
		AddSource: false,           // 是否在日志中添加源码位置
	})
	return slog.New(handler)
}

// setupTextLogger 创建文本格式的日志记录器
// 文本格式适合人类阅读，常用于本地开发
func setupTextLogger() *slog.Logger {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(handler)
}

// demonstrateLogging 演示 slog 的各种用法
func demonstrateLogging() {
	fmt.Println("=== 第一部分：结构化日志 (slog) ===")
	fmt.Println()

	// --- JSON 格式 ---
	fmt.Println("--- JSON 格式日志 ---")
	jsonLogger := setupJSONLogger()

	// 基本日志级别
	jsonLogger.Debug("调试信息：数据库连接池状态", "pool_size", 10, "active", 3)
	jsonLogger.Info("服务启动成功", "port", 8080, "env", "production")
	jsonLogger.Warn("内存使用率较高", "usage_percent", 85.5, "threshold", 80.0)
	jsonLogger.Error("请求处理失败", "method", "POST", "path", "/api/users", "error", "connection timeout")
	fmt.Println()

	// --- 文本格式 ---
	fmt.Println("--- 文本格式日志 ---")
	textLogger := setupTextLogger()
	textLogger.Info("这是文本格式的日志", "key1", "value1", "key2", 42)
	fmt.Println()

	// --- 属性分组 ---
	fmt.Println("--- 属性分组 ---")
	jsonLogger.Info("HTTP 请求",
		slog.Group("request",
			slog.String("method", "GET"),
			slog.String("path", "/api/users"),
			slog.String("remote_addr", "192.168.1.100"),
		),
		slog.Group("response",
			slog.Int("status", 200),
			slog.Duration("latency", 45*time.Millisecond),
		),
	)
	fmt.Println()

	// --- 携带预设属性的 Logger ---
	// With() 创建一个新 Logger，后续所有日志都会自动包含这些属性
	fmt.Println("--- 请求作用域日志 ---")
	requestLogger := jsonLogger.With(
		"trace_id", "abc-123-def-456",
		"service", "user-service",
		"version", "v2.1.0",
	)
	requestLogger.Info("开始处理请求")
	requestLogger.Info("查询数据库", "query", "SELECT * FROM users", "duration_ms", 12)
	requestLogger.Info("请求处理完成", "status", 200)
	fmt.Println()
}

// ============================================================
// 第二部分：指标收集（Metrics）
// 模拟 Prometheus 指标类型：
//   - Counter（计数器）：只增不减，如请求总数
//   - Histogram（直方图）：观测值的分布，如请求延迟
//   - Gauge（仪表盘）：可增可减，如当前连接数
//
// 对应 OpenTelemetry Metrics 信号
// ============================================================

// Counter 计数器：单调递增的指标
// 适用场景：请求总数、错误总数、处理的字节数
type Counter struct {
	name   string // 指标名称
	help   string // 指标描述
	value  int64  // 当前值（原子操作）
	labels sync.Map // 带标签的计数器值 key: labelString, value: *int64
}

// NewCounter 创建新的计数器
func NewCounter(name, help string) *Counter {
	return &Counter{name: name, help: help}
}

// Inc 计数器加一
func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add 计数器加 n
func (c *Counter) Add(n int64) {
	atomic.AddInt64(&c.value, n)
}

// IncWithLabels 带标签的计数器加一
func (c *Counter) IncWithLabels(labels map[string]string) {
	key := formatLabels(labels)
	val, _ := c.labels.LoadOrStore(key, new(int64))
	atomic.AddInt64(val.(*int64), 1)
}

// Value 获取当前值
func (c *Counter) Value() int64 {
	return atomic.LoadInt64(&c.value)
}

// Histogram 直方图：观测值的统计分布
// 适用场景：请求延迟、响应大小
// Prometheus 直方图使用预定义的桶（bucket）统计落在各区间的数量
type Histogram struct {
	name    string    // 指标名称
	help    string    // 指标描述
	buckets []float64 // 桶的上界值
	mu      sync.Mutex
	counts  []int64   // 每个桶的计数
	sum     float64   // 观测值总和
	count   int64     // 观测次数
}

// NewHistogram 创建新的直方图
func NewHistogram(name, help string, buckets []float64) *Histogram {
	sort.Float64s(buckets)
	return &Histogram{
		name:    name,
		help:    help,
		buckets: buckets,
		counts:  make([]int64, len(buckets)),
	}
}

// Observe 记录一个观测值
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sum += value
	h.count++

	// 将值放入对应的桶
	for i, bound := range h.buckets {
		if value <= bound {
			h.counts[i]++
		}
	}
}

// Gauge 仪表盘：可增可减的指标
// 适用场景：当前连接数、队列长度、温度
type Gauge struct {
	name  string  // 指标名称
	help  string  // 指标描述
	value int64   // 使用 int64 存储，实际值需除以 1000（模拟浮点）
}

// NewGauge 创建新的仪表盘
func NewGauge(name, help string) *Gauge {
	return &Gauge{name: name, help: help}
}

// Set 设置值
func (g *Gauge) Set(v float64) {
	atomic.StoreInt64(&g.value, int64(v*1000))
}

// Inc 加一
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1000)
}

// Dec 减一
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1000)
}

// Value 获取当前值
func (g *Gauge) Value() float64 {
	return float64(atomic.LoadInt64(&g.value)) / 1000.0
}

// MetricsRegistry 指标注册中心，管理所有指标
type MetricsRegistry struct {
	counters   []*Counter
	histograms []*Histogram
	gauges     []*Gauge
	mu         sync.Mutex
}

// NewMetricsRegistry 创建指标注册中心
func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{}
}

// RegisterCounter 注册计数器
func (r *MetricsRegistry) RegisterCounter(c *Counter) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = append(r.counters, c)
	return c
}

// RegisterHistogram 注册直方图
func (r *MetricsRegistry) RegisterHistogram(h *Histogram) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.histograms = append(r.histograms, h)
	return h
}

// RegisterGauge 注册仪表盘
func (r *MetricsRegistry) RegisterGauge(g *Gauge) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, g)
	return g
}

// ExposeMetrics 以 Prometheus 文本格式输出所有指标
func (r *MetricsRegistry) ExposeMetrics() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var sb strings.Builder

	// 输出计数器
	for _, c := range r.counters {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", c.name, c.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n", c.name))
		sb.WriteString(fmt.Sprintf("%s %d\n", c.name, c.Value()))

		// 输出带标签的值
		c.labels.Range(func(key, value interface{}) bool {
			sb.WriteString(fmt.Sprintf("%s{%s} %d\n", c.name, key, atomic.LoadInt64(value.(*int64))))
			return true
		})
		sb.WriteString("\n")
	}

	// 输出直方图
	for _, h := range r.histograms {
		h.mu.Lock()
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", h.name, h.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s histogram\n", h.name))

		// 累积计数（Prometheus 直方图是累积式的）
		var cumulative int64
		for i, bound := range h.buckets {
			cumulative += h.counts[i]
			if bound == math.Inf(1) {
				sb.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", h.name, cumulative))
			} else {
				sb.WriteString(fmt.Sprintf("%s_bucket{le=\"%.3f\"} %d\n", h.name, bound, cumulative))
			}
		}
		sb.WriteString(fmt.Sprintf("%s_sum %.3f\n", h.name, h.sum))
		sb.WriteString(fmt.Sprintf("%s_count %d\n", h.name, h.count))
		h.mu.Unlock()
		sb.WriteString("\n")
	}

	// 输出仪表盘
	for _, g := range r.gauges {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", g.name, g.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", g.name))
		sb.WriteString(fmt.Sprintf("%s %.1f\n", g.name, g.Value()))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatLabels 将标签 map 格式化为 Prometheus 标签字符串
func formatLabels(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=\"%s\"", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// demonstrateMetrics 演示指标收集和输出
func demonstrateMetrics(registry *MetricsRegistry) {
	fmt.Println("=== 第二部分：指标收集 (Metrics) ===")
	fmt.Println()

	// 创建并注册指标
	requestCounter := registry.RegisterCounter(
		NewCounter("http_requests_total", "HTTP 请求总数"),
	)
	requestDuration := registry.RegisterHistogram(
		NewHistogram("http_request_duration_seconds", "HTTP 请求延迟分布",
			[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, math.Inf(1)}),
	)
	activeConnections := registry.RegisterGauge(
		NewGauge("http_active_connections", "当前活跃 HTTP 连接数"),
	)

	// 模拟请求处理
	fmt.Println("--- 模拟请求处理 ---")
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 100; i++ {
		// 模拟连接
		activeConnections.Inc()

		// 模拟请求处理延迟（指数分布）
		duration := rng.ExpFloat64() * 0.05 // 平均 50ms
		requestDuration.Observe(duration)

		// 记录请求计数（带标签）
		requestCounter.Inc()
		methods := []string{"GET", "POST", "PUT"}
		requestCounter.IncWithLabels(map[string]string{
			"method": methods[rng.Intn(len(methods))],
			"status": fmt.Sprintf("%d", []int{200, 200, 200, 404, 500}[rng.Intn(5)]),
		})

		// 模拟连接完成
		activeConnections.Dec()
	}

	// 设置最终活跃连接数
	activeConnections.Set(5)

	fmt.Printf("  处理了 %d 个请求\n", requestCounter.Value())
	fmt.Printf("  当前活跃连接: %.0f\n", activeConnections.Value())
	fmt.Println()

	// 输出 Prometheus 格式的指标
	fmt.Println("--- Prometheus 文本格式输出（/metrics 端点） ---")
	fmt.Println(registry.ExposeMetrics())
}

// ============================================================
// 第三部分：分布式追踪（Distributed Tracing）
// 通过 context.Context 传播 trace ID
//
// 核心概念：
//   - Trace：一个完整请求的链路，由多个 Span 组成
//   - Span：链路中的一个工作单元（如一次 HTTP 请求、一次 DB 查询）
//   - TraceID：全局唯一，标识整条链路
//   - SpanID：每个 Span 的唯一标识
//   - ParentSpanID：父 Span 的 ID，构成树状结构
//
// 对应 OpenTelemetry Traces 信号
// ============================================================

// 使用自定义类型作为 context key，避免键冲突
type contextKey string

const (
	traceIDKey  contextKey = "trace_id"  // Trace ID 的 context key
	spanIDKey   contextKey = "span_id"   // Span ID 的 context key
)

// Span 表示一个追踪跨度（工作单元）
type Span struct {
	TraceID      string        // 全局追踪 ID
	SpanID       string        // 当前跨度 ID
	ParentSpanID string        // 父跨度 ID（根跨度为空）
	OperationName string       // 操作名称
	StartTime    time.Time     // 开始时间
	Duration     time.Duration // 持续时间
	Tags         map[string]string // 标签
	Status       string        // 状态：OK / ERROR
}

// SpanCollector 收集所有 Span，用于最终展示
type SpanCollector struct {
	mu    sync.Mutex
	spans []Span
}

// globalCollector 全局 Span 收集器
var globalCollector = &SpanCollector{}

// AddSpan 添加一个完成的 Span
func (sc *SpanCollector) AddSpan(span Span) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.spans = append(sc.spans, span)
}

// generateID 生成模拟的 ID（简化版，实际应使用 128 位随机数）
func generateID() string {
	return fmt.Sprintf("%08x", rand.Int31())
}

// StartTrace 创建新的 Trace（根 Span）
// 返回携带 trace 信息的 context 和一个结束函数
func StartTrace(ctx context.Context, operationName string) (context.Context, func()) {
	traceID := generateID() + generateID() // 模拟 128 位 trace ID
	spanID := generateID()

	span := Span{
		TraceID:       traceID,
		SpanID:        spanID,
		OperationName: operationName,
		StartTime:     time.Now(),
		Tags:          make(map[string]string),
		Status:        "OK",
	}

	// 将 trace 信息存入 context
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	ctx = context.WithValue(ctx, spanIDKey, spanID)

	// 返回结束函数，调用时记录 Span 的持续时间
	return ctx, func() {
		span.Duration = time.Since(span.StartTime)
		globalCollector.AddSpan(span)
	}
}

// StartSpan 在现有 Trace 中创建子 Span
func StartSpan(ctx context.Context, operationName string) (context.Context, func()) {
	traceID, _ := ctx.Value(traceIDKey).(string)
	parentSpanID, _ := ctx.Value(spanIDKey).(string)
	spanID := generateID()

	span := Span{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		OperationName: operationName,
		StartTime:     time.Now(),
		Tags:          make(map[string]string),
		Status:        "OK",
	}

	// 更新 context 中的 spanID 为当前 Span
	ctx = context.WithValue(ctx, spanIDKey, spanID)

	return ctx, func() {
		span.Duration = time.Since(span.StartTime)
		globalCollector.AddSpan(span)
	}
}

// GetTraceID 从 context 中提取 trace ID
func GetTraceID(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return "unknown"
}

// 模拟带追踪的服务调用链

// handleHTTPRequest 模拟 HTTP 请求处理（入口 Span）
func handleHTTPRequest(ctx context.Context, logger *slog.Logger) {
	ctx, endSpan := StartTrace(ctx, "HTTP GET /api/users")
	defer endSpan()

	traceID := GetTraceID(ctx)
	// 请求作用域日志：每条日志都携带 trace_id，便于关联
	reqLogger := logger.With("trace_id", traceID)
	reqLogger.Info("收到 HTTP 请求", "method", "GET", "path", "/api/users")

	// 调用认证服务
	authenticateUser(ctx, reqLogger)

	// 查询数据库
	queryDatabase(ctx, reqLogger)

	// 调用外部服务
	callExternalService(ctx, reqLogger)

	reqLogger.Info("请求处理完成", "status", 200)
}

// authenticateUser 模拟认证服务（子 Span）
func authenticateUser(ctx context.Context, logger *slog.Logger) {
	_, endSpan := StartSpan(ctx, "auth.ValidateToken")
	defer endSpan()

	logger.Debug("验证用户令牌")
	time.Sleep(5 * time.Millisecond) // 模拟处理耗时
	logger.Debug("令牌验证通过", "user_id", "u-12345")
}

// queryDatabase 模拟数据库查询（子 Span）
func queryDatabase(ctx context.Context, logger *slog.Logger) {
	ctx, endSpan := StartSpan(ctx, "db.Query")
	defer endSpan()

	logger.Debug("执行数据库查询", "query", "SELECT * FROM users LIMIT 10")
	time.Sleep(15 * time.Millisecond) // 模拟查询耗时

	// 数据库查询中的子操作
	_, endSubSpan := StartSpan(ctx, "db.Serialize")
	time.Sleep(3 * time.Millisecond)
	endSubSpan()

	logger.Debug("查询完成", "rows", 10)
}

// callExternalService 模拟外部服务调用（子 Span）
func callExternalService(ctx context.Context, logger *slog.Logger) {
	_, endSpan := StartSpan(ctx, "http.Client GET /external/api")
	defer endSpan()

	logger.Debug("调用外部服务")
	time.Sleep(30 * time.Millisecond) // 模拟网络延迟
	logger.Debug("外部服务响应成功", "status", 200)
}

// demonstrateTracing 演示分布式追踪
func demonstrateTracing(logger *slog.Logger) {
	fmt.Println("=== 第三部分：分布式追踪 (Tracing) ===")
	fmt.Println()

	// 模拟处理 3 个请求
	fmt.Println("--- 模拟请求处理（带追踪） ---")
	for i := 0; i < 3; i++ {
		ctx := context.Background()
		handleHTTPRequest(ctx, logger)
		fmt.Println()
	}

	// 打印追踪结果
	fmt.Println("--- 追踪结果（Trace 视图） ---")
	globalCollector.mu.Lock()
	spans := make([]Span, len(globalCollector.spans))
	copy(spans, globalCollector.spans)
	globalCollector.mu.Unlock()

	// 按 Trace 分组展示
	traceMap := make(map[string][]Span)
	for _, s := range spans {
		traceMap[s.TraceID] = append(traceMap[s.TraceID], s)
	}

	traceNum := 0
	for traceID, traceSpans := range traceMap {
		traceNum++
		if traceNum > 1 {
			break // 只展示第一个 Trace 的详情
		}
		fmt.Printf("  Trace ID: %s\n", traceID)
		fmt.Println("  ┬")
		for i, s := range traceSpans {
			prefix := "  ├── "
			if i == len(traceSpans)-1 {
				prefix = "  └── "
			}
			parent := ""
			if s.ParentSpanID != "" {
				parent = fmt.Sprintf(" (parent: %s)", s.ParentSpanID[:8])
			}
			fmt.Printf("%s[%s] %s  %v%s\n", prefix, s.SpanID[:8], s.OperationName, s.Duration.Round(time.Millisecond), parent)
		}
	}
	fmt.Println()
}

// ============================================================
// 第四部分：综合演示 - /metrics HTTP 端点
// ============================================================

// demonstrateMetricsEndpoint 演示 /metrics 端点（打印模拟输出）
func demonstrateMetricsEndpoint(registry *MetricsRegistry) {
	fmt.Println("=== 第四部分：/metrics HTTP 端点 ===")
	fmt.Println()
	fmt.Println("在真实应用中，以下代码会启动 HTTP 服务器暴露 /metrics 端点：")
	fmt.Println()
	fmt.Println("  mux := http.NewServeMux()")
	fmt.Println("  mux.HandleFunc(\"/metrics\", metricsHandler(registry))")
	fmt.Println("  http.ListenAndServe(\":9090\", mux)")
	fmt.Println()

	// 创建一个模拟的 HTTP handler 来展示输出格式
	handler := metricsHandler(registry)
	_ = handler // handler 已准备就绪，可用于真实 HTTP 服务器
	fmt.Println("  Prometheus 采集配置示例（prometheus.yml）：")
	fmt.Println("  scrape_configs:")
	fmt.Println("    - job_name: 'gollm-app'")
	fmt.Println("      static_configs:")
	fmt.Println("        - targets: ['localhost:9090']")
	fmt.Println()
}

// metricsHandler 返回一个 HTTP handler，输出 Prometheus 格式的指标
func metricsHandler(registry *MetricsRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, registry.ExposeMetrics())
	}
}

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║   可观测性三大支柱演示               ║")
	fmt.Println("║   Logs · Metrics · Traces            ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// 第一部分：结构化日志
	demonstrateLogging()

	// 第二部分：指标收集
	registry := NewMetricsRegistry()
	demonstrateMetrics(registry)

	// 第三部分：分布式追踪
	logger := setupJSONLogger()
	demonstrateTracing(logger)

	// 第四部分：/metrics 端点
	demonstrateMetricsEndpoint(registry)

	// ===== OpenTelemetry 概念映射总结 =====
	fmt.Println("=== OpenTelemetry 概念映射 ===")
	fmt.Println()
	fmt.Println("  ┌──────────────┬───────────────────────┬──────────────────────────┐")
	fmt.Println("  │ 信号         │ 本示例实现            │ OpenTelemetry 对应       │")
	fmt.Println("  ├──────────────┼───────────────────────┼──────────────────────────┤")
	fmt.Println("  │ Logs         │ log/slog              │ otel-go Log SDK          │")
	fmt.Println("  │ Metrics      │ Counter/Histogram     │ otel-go Metric SDK       │")
	fmt.Println("  │ Traces       │ Context + Span        │ otel-go Trace SDK        │")
	fmt.Println("  ├──────────────┼───────────────────────┼──────────────────────────┤")
	fmt.Println("  │ 导出目标     │ stdout / /metrics     │ OTLP Exporter            │")
	fmt.Println("  │ 采集器       │ (无)                  │ OpenTelemetry Collector   │")
	fmt.Println("  │ 后端存储     │ (无)                  │ Jaeger/Prometheus/Loki   │")
	fmt.Println("  └──────────────┴───────────────────────┴──────────────────────────┘")
	fmt.Println()
	fmt.Println("  最佳实践：")
	fmt.Println("  1. 日志中携带 trace_id，便于从日志关联到链路追踪")
	fmt.Println("  2. 使用结构化日志（JSON），便于机器解析和查询")
	fmt.Println("  3. 合理设置直方图桶的边界值，覆盖常见延迟范围")
	fmt.Println("  4. 使用 context.Context 在整个调用链中传播追踪信息")
	fmt.Println("  5. 在生产环境中使用 OpenTelemetry SDK 替代手动实现")
}
