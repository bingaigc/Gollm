// Package main 演示可观测性的基础概念
//
// 本示例用纯 Go 标准库介绍可观测性三大支柱的基本原理：
//   - 日志（Logging）：记录离散事件，是最传统的观测手段
//   - 指标（Metrics）：聚合的数值数据，用于监控系统健康
//   - 追踪（Tracing）：跨服务/函数的请求链路跟踪
//
// 同时介绍健康检查模式和日志采样策略等实用技巧。
// 所有实现均使用 Go 标准库，不引入任何外部依赖。
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// 第一部分：日志基础 — 标准库 log 包
// ============================================================

// demonstrateBasicLogging 演示 Go 标准库 log 包的基本用法
func demonstrateBasicLogging() {
	fmt.Println("📝 === 第一部分：日志基础 ===")
	fmt.Println()

	// --- 1.1 默认 Logger：输出到 stderr，带日期和时间前缀 ---
	fmt.Println("📌 1.1 默认 Logger 输出：")
	log.Println("这是一条默认格式的日志")
	log.Printf("带格式化的日志：用户 %s 登录成功，耗时 %dms", "alice", 42)
	fmt.Println()

	// --- 1.2 自定义 Logger：指定输出目标、前缀和标志 ---
	fmt.Println("📌 1.2 自定义 Logger（不同前缀和标志）：")
	infoLogger := log.New(os.Stdout, "[INFO]  ", log.Ldate|log.Ltime|log.Lshortfile)
	warnLogger := log.New(os.Stdout, "[WARN]  ", log.Ldate|log.Ltime|log.Lshortfile)
	errorLogger := log.New(os.Stderr, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)
	infoLogger.Println("服务启动成功，监听端口 8080")
	warnLogger.Println("数据库连接池使用率超过 80%")
	errorLogger.Println("Redis 连接超时，将使用本地缓存")
	fmt.Println()

	// --- 1.3 日志级别概念（标准库无内置级别，通过多个 Logger 模拟）---
	fmt.Println("📌 1.3 日志级别说明：")
	fmt.Println("  DEBUG - 调试信息，仅开发环境启用")
	fmt.Println("  INFO  - 常规运行信息")
	fmt.Println("  WARN  - 警告，潜在问题")
	fmt.Println("  ERROR - 错误，需要关注")
	fmt.Println("  FATAL - 致命错误，程序将退出")
	fmt.Println()

	// --- 1.4 日志标志组合 ---
	fmt.Println("📌 1.4 日志标志组合效果：")
	log.New(os.Stdout, "[MICRO] ", log.Ldate|log.Ltime|log.Lmicroseconds).Println("微秒精度时间戳")
	log.New(os.Stdout, "[UTC]   ", log.Ldate|log.Ltime|log.LUTC).Println("UTC 时间戳")
	log.New(os.Stdout, "[APP] ", log.Ldate|log.Ltime|log.Lmsgprefix).Println("前缀在消息前方")
	fmt.Println()
}

// ============================================================
// 第二部分：结构化日志原理
// ============================================================

// LogLevel 表示日志级别（数值越大越严重）
type LogLevel int
const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l LogLevel) String() string {
	names := [...]string{"DEBUG", "INFO", "WARN", "ERROR"}
	if int(l) < len(names) { return names[l] }
	return "UNKNOWN"
}

// StructuredLogger 简单的结构化日志输出器（key=value 格式）
type StructuredLogger struct {
	mu       sync.Mutex
	minLevel LogLevel
	fields   map[string]any
}

func NewStructuredLogger(minLevel LogLevel) *StructuredLogger {
	return &StructuredLogger{minLevel: minLevel, fields: make(map[string]any)}
}

func (sl *StructuredLogger) WithField(key string, value any) *StructuredLogger {
	sl.mu.Lock(); defer sl.mu.Unlock()
	sl.fields[key] = value
	return sl
}

// Log 输出结构化日志：timestamp=... level=... msg=... key=value ...
func (sl *StructuredLogger) Log(level LogLevel, msg string, kvPairs ...any) {
	if level < sl.minLevel {
		return
	}
	sl.mu.Lock(); defer sl.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "timestamp=%s level=%-5s msg=%q",
		time.Now().Format("2006-01-02T15:04:05.000Z07:00"), level, msg)
	for k, v := range sl.fields {
		fmt.Fprintf(&sb, " %s=%v", k, v)
	}
	for i := 0; i+1 < len(kvPairs); i += 2 {
		fmt.Fprintf(&sb, " %s=%v", kvPairs[i], kvPairs[i+1])
	}
	fmt.Println(sb.String())
}

func demonstrateStructuredLogging() {
	fmt.Println("📊 === 第二部分：结构化日志原理 ===")
	fmt.Println()
	fmt.Println("📌 2.1 传统日志 vs 结构化日志：")
	fmt.Println("  传统：2024-01-15 用户 alice 登录成功，耗时 42ms")
	fmt.Println("  结构化：timestamp=2024-01-15 level=INFO msg=\"用户登录\" user=alice latency_ms=42")
	fmt.Println("  ➡️ 结构化日志便于机器解析、搜索和聚合分析")
	fmt.Println()

	fmt.Println("📌 2.2 结构化日志输出器实战：")
	logger := NewStructuredLogger(LevelInfo)
	logger.WithField("service", "user-api").WithField("version", "1.0.0")
	logger.Log(LevelDebug, "调试信息，不会显示")               // 被过滤
	logger.Log(LevelInfo, "服务启动", "port", 8080, "env", "production")
	logger.Log(LevelWarn, "慢查询告警", "query", "SELECT *", "duration_ms", 1500)
	logger.Log(LevelError, "数据库连接失败", "host", "db-master", "retry", 3)
	fmt.Println()
}

// ============================================================
// 第三部分：指标类型入门
// ============================================================
// Counter 计数器：只增不减（请求总数、错误总数）
type Counter struct{ name string; value int64 }

func NewCounter(name string) *Counter { return &Counter{name: name} }
func (c *Counter) Inc()               { atomic.AddInt64(&c.value, 1) }
func (c *Counter) Value() int64       { return atomic.LoadInt64(&c.value) }
func (c *Counter) String() string     { return fmt.Sprintf("Counter{%s=%d}", c.name, c.Value()) }

// Gauge 仪表盘：可增可减（连接数、内存使用量）
type Gauge struct{ name string; value int64 }

func NewGauge(name string) *Gauge { return &Gauge{name: name} }
func (g *Gauge) Set(v int64)     { atomic.StoreInt64(&g.value, v) }
func (g *Gauge) Inc()            { atomic.AddInt64(&g.value, 1) }
func (g *Gauge) Dec()            { atomic.AddInt64(&g.value, -1) }
func (g *Gauge) Value() int64    { return atomic.LoadInt64(&g.value) }
func (g *Gauge) String() string  { return fmt.Sprintf("Gauge{%s=%d}", g.name, g.Value()) }

// Timer 计时器：记录耗时分布（请求延迟、查询时间）
type Timer struct {
	mu              sync.Mutex
	name            string
	count           int64
	total, min, max time.Duration
}

func NewTimer(name string) *Timer {
	return &Timer{name: name, min: time.Duration(1<<63 - 1)}
}

// Observe 记录一次耗时
func (t *Timer) Observe(d time.Duration) {
	t.mu.Lock(); defer t.mu.Unlock()
	t.count++; t.total += d
	if d < t.min { t.min = d }
	if d > t.max { t.max = d }
}

// TimeFunc 测量函数执行时间并自动记录
func (t *Timer) TimeFunc(fn func()) time.Duration {
	start := time.Now()
	fn()
	d := time.Since(start)
	t.Observe(d)
	return d
}

func (t *Timer) Summary() string {
	t.mu.Lock(); defer t.mu.Unlock()
	if t.count == 0 { return fmt.Sprintf("Timer{%s, count=0}", t.name) }
	avg := t.total / time.Duration(t.count)
	return fmt.Sprintf("Timer{%s, count=%d, avg=%v, min=%v, max=%v}", t.name, t.count, avg, t.min, t.max)
}

func demonstrateMetrics() {
	fmt.Println("📈 === 第三部分：指标类型入门 ===")
	fmt.Println()

	// --- Counter ---
	fmt.Println("📌 3.1 Counter（计数器）— 只增不减：")
	reqCounter := NewCounter("http_requests_total")
	errCounter := NewCounter("http_errors_total")
	for i := 0; i < 20; i++ {
		reqCounter.Inc()
		if rng.Intn(5) == 0 {
			errCounter.Inc()
		}
	}
	fmt.Printf("  %s\n", reqCounter)
	fmt.Printf("  %s\n", errCounter)
	if reqCounter.Value() > 0 {
		fmt.Printf("  错误率：%.1f%%\n", float64(errCounter.Value())/float64(reqCounter.Value())*100)
	}
	fmt.Println()

	// --- Gauge ---
	fmt.Println("📌 3.2 Gauge（仪表盘）— 可增可减：")
	connGauge := NewGauge("active_connections")
	connGauge.Set(10)
	fmt.Printf("  初始：%s\n", connGauge)
	connGauge.Inc()
	connGauge.Inc()
	connGauge.Inc()
	fmt.Printf("  +3 连接：%s\n", connGauge)
	connGauge.Dec()
	connGauge.Dec()
	fmt.Printf("  -2 断开：%s\n", connGauge)
	fmt.Println()

	// --- Timer ---
	fmt.Println("📌 3.3 Timer（计时器）— 耗时分布：")
	apiTimer := NewTimer("api_request_duration")
	for _, d := range []time.Duration{5, 12, 3, 45, 8, 120, 7} {
		apiTimer.Observe(d * time.Millisecond)
	}
	fmt.Printf("  %s\n", apiTimer.Summary())
	dbTimer := NewTimer("db_query_duration")
	elapsed := dbTimer.TimeFunc(func() { time.Sleep(2 * time.Millisecond) })
	fmt.Printf("  自动计时：查询耗时 %v → %s\n", elapsed, dbTimer.Summary())
	fmt.Println()

	fmt.Println("📌 3.4 指标类型对比：")
	fmt.Println("  Counter — 只增不减 → 请求总数、错误总数")
	fmt.Println("  Gauge   — 可增可减 → 连接数、内存使用")
	fmt.Println("  Timer   — 耗时分布 → 请求延迟、查询时间")
	fmt.Println()
}

// ============================================================
// 第四部分：请求追踪基础
// ============================================================

// rng 用于演示的本地随机源（固定种子保证输出可重现）
var rng *rand.Rand
type contextKey string
const requestIDKey contextKey = "request_id"

func generateID() string {
	const charset = "abcdef0123456789"
	b := make([]byte, 8)
	for i := range b { b[i] = charset[rng.Intn(len(charset))] }
	return string(b)
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok { return id }
	return "unknown"
}

// traceLog 输出带追踪信息的日志
func traceLog(ctx context.Context, op, msg string) {
	fmt.Printf("  [req=%s] [op=%s] %s\n", getRequestID(ctx), op, msg)
}

func simulateAuthService(ctx context.Context, user string) {
	traceLog(ctx, "auth", fmt.Sprintf("验证用户 %s 的凭证", user))
	time.Sleep(1 * time.Millisecond)
	traceLog(ctx, "auth", fmt.Sprintf("用户 %s 认证成功", user))
}

func simulateDBQuery(ctx context.Context, query string) {
	traceLog(ctx, "db", fmt.Sprintf("执行查询: %s", query))
	time.Sleep(2 * time.Millisecond)
	traceLog(ctx, "db", "查询返回 3 条记录")
}

func simulateCacheCheck(ctx context.Context, key string) bool {
	traceLog(ctx, "cache", fmt.Sprintf("检查缓存 key=%s", key))
	hit := rng.Intn(2) == 0
	if hit {
		traceLog(ctx, "cache", "缓存命中 ✅")
	} else {
		traceLog(ctx, "cache", "缓存未命中 ❌")
	}
	return hit
}

// handleRequest 模拟处理一个完整的 HTTP 请求链路
func handleRequest(ctx context.Context, user string) {
	reqID := getRequestID(ctx)
	fmt.Printf("  ┌─ 请求开始 [req=%s]\n", reqID)
	simulateAuthService(ctx, user)
	if !simulateCacheCheck(ctx, "user:"+user+":profile") {
		simulateDBQuery(ctx, "SELECT * FROM users WHERE name='"+user+"'")
	}
	traceLog(ctx, "handler", "请求处理完成，返回 200")
	fmt.Printf("  └─ 请求结束 [req=%s]\n", reqID)
}

func demonstrateTracing() {
	fmt.Println("🔍 === 第四部分：请求追踪基础 ===")
	fmt.Println()
	fmt.Println("📌 4.1 通过 Context 传递 Request ID：")
	fmt.Println("  请求 ID 是追踪的基础——同一请求的所有日志共享同一 ID")
	fmt.Println()
	fmt.Println("📌 4.2 模拟请求链路追踪：")
	fmt.Println()
	fmt.Println("  --- 请求 A ---")
	handleRequest(withRequestID(context.Background(), generateID()), "alice")
	fmt.Println()
	fmt.Println("  --- 请求 B ---")
	handleRequest(withRequestID(context.Background(), generateID()), "bob")
	fmt.Println()
	fmt.Println("📌 4.3 追踪的核心思想：")
	fmt.Println("  ✅ 每个请求分配唯一 ID（Trace ID）")
	fmt.Println("  ✅ ID 通过 context.Context 在调用链中传递")
	fmt.Println("  ✅ 所有日志包含该 ID，便于关联分析")
	fmt.Println()
}

// ============================================================
// 第五部分：健康检查模式
// ============================================================

// DependencyStatus 依赖的健康状态
type DependencyStatus struct {
	Name    string
	Healthy bool
	Latency time.Duration
	Message string
}

// HealthChecker 区分 Liveness（存活）与 Readiness（就绪）检查
type HealthChecker struct {
	mu     sync.RWMutex
	checks map[string]func() DependencyStatus
	ready  bool
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{checks: make(map[string]func() DependencyStatus)}
}

func (hc *HealthChecker) RegisterCheck(name string, fn func() DependencyStatus) {
	hc.mu.Lock(); defer hc.mu.Unlock()
	hc.checks[name] = fn
}

func (hc *HealthChecker) SetReady(r bool) {
	hc.mu.Lock(); defer hc.mu.Unlock()
	hc.ready = r
}

// Liveness 存活检查（失败 → 重启容器）
func (hc *HealthChecker) Liveness() (bool, string) { return true, "进程正常运行" }

// Readiness 就绪检查（失败 → 暂停接收流量）
func (hc *HealthChecker) Readiness() (bool, map[string]DependencyStatus) {
	hc.mu.RLock(); defer hc.mu.RUnlock()
	results := make(map[string]DependencyStatus)
	allHealthy := hc.ready
	for name, check := range hc.checks {
		s := check(); results[name] = s
		if !s.Healthy { allHealthy = false }
	}
	return allHealthy, results
}

func demonstrateHealthCheck() {
	fmt.Println("🏥 === 第五部分：健康检查模式 ===")
	fmt.Println()
	fmt.Println("📌 5.1 Liveness vs Readiness：")
	fmt.Println("  Liveness（存活）：进程还活着吗？失败 → 重启容器")
	fmt.Println("  Readiness（就绪）：能接受请求吗？失败 → 暂停接收流量")
	fmt.Println()

	hc := NewHealthChecker()
	hc.RegisterCheck("database", func() DependencyStatus {
		start := time.Now()
		time.Sleep(1 * time.Millisecond)
		return DependencyStatus{"database", true, time.Since(start), "PostgreSQL 连接正常"}
	})
	hc.RegisterCheck("cache", func() DependencyStatus {
		start := time.Now()
		time.Sleep(500 * time.Microsecond)
		return DependencyStatus{"cache", true, time.Since(start), "Redis 连接正常"}
	})
	hc.RegisterCheck("payment-api", func() DependencyStatus {
		start := time.Now()
		time.Sleep(1 * time.Millisecond)
		return DependencyStatus{"payment-api", false, time.Since(start), "连接超时"}
	})

	fmt.Println("📌 5.2 执行存活检查（Liveness）：")
	alive, msg := hc.Liveness()
	fmt.Printf("  存活状态：%v — %s\n", alive, msg)
	fmt.Println()

	fmt.Println("📌 5.3 执行就绪检查（Readiness）：")
	hc.SetReady(true)
	ready, deps := hc.Readiness()
	fmt.Printf("  就绪状态：%v\n", ready)
	for name, status := range deps {
		icon := "✅"
		if !status.Healthy {
			icon = "❌"
		}
		fmt.Printf("  %s %s: %s (延迟: %v)\n", icon, name, status.Message, status.Latency)
	}
	fmt.Println()

	fmt.Println("📌 5.4 健康检查最佳实践：")
	fmt.Println("  ✅ Liveness 检查应轻量，只验证进程存活")
	fmt.Println("  ✅ Readiness 检查验证所有关键依赖")
	fmt.Println("  ✅ 设置合理的超时，区分关键/非关键依赖")
	fmt.Println()
}

// ============================================================
// 第六部分：日志采样
// ============================================================

// LogSampler 日志采样器——高流量场景下避免日志风暴
type LogSampler struct {
	mu                      sync.Mutex
	counts                  map[string]int64
	lastLogged              map[string]time.Time
	sampleRate              int64         // 每 N 条输出一条
	interval                time.Duration // 同类日志最小输出间隔
}

func NewLogSampler(rate int64, interval time.Duration) *LogSampler {
	return &LogSampler{
		counts: make(map[string]int64), lastLogged: make(map[string]time.Time),
		sampleRate: rate, interval: interval,
	}
}

// ShouldLog 判断日志是否应该输出（首条必输出 + 计数采样 + 时间采样）
func (ls *LogSampler) ShouldLog(key string) (bool, int64) {
	ls.mu.Lock(); defer ls.mu.Unlock()
	ls.counts[key]++
	count := ls.counts[key]
	if count == 1 { // 首条必采
		ls.lastLogged[key] = time.Now()
		return true, count
	}
	if count%ls.sampleRate == 0 { // 计数采样
		ls.lastLogged[key] = time.Now()
		return true, count
	}
	if last, ok := ls.lastLogged[key]; ok && time.Since(last) >= ls.interval {
		ls.lastLogged[key] = time.Now()
		return true, count
	}
	return false, count
}

func demonstrateLogSampling() {
	fmt.Println("🎯 === 第六部分：日志采样 ===")
	fmt.Println()
	fmt.Println("📌 6.1 为什么需要日志采样？")
	fmt.Println("  高频事件每次都记录会导致日志风暴，消耗大量 I/O 和存储。")
	fmt.Println("  采样策略可以有效减少日志量，同时保留关键信息。")
	fmt.Println()

	fmt.Println("📌 6.2 采样演示（每 10 条输出 1 条）：")
	sampler := NewLogSampler(10, 5*time.Second)
	outputCount := 0
	totalCount := 50
	for i := 1; i <= totalCount; i++ {
		if shouldLog, count := sampler.ShouldLog("heartbeat"); shouldLog {
			fmt.Printf("  [采样输出] 心跳检查 #%d（总计第 %d 次）\n", i, count)
			outputCount++
		}
	}
	fmt.Printf("  📊 总事件: %d, 实际输出: %d, 采样率: %.1f%%\n",
		totalCount, outputCount, float64(outputCount)/float64(totalCount)*100)
	fmt.Println()

	fmt.Println("📌 6.3 不同类型事件独立采样：")
	multiSampler := NewLogSampler(5, 5*time.Second)
	events := []string{"login", "login", "login", "login", "login",
		"query", "query", "query", "query", "query",
		"login", "login", "login", "login", "login",
		"error", "error", "error"}
	for i, event := range events {
		if shouldLog, count := multiSampler.ShouldLog(event); shouldLog {
			fmt.Printf("  [输出] 事件=%s 序号=%d 该类型总计=%d\n", event, i+1, count)
		}
	}
	fmt.Println()

	fmt.Println("📌 6.4 采样策略对比：")
	fmt.Println("  计数采样 — 每 N 条输出 1 条")
	fmt.Println("  时间采样 — 同类日志间隔至少 T 秒")
	fmt.Println("  概率采样 — 按概率随机保留（如 10%）")
	fmt.Println("  首条必采 — 新出现的日志类型立即输出")
	fmt.Println()
}

// ============================================================
// 主函数
// ============================================================

func main() {
	rng = rand.New(rand.NewSource(42))

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║     Day 27: 可观测性入门 - Observability Basics     ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()

	demonstrateBasicLogging()
	demonstrateStructuredLogging()
	demonstrateMetrics()
	demonstrateTracing()
	demonstrateHealthCheck()
	demonstrateLogSampling()

	fmt.Println("🎓 === 总结 ===")
	fmt.Println()
	fmt.Println("可观测性三大支柱：")
	fmt.Println("  📝 日志（Logs）    — 离散事件的详细记录")
	fmt.Println("  📈 指标（Metrics） — 聚合的数值数据")
	fmt.Println("  🔍 追踪（Traces） — 请求的调用链路")
	fmt.Println()
	fmt.Println("附加实践：")
	fmt.Println("  🏥 健康检查 — Liveness + Readiness 保障服务可用性")
	fmt.Println("  🎯 日志采样 — 减少日志量，避免日志风暴")
	fmt.Println()
	fmt.Println("💡 Day 28 将深入介绍 slog 结构化日志、Prometheus 指标和分布式追踪")
	fmt.Println()
	fmt.Println("✅ Day 27 可观测性入门演示完成！")
}

