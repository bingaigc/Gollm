// Day 13 - 高级中间件模式
// 本示例深入演示 gRPC 拦截器的高级用法
// 使用标准库模拟重试、断路器、请求追踪等生产级中间件
//
// ============================================================
// 高级中间件模式说明：
//
// 1. 重试中间件 - 自动重试失败请求，带指数退避策略
// 2. 断路器（Circuit Breaker）- 状态机: Closed → Open → HalfOpen
// 3. 熔断统计与阈值 - 追踪失败率，自动触发熔断
// 4. 请求追踪（Tracing）- 生成 TraceID/SpanID
// 5. 多中间件组合 - 洋葱模型的高级应用
// 6. 优雅降级 - 当服务不可用时返回降级结果
//
// 拦截器执行顺序（洋葱模型）：
//   请求 → 追踪 → 重试 → 断路器 → 降级 → 处理器
//        ← 追踪 ← 重试 ← 断路器 ← 降级 ← 响应
// ============================================================

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"
)

// ============================================================
// 核心类型定义（与 Day 12 保持一致的抽象）
// ============================================================

// Request 统一请求结构（模拟 gRPC 的 proto 消息）
type Request struct {
	Method string
	Body   string
}

// Response 统一响应结构
type Response struct {
	Code    int
	Message string
}

// Handler 处理器函数类型（对应 gRPC UnaryHandler）
type Handler func(ctx context.Context, req *Request) (*Response, error)

// Interceptor 拦截器函数类型（对应 gRPC UnaryServerInterceptor）
type Interceptor func(ctx context.Context, req *Request, next Handler) (*Response, error)

type contextKey string

const (
	ctxKeyTraceID contextKey = "trace_id"
	ctxKeySpanID  contextKey = "span_id"
)

// ============================================================
// 1. 重试中间件 - 自动重试失败请求，带指数退避
// ============================================================

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// RetryInterceptor 创建带指数退避的重试拦截器
func RetryInterceptor(config RetryConfig) Interceptor {
	return func(ctx context.Context, req *Request, next Handler) (*Response, error) {
		var lastErr error
		var lastResp *Response
		for attempt := 0; attempt <= config.MaxRetries; attempt++ {
			if attempt > 0 {
				// 指数退避: baseDelay * 2^(attempt-1)
				delay := time.Duration(float64(config.BaseDelay) * math.Pow(2, float64(attempt-1)))
				if delay > config.MaxDelay {
					delay = config.MaxDelay
				}
				fmt.Printf("    [重试] 第 %d 次重试，等待 %v...\n", attempt, delay)
				// 随机抖动，防止 thundering herd
				jitterN, _ := rand.Int(rand.Reader, big.NewInt(int64(delay/10)+1))
				time.Sleep(delay + time.Duration(jitterN.Int64()))
				if ctx.Err() != nil {
					return nil, fmt.Errorf("重试被取消: %w", ctx.Err())
				}
			}
			resp, err := next(ctx, req)
			if err == nil && resp.Code < 500 {
				if attempt > 0 {
					fmt.Printf("    [重试] 第 %d 次重试成功!\n", attempt)
				}
				return resp, nil
			}
			lastErr = err
			lastResp = resp
			fmt.Printf("    [重试] 请求失败 (attempt=%d): %v\n", attempt+1, err)
		}
		if lastErr != nil {
			return nil, fmt.Errorf("重试 %d 次后仍然失败: %w", config.MaxRetries, lastErr)
		}
		return lastResp, nil
	}
}

// ============================================================
// 2. 断路器（Circuit Breaker）- 状态机实现
// ============================================================
// Closed(正常) --失败>阈值--> Open(熔断) --超时--> HalfOpen(探测)
// HalfOpen --成功--> Closed     HalfOpen --失败--> Open
type CircuitState int

const (
	StateClosed   CircuitState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "Closed(正常)"
	case StateOpen:
		return "Open(熔断)"
	case StateHalfOpen:
		return "HalfOpen(探测)"
	default:
		return "Unknown"
	}
}

// CircuitBreaker 断路器
type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	lastFailureTime  time.Time
	totalRequests    int
	totalFailures    int
}

// NewCircuitBreaker 创建断路器
func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
}

func (cb *CircuitBreaker) Allow() (bool, CircuitState) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.totalRequests++
	switch cb.state {
	case StateClosed:
		return true, cb.state
	case StateOpen:
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			fmt.Printf("    [断路器] 超时 → HalfOpen\n")
			return true, cb.state
		}
		return false, cb.state
	case StateHalfOpen:
		return true, cb.state
	default:
		return false, cb.state
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateClosed:
		cb.failureCount = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.successCount = 0
			fmt.Printf("    [断路器] 连续成功 %d 次 → Closed\n", cb.successThreshold)
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.totalFailures++
	cb.lastFailureTime = time.Now()
	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
			fmt.Printf("    [断路器] 连续失败 %d 次 → Open\n", cb.failureCount)
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.successCount = 0
		fmt.Printf("    [断路器] 探测失败 → Open\n")
	}
}

func (cb *CircuitBreaker) Stats() (CircuitState, int, int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state, cb.totalRequests, cb.totalFailures
}

// CircuitBreakerInterceptor 断路器拦截器
func CircuitBreakerInterceptor(cb *CircuitBreaker) Interceptor {
	return func(ctx context.Context, req *Request, next Handler) (*Response, error) {
		allowed, state := cb.Allow()
		if !allowed {
			fmt.Printf("    [断路器] 请求被拒绝 (状态: %s)\n", state)
			return nil, fmt.Errorf("断路器打开，请求被拒绝")
		}
		resp, err := next(ctx, req)
		if err != nil || (resp != nil && resp.Code >= 500) {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
		return resp, err
	}
}

// ============================================================
// 3. 请求追踪中间件（Tracing）
// ============================================================
// TraceID: 请求链路唯一标识 / SpanID: 单个服务处理标识

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// TracingInterceptor 追踪拦截器
func TracingInterceptor(ctx context.Context, req *Request, next Handler) (*Response, error) {
	traceID, _ := ctx.Value(ctxKeyTraceID).(string)
	if traceID == "" {
		traceID = generateID()
	}
	spanID := generateID()
	ctx = context.WithValue(ctx, ctxKeyTraceID, traceID)
	ctx = context.WithValue(ctx, ctxKeySpanID, spanID)
	start := time.Now()
	fmt.Printf("    [追踪] 开始 | TraceID=%s SpanID=%s | %s\n", traceID, spanID, req.Method)
	resp, err := next(ctx, req)
	status := "成功"
	if err != nil {
		status = fmt.Sprintf("失败(%v)", err)
	}
	fmt.Printf("    [追踪] 结束 | TraceID=%s | 耗时=%v | %s\n",
		traceID, time.Since(start).Round(time.Microsecond), status)
	return resp, err
}

// ============================================================
// 4. 优雅降级中间件
// ============================================================

type FallbackFunc func(ctx context.Context, req *Request, err error) (*Response, error)

// GracefulDegradationInterceptor 优雅降级拦截器
func GracefulDegradationInterceptor(fallback FallbackFunc) Interceptor {
	return func(ctx context.Context, req *Request, next Handler) (*Response, error) {
		resp, err := next(ctx, req)
		if err == nil && resp.Code < 500 {
			return resp, nil
		}
		fmt.Printf("    [降级] 主请求失败，启动降级...\n")
		degradedResp, degradedErr := fallback(ctx, req, err)
		if degradedErr != nil {
			fmt.Printf("    [降级] 降级也失败: %v\n", degradedErr)
			return resp, err
		}
		fmt.Printf("    [降级] 返回降级结果: %s\n", degradedResp.Message)
		return degradedResp, nil
	}
}

// ============================================================
// 5. 多中间件组合 & 业务处理器
// ============================================================

// ChainInterceptors 将多个拦截器串联（洋葱模型）
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return func(ctx context.Context, req *Request, finalHandler Handler) (*Response, error) {
		handler := finalHandler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			currentHandler := handler
			handler = func(ctx context.Context, req *Request) (*Response, error) {
				return interceptor(ctx, req, currentHandler)
			}
		}
		return handler(ctx, req)
	}
}

// unreliableHandler 创建按模式失败的处理器（true=失败）
func unreliableHandler(failPattern []bool) Handler {
	idx := 0
	var mu sync.Mutex
	return func(ctx context.Context, req *Request) (*Response, error) {
		mu.Lock()
		shouldFail := idx < len(failPattern) && failPattern[idx]
		idx++
		mu.Unlock()
		if shouldFail {
			return nil, errors.New("服务暂时不可用")
		}
		traceID, _ := ctx.Value(ctxKeyTraceID).(string)
		return &Response{Code: 200, Message: fmt.Sprintf("成功 [%s, trace=%s]", req.Method, traceID)}, nil
	}
}

func alwaysFailHandler(_ context.Context, _ *Request) (*Response, error) {
	return nil, errors.New("服务完全不可用")
}

func successHandler(ctx context.Context, req *Request) (*Response, error) {
	traceID, _ := ctx.Value(ctxKeyTraceID).(string)
	spanID, _ := ctx.Value(ctxKeySpanID).(string)
	return &Response{Code: 200,
		Message: fmt.Sprintf("成功 [%s, trace=%s, span=%s]", req.Method, traceID, spanID)}, nil
}

func main() {
	fmt.Println("=== Day 13: 高级中间件模式 ===")
	fmt.Println("深入 gRPC 拦截器的高级用法")
	fmt.Println()

	demoRetryInterceptor()
	demoCircuitBreaker()
	demoTracing()
	demoGracefulDegradation()
	demoMiddlewareComposition()
	demoCircuitBreakerStats()
	fmt.Println("=== Day 13 演示完成 ===")
}

// demoRetryInterceptor 演示重试中间件
func demoRetryInterceptor() {
	fmt.Println("--- 演示 1: 重试中间件（指数退避）---")
	fmt.Println("  场景 A：前2次失败，第3次成功")
	handler := unreliableHandler([]bool{true, true, false})
	retry := RetryInterceptor(RetryConfig{
		MaxRetries: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond,
	})
	resp, err := retry(context.Background(), &Request{Method: "GetUser", Body: "id=123"}, handler)
	if err != nil {
		fmt.Printf("  结果: 失败 - %v\n", err)
	} else {
		fmt.Printf("  结果: 成功 - %s\n", resp.Message)
	}
	fmt.Println()

	fmt.Println("  场景 B：服务完全不可用，所有重试失败")
	retry2 := RetryInterceptor(RetryConfig{
		MaxRetries: 2, BaseDelay: 5 * time.Millisecond, MaxDelay: 50 * time.Millisecond,
	})
	_, err = retry2(context.Background(), &Request{Method: "GetUser", Body: "id=456"}, alwaysFailHandler)
	if err != nil {
		fmt.Printf("  结果: 失败 - %v\n", err)
	}
	fmt.Println()
}

// demoCircuitBreaker 演示断路器状态转换
func demoCircuitBreaker() {
	fmt.Println("--- 演示 2: 断路器（Circuit Breaker）---")
	fmt.Println("  配置: 失败3次熔断, 50ms后半开, 成功2次恢复")

	cb := NewCircuitBreaker(3, 2, 50*time.Millisecond)
	cbInterceptor := CircuitBreakerInterceptor(cb)
	handler := unreliableHandler([]bool{true, true, true, true, true, false, false, false})
	req := &Request{Method: "QueryOrder", Body: "id=456"}
	ctx := context.Background()

	fmt.Println("  阶段 1: 连续请求（服务故障中）")
	for i := 1; i <= 5; i++ {
		state, _, _ := cb.Stats()
		_, err := cbInterceptor(ctx, req, handler)
		if err != nil {
			fmt.Printf("    请求 #%d: ✗ %v (状态: %s)\n", i, err, state)
		}
	}
	fmt.Println()

	fmt.Println("  阶段 2: 等待 50ms 超时...")
	time.Sleep(60 * time.Millisecond)
	fmt.Println("  阶段 3: 发送探测请求（服务已恢复）")
	for i := 6; i <= 8; i++ {
		state, _, _ := cb.Stats()
		resp, err := cbInterceptor(ctx, req, handler)
		if err != nil {
			fmt.Printf("    请求 #%d: ✗ %v (状态: %s)\n", i, err, state)
		} else {
			fmt.Printf("    请求 #%d: ✓ %s (状态: %s)\n", i, resp.Message, state)
		}
	}
	fmt.Println()
}

// demoTracing 演示请求追踪
func demoTracing() {
	fmt.Println("--- 演示 3: 请求追踪（Tracing）---")
	req := &Request{Method: "CreateOrder", Body: "item=book"}
	fmt.Println("  场景 1: 新请求（自动生成 TraceID）")
	resp, _ := TracingInterceptor(context.Background(), req, successHandler)
	fmt.Printf("  结果: %s\n", resp.Message)
	fmt.Println()

	fmt.Println("  场景 2: 继承上游 TraceID（跨服务调用）")
	ctx := context.WithValue(context.Background(), ctxKeyTraceID, "upstream-abc123")
	resp, _ = TracingInterceptor(ctx, req, successHandler)
	fmt.Printf("  结果: %s\n", resp.Message)
	fmt.Println()

	// 场景 3: 多层追踪 ServiceA → ServiceB，共享同一 TraceID
	fmt.Println("  场景 3: 多层追踪 (ServiceA → ServiceB)")
	serviceBHandler := func(ctx context.Context, req *Request) (*Response, error) {
		return TracingInterceptor(ctx,
			&Request{Method: "ServiceB.Pay", Body: req.Body}, successHandler)
	}
	resp, _ = TracingInterceptor(context.Background(),
		&Request{Method: "ServiceA.Order", Body: "order=999"}, serviceBHandler)
	fmt.Printf("  结果: %s\n", resp.Message)
	fmt.Println()
}

// demoGracefulDegradation 演示优雅降级
func demoGracefulDegradation() {
	fmt.Println("--- 演示 4: 优雅降级 ---")
	fallback := func(_ context.Context, req *Request, origErr error) (*Response, error) {
		return &Response{Code: 200,
			Message: fmt.Sprintf("[降级数据] %s 默认响应 (原因: %v)", req.Method, origErr)}, nil
	}
	degradation := GracefulDegradationInterceptor(fallback)
	req := &Request{Method: "GetRecommend", Body: "user=alice"}
	fmt.Println("  场景 1: 服务正常 → 返回真实数据")
	resp, _ := degradation(context.Background(), req, successHandler)
	fmt.Printf("  结果: %s\n", resp.Message)
	fmt.Println()

	fmt.Println("  场景 2: 服务故障 → 返回降级数据")
	resp, _ = degradation(context.Background(), req, alwaysFailHandler)
	fmt.Printf("  结果: %s\n", resp.Message)
	fmt.Println()

	fmt.Println("  场景 3: 降级函数也失败（极端情况）")
	badDeg := GracefulDegradationInterceptor(
		func(_ context.Context, _ *Request, _ error) (*Response, error) {
			return nil, errors.New("降级服务也不可用")
		})
	_, err := badDeg(context.Background(), req, alwaysFailHandler)
	fmt.Printf("  结果: 失败 - %v\n", err)
	fmt.Println()
}

// demoMiddlewareComposition 演示多中间件组合
func demoMiddlewareComposition() {
	fmt.Println("--- 演示 5: 多中间件组合 ---")
	fmt.Println("  组合: 追踪 → 重试 → 断路器 → 降级 → 处理器")
	retryConf := RetryConfig{MaxRetries: 2, BaseDelay: 5 * time.Millisecond, MaxDelay: 20 * time.Millisecond}
	fallback := func(_ context.Context, req *Request, _ error) (*Response, error) {
		return &Response{Code: 200, Message: fmt.Sprintf("[降级] %s 缓存结果", req.Method)}, nil
	}
	req := &Request{Method: "CompositeCall", Body: "data=hello"}
	makeChain := func() Interceptor {
		cb := NewCircuitBreaker(5, 2, 100*time.Millisecond)
		return ChainInterceptors(TracingInterceptor, RetryInterceptor(retryConf),
			CircuitBreakerInterceptor(cb), GracefulDegradationInterceptor(fallback))
	}

	fmt.Println("  场景 1: 正常请求")
	resp, err := makeChain()(context.Background(), req, successHandler)
	if err != nil {
		fmt.Printf("  结果: 失败 - %v\n", err)
	} else {
		fmt.Printf("  结果: %s\n", resp.Message)
	}
	fmt.Println()

	fmt.Println("  场景 2: 服务故障 → 重试后降级")
	resp, err = makeChain()(context.Background(), req, alwaysFailHandler)
	if err != nil {
		fmt.Printf("  结果: 失败 - %v\n", err)
	} else {
		fmt.Printf("  结果: %s\n", resp.Message)
	}
	fmt.Println()
}

// demoCircuitBreakerStats 演示熔断统计与阈值追踪
func demoCircuitBreakerStats() {
	fmt.Println("--- 演示 6: 熔断统计与阈值追踪 ---")
	fmt.Println("  配置: 失败阈值=3, 恢复阈值=2, 超时=80ms")
	cb := NewCircuitBreaker(3, 2, 80*time.Millisecond)
	cbInterceptor := CircuitBreakerInterceptor(cb)
	req := &Request{Method: "HealthCheck", Body: ""}
	ctx := context.Background()
	pattern := []bool{false, false, true, true, true, true, false, false} // ✓✓✗✗✗(熔断)...✓✓
	handler := unreliableHandler(pattern)
	printStats := func(label string) {
		state, total, failures := cb.Stats()
		rate := 0.0
		if total > 0 {
			rate = float64(failures) / float64(total) * 100
		}
		fmt.Printf("  [统计] %s | 状态=%s | 请求=%d | 失败=%d | 失败率=%.1f%%\n",
			label, state, total, failures, rate)
	}

	fmt.Println("  发送请求序列 (✓=成功, ✗=失败):")
	for i := 0; i < len(pattern); i++ {
		state, _, _ := cb.Stats()
		if state == StateOpen {
			fmt.Println()
			printStats(fmt.Sprintf("请求 #%d 前", i+1))
			fmt.Println("  等待断路器超时...")
			time.Sleep(90 * time.Millisecond)
		}
		resp, err := cbInterceptor(ctx, req, handler)
		if err != nil {
			fmt.Printf("    请求 #%02d: ✗ %v\n", i+1, err)
		} else {
			fmt.Printf("    请求 #%02d: ✓ %s\n", i+1, resp.Message)
		}
	}
	fmt.Println()
	printStats("最终")
	fmt.Println()
}
