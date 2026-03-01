// Day 12 - gRPC 拦截器模式
// 本示例演示 gRPC 拦截器（Interceptor）的核心概念
// 使用标准库 net/http 中间件模式模拟
//
// ============================================================
// gRPC 拦截器说明：
//
// 一元拦截器（UnaryInterceptor）：
//   type UnaryServerInterceptor func(
//       ctx context.Context,
//       req interface{},
//       info *UnaryServerInfo,
//       handler UnaryHandler,
//   ) (interface{}, error)
//
// 流式拦截器（StreamInterceptor）：
//   type StreamServerInterceptor func(
//       srv interface{},
//       ss ServerStream,
//       info *StreamServerInfo,
//       handler StreamHandler,
//   ) error
//
// 注册拦截器：
//   server := grpc.NewServer(
//       grpc.ChainUnaryInterceptor(
//           loggingInterceptor,
//           authInterceptor,
//           rateLimitInterceptor,
//       ),
//   )
//
// 拦截器执行顺序（洋葱模型）：
//   请求 → 日志 → 认证 → 限流 → 处理器 → 限流 → 认证 → 日志 → 响应
// ============================================================

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// ============================================================
// 核心类型定义
// ============================================================

// Request 统一请求结构（模拟 gRPC 的 proto 消息）
type Request struct {
	Method string `json:"method"` // RPC 方法名
	Body   string `json:"body"`   // 请求内容
}

// Response 统一响应结构
type Response struct {
	Code    int    `json:"code"`    // 状态码（模拟 gRPC status code）
	Message string `json:"message"` // 响应消息
}

// Handler 处理器函数类型
// 对应 gRPC 的 UnaryHandler：
//
//	type UnaryHandler func(ctx context.Context, req interface{}) (interface{}, error)
type Handler func(ctx context.Context, req *Request) (*Response, error)

// Interceptor 拦截器函数类型
// 对应 gRPC 的 UnaryServerInterceptor
// 接收上下文、请求和下一个处理器，返回响应
type Interceptor func(ctx context.Context, req *Request, next Handler) (*Response, error)

// ============================================================
// 上下文键定义
// 使用自定义类型避免键冲突（Go 最佳实践）
// ============================================================

type contextKey string

const (
	// 存储用户 ID 的上下文键
	ctxKeyUserID contextKey = "user_id"
	// 存储请求 ID 的上下文键
	ctxKeyRequestID contextKey = "request_id"
)

// ============================================================
// 拦截器实现
// ============================================================

// LoggingInterceptor 日志拦截器
// 记录每个请求的方法、耗时和结果
// 对应 gRPC 中常见的日志中间件，如 grpc_zap、grpc_logrus
func LoggingInterceptor(ctx context.Context, req *Request, next Handler) (*Response, error) {
	start := time.Now()

	// 生成请求 ID（简化版，实际用 UUID）
	requestID := fmt.Sprintf("req-%d", start.UnixNano()%100000)
	ctx = context.WithValue(ctx, ctxKeyRequestID, requestID)

	log.Printf("[日志] → 请求开始 | ID: %s | 方法: %s", requestID, req.Method)

	// 调用下一个处理器（核心的 next 调用模式）
	resp, err := next(ctx, req)

	// 计算耗时
	duration := time.Since(start)

	if err != nil {
		log.Printf("[日志] ← 请求失败 | ID: %s | 耗时: %v | 错误: %v",
			requestID, duration, err)
	} else {
		log.Printf("[日志] ← 请求完成 | ID: %s | 耗时: %v | 状态码: %d",
			requestID, duration, resp.Code)
	}

	return resp, err
}

// AuthInterceptor 认证拦截器
// 检查请求中的认证令牌
// 对应 gRPC 中的 metadata 认证：
//
//	md, ok := metadata.FromIncomingContext(ctx)
//	token := md.Get("authorization")
//
// 在 gRPC 中，客户端通过 metadata 发送令牌：
//
//	md := metadata.Pairs("authorization", "Bearer xxx")
//	ctx := metadata.NewOutgoingContext(ctx, md)
func AuthInterceptor(ctx context.Context, req *Request, next Handler) (*Response, error) {
	// 从上下文获取令牌（模拟从 gRPC metadata 获取）
	// 这里简化处理，实际会从 HTTP 头或 gRPC metadata 读取
	token, _ := ctx.Value(contextKey("auth_token")).(string)

	log.Printf("[认证] 检查令牌: %q", token)

	// 验证令牌（简化版，实际会调用 JWT 验证或 OAuth 服务）
	if token == "" {
		// gRPC 中返回 codes.Unauthenticated
		return &Response{
			Code:    401,
			Message: "未提供认证令牌",
		}, nil
	}

	if token != "valid-token-123" {
		// gRPC 中返回 codes.PermissionDenied
		return &Response{
			Code:    403,
			Message: "认证令牌无效",
		}, nil
	}

	// 将用户 ID 存入上下文（后续处理器可使用）
	ctx = context.WithValue(ctx, ctxKeyUserID, "user-42")
	log.Printf("[认证] 认证成功，用户: user-42")

	return next(ctx, req)
}

// ============================================================
// 令牌桶限流器
// ============================================================

// TokenBucket 令牌桶限流器
// 以固定速率向桶中添加令牌，每次请求消耗一个令牌
// 桶满时多余的令牌被丢弃，桶空时请求被拒绝
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64   // 当前令牌数
	maxTokens  float64   // 桶的最大容量
	refillRate float64   // 每秒补充的令牌数
	lastRefill time.Time // 上次补充时间
}

// NewTokenBucket 创建令牌桶
// rate: 每秒允许的请求数，burst: 最大突发请求数
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// Allow 尝试获取一个令牌
// 返回 true 表示允许请求，false 表示被限流
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// 计算需要补充的令牌数
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	// 尝试消耗一个令牌
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	return false
}

// RateLimitInterceptor 创建限流拦截器
// 对应 gRPC 中的限流中间件，如 grpc_ratelimit
// rate: 每秒允许的请求数，burst: 最大突发数
func RateLimitInterceptor(rate float64, burst int) Interceptor {
	bucket := NewTokenBucket(rate, burst)

	return func(ctx context.Context, req *Request, next Handler) (*Response, error) {
		if !bucket.Allow() {
			log.Printf("[限流] 请求被拒绝: %s（超出速率限制）", req.Method)
			// gRPC 中返回 codes.ResourceExhausted
			return &Response{
				Code:    429,
				Message: "请求过于频繁，请稍后重试",
			}, nil
		}
		log.Printf("[限流] 请求通过: %s", req.Method)
		return next(ctx, req)
	}
}

// ============================================================
// 拦截器链（Chain）
// ============================================================

// ChainInterceptors 将多个拦截器串联成处理链
// 执行顺序与参数顺序一致（第一个拦截器最先执行）
// 对应 gRPC 的 grpc.ChainUnaryInterceptor(...)
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return func(ctx context.Context, req *Request, finalHandler Handler) (*Response, error) {
		// 从最后一个拦截器开始，逐层包装
		// 构建洋葱模型：最外层的拦截器最先执行
		handler := finalHandler
		for i := len(interceptors) - 1; i >= 0; i-- {
			// 捕获当前拦截器（避免闭包变量问题）
			interceptor := interceptors[i]
			currentHandler := handler
			handler = func(ctx context.Context, req *Request) (*Response, error) {
				return interceptor(ctx, req, currentHandler)
			}
		}
		return handler(ctx, req)
	}
}

// ============================================================
// 业务处理器
// ============================================================

// chatHandler 实际的业务处理函数
// 在 gRPC 中，这是服务实现方法的核心逻辑
func chatHandler(ctx context.Context, req *Request) (*Response, error) {
	// 从上下文中获取用户 ID（由认证拦截器设置）
	userID, _ := ctx.Value(ctxKeyUserID).(string)
	requestID, _ := ctx.Value(ctxKeyRequestID).(string)

	log.Printf("[处理器] 处理请求 | 用户: %s | 请求ID: %s | 内容: %s",
		userID, requestID, req.Body)

	// 模拟处理时间
	time.Sleep(50 * time.Millisecond)

	return &Response{
		Code:    200,
		Message: fmt.Sprintf("你好 %s，已处理您的请求：%s", userID, req.Body),
	}, nil
}

// ============================================================
// HTTP 适配器（将拦截器链暴露为 HTTP 端点）
// ============================================================

// makeHTTPHandler 将拦截器链包装为 HTTP 处理函数
func makeHTTPHandler(chain Interceptor, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 解析请求
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求解析失败", http.StatusBadRequest)
			return
		}

		// 从 HTTP 头中提取认证令牌，注入上下文
		// 模拟 gRPC metadata 传递
		ctx := r.Context()
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			ctx = context.WithValue(ctx, contextKey("auth_token"), authHeader)
		}

		// 执行拦截器链 + 处理器
		resp, err := chain(ctx, &req, handler)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ============================================================
// 主函数
// ============================================================

func main() {
	fmt.Println("=== Day 12: gRPC 拦截器模式 ===")
	fmt.Println()

	// ----------------------------------------------------------
	// 1. 构建拦截器链
	// ----------------------------------------------------------
	fmt.Println("--- 构建拦截器链 ---")
	fmt.Println("  顺序: 日志 → 认证 → 限流 → 处理器")
	fmt.Println()

	// 创建限流拦截器：每秒 5 个请求，最大突发 3 个
	rateLimiter := RateLimitInterceptor(5.0, 3)

	// 将拦截器串联起来（对应 gRPC 的 ChainUnaryInterceptor）
	chain := ChainInterceptors(
		LoggingInterceptor,  // 最外层：记录日志和耗时
		AuthInterceptor,     // 第二层：验证认证
		rateLimiter,         // 第三层：限制请求速率
	)

	// ----------------------------------------------------------
	// 2. 不使用 HTTP 的纯函数调用演示
	// ----------------------------------------------------------
	fmt.Println("--- 演示 1: 无令牌请求（认证失败）---")

	req := &Request{Method: "Chat", Body: "你好"}
	ctx := context.Background()
	resp, err := chain(ctx, req, chatHandler)
	if err != nil {
		fmt.Printf("  错误: %v\n", err)
	} else {
		fmt.Printf("  状态码: %d, 消息: %s\n", resp.Code, resp.Message)
	}
	fmt.Println()

	fmt.Println("--- 演示 2: 无效令牌请求（认证失败）---")

	ctx = context.WithValue(context.Background(), contextKey("auth_token"), "wrong-token")
	resp, err = chain(ctx, req, chatHandler)
	if err != nil {
		fmt.Printf("  错误: %v\n", err)
	} else {
		fmt.Printf("  状态码: %d, 消息: %s\n", resp.Code, resp.Message)
	}
	fmt.Println()

	fmt.Println("--- 演示 3: 有效令牌请求（认证成功）---")

	ctx = context.WithValue(context.Background(), contextKey("auth_token"), "valid-token-123")
	resp, err = chain(ctx, req, chatHandler)
	if err != nil {
		fmt.Printf("  错误: %v\n", err)
	} else {
		fmt.Printf("  状态码: %d, 消息: %s\n", resp.Code, resp.Message)
	}
	fmt.Println()

	// ----------------------------------------------------------
	// 3. HTTP 服务器演示
	// ----------------------------------------------------------
	fmt.Println("--- 演示 4: HTTP 服务器中使用拦截器链 ---")

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", makeHTTPHandler(chain, chatHandler))

	// 动态分配端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()
	defer server.Close()

	time.Sleep(50 * time.Millisecond) // 等待服务器就绪

	// 发送带认证头的请求
	fmt.Println("  发送带认证头的 HTTP 请求...")
	body := `{"method":"Chat","body":"通过 HTTP 调用"}`
	httpReq, _ := http.NewRequest(http.MethodPost, baseURL+"/rpc",
		jsonReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "valid-token-123")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fmt.Printf("  HTTP 请求失败: %v\n", err)
	} else {
		var result Response
		json.NewDecoder(httpResp.Body).Decode(&result)
		httpResp.Body.Close()
		fmt.Printf("  HTTP 响应: 状态码=%d, 消息=%s\n", result.Code, result.Message)
	}
	fmt.Println()

	// ----------------------------------------------------------
	// 4. 限流演示
	// ----------------------------------------------------------
	fmt.Println("--- 演示 5: 限流效果 ---")
	fmt.Println("  快速发送 6 个请求（令牌桶容量 3，速率 5/秒）...")

	// 使用新的拦截器链（仅限流，跳过认证以简化演示）
	rateLimitOnly := ChainInterceptors(rateLimiter)
	simpleHandler := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Code: 200, Message: "OK"}, nil
	}

	for i := 0; i < 6; i++ {
		r := &Request{Method: "Chat", Body: fmt.Sprintf("请求 %d", i)}
		resp, _ := rateLimitOnly(context.Background(), r, simpleHandler)
		fmt.Printf("  请求 %d: 状态码=%d, 消息=%s\n", i, resp.Code, resp.Message)
	}
	fmt.Println()

	// ----------------------------------------------------------
	// 总结
	// ----------------------------------------------------------
	fmt.Println("=== 总结 ===")
	fmt.Println("1. 拦截器（Interceptor）= 中间件（Middleware），用于横切关注点")
	fmt.Println("2. 常见拦截器：日志、认证、限流、链路追踪、错误恢复")
	fmt.Println("3. 拦截器通过 next(ctx, req) 调用下一层，形成洋葱模型")
	fmt.Println("4. gRPC 使用 ChainUnaryInterceptor 注册多个拦截器")
	fmt.Println("5. 上下文（context）用于在拦截器之间传递数据")
	fmt.Println("6. 令牌桶算法是常用的限流策略，支持突发流量")
}

// jsonReader 创建包含 JSON 字符串的 io.Reader
func jsonReader(s string) *jsonStringReader {
	return &jsonStringReader{data: []byte(s)}
}

type jsonStringReader struct {
	data []byte
	pos  int
}

func (r *jsonStringReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
