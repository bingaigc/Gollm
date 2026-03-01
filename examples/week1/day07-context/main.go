// Day 07 - Context 深入详解
// 本示例演示 Go context 包的核心用法：
// - context.WithTimeout：设置自动超时取消
// - context.WithCancel：手动取消传播
// - context.WithValue：传递请求级数据（如 Trace ID）
// - Context 树：父 context 取消会级联到所有子 context
// - 模拟微服务调用链中的超时传播
// - 模拟带 context 超时控制的 HTTP 请求
// - 打印 context 树的传播过程

package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// =============================
// 演示 1：context.WithTimeout
// =============================

// simulateDBQuery 模拟一个数据库查询操作
// 如果 context 在查询完成前被取消或超时，查询会立即中止
func simulateDBQuery(ctx context.Context, query string, delay time.Duration) (string, error) {
	fmt.Printf("    🔍 开始查询: %s（预计耗时 %v）\n", query, delay)

	select {
	case <-time.After(delay):
		// 查询在超时前完成
		return fmt.Sprintf("查询结果: [%s] 共 42 条记录", query), nil
	case <-ctx.Done():
		// context 被取消或超时
		// ctx.Err() 返回取消原因：DeadlineExceeded 或 Canceled
		return "", fmt.Errorf("查询 %q 被中止: %w", query, ctx.Err())
	}
}

// demonstrateWithTimeout 展示 context.WithTimeout 的用法
// WithTimeout 创建一个在指定时间后自动取消的 context
func demonstrateWithTimeout() {
	fmt.Println("=== context.WithTimeout 自动超时 ===")

	// 创建一个 3 秒后自动取消的 context
	// cancel 函数用于提前取消（最佳实践：总是 defer cancel）
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel() // 确保资源释放，即使操作提前完成

	fmt.Println("  超时设置: 3 秒")
	fmt.Println()

	// 场景 1：快速查询（在超时前完成）
	fmt.Println("  场景 1 - 快速查询（1s < 3s 超时）:")
	result, err := simulateDBQuery(ctx, "SELECT * FROM users LIMIT 10", 1*time.Second)
	if err != nil {
		fmt.Printf("    ❌ %v\n", err)
	} else {
		fmt.Printf("    ✅ %s\n", result)
	}
	fmt.Println()

	// 场景 2：慢速查询（超过超时时间）
	// 注意：上面已经消耗了约 1 秒，剩余超时约 2 秒
	fmt.Println("  场景 2 - 慢速查询（5s > 剩余超时）:")
	result, err = simulateDBQuery(ctx, "SELECT * FROM logs", 5*time.Second)
	if err != nil {
		fmt.Printf("    ❌ %v\n", err)
	} else {
		fmt.Printf("    ✅ %s\n", result)
	}
	fmt.Println()
}

// =============================
// 演示 2：context.WithCancel
// =============================

// longRunningTask 模拟一个可取消的长时间运行任务
func longRunningTask(ctx context.Context, name string, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	step := 0
	for {
		select {
		case <-ctx.Done():
			// 收到取消信号
			fmt.Printf("    🛑 任务 %q 在第 %d 步被取消: %v\n", name, step, ctx.Err())
			return
		case <-ticker.C:
			step++
			fmt.Printf("    ⚙️  任务 %q 执行第 %d 步...\n", name, step)
		}
	}
}

// demonstrateWithCancel 展示 context.WithCancel 的手动取消传播
// WithCancel 创建一个可以通过调用 cancel() 函数手动取消的 context
func demonstrateWithCancel() {
	fmt.Println("=== context.WithCancel 手动取消 ===")

	// 创建可手动取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	// 启动两个长时间运行的任务
	wg.Add(2)
	go longRunningTask(ctx, "数据同步", &wg)
	go longRunningTask(ctx, "日志收集", &wg)

	// 让任务运行一段时间
	time.Sleep(500 * time.Millisecond)

	// 手动取消：调用 cancel() 会使两个任务的 ctx.Done() 立即触发
	fmt.Println("  📢 调用 cancel()，取消所有任务...")
	cancel()

	// 等待所有任务确认退出
	wg.Wait()
	fmt.Println("  ✅ 所有任务已安全退出")
	fmt.Println()
}

// =============================
// 演示 3：context.WithValue
// =============================

// 定义自定义 key 类型，避免与其他包的 key 冲突
// Go 官方建议使用未导出的类型作为 context key
type contextKey string

const (
	// traceIDKey 用于在 context 中存储追踪 ID
	traceIDKey contextKey = "traceID"
	// userIDKey 用于在 context 中存储用户 ID
	userIDKey contextKey = "userID"
)

// getTraceID 从 context 中安全提取 Trace ID
func getTraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return "unknown"
}

// getUserID 从 context 中安全提取用户 ID
func getUserID(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return "anonymous"
}

// handleRequest 模拟一个 HTTP 请求处理函数
func handleRequest(ctx context.Context) {
	traceID := getTraceID(ctx)
	userID := getUserID(ctx)
	fmt.Printf("    📋 处理请求 [TraceID=%s, UserID=%s]\n", traceID, userID)

	// 将 context 传递给下游函数
	queryDatabase(ctx)
	callExternalAPI(ctx)
}

// queryDatabase 模拟数据库查询，从 context 获取请求级数据
func queryDatabase(ctx context.Context) {
	traceID := getTraceID(ctx)
	fmt.Printf("    💾 数据库查询 [TraceID=%s]\n", traceID)
}

// callExternalAPI 模拟外部 API 调用，从 context 获取请求级数据
func callExternalAPI(ctx context.Context) {
	traceID := getTraceID(ctx)
	userID := getUserID(ctx)
	fmt.Printf("    🌐 外部 API 调用 [TraceID=%s, UserID=%s]\n", traceID, userID)
}

// demonstrateWithValue 展示 context.WithValue 传递请求级数据
// WithValue 用于在函数调用链中传递请求范围的数据，如 Trace ID、用户身份等
// 注意：不要滥用 WithValue 传递函数参数，它只适合请求级元数据
func demonstrateWithValue() {
	fmt.Println("=== context.WithValue 请求级数据 ===")

	// 逐层添加 value，形成 context 链
	ctx := context.Background()
	ctx = context.WithValue(ctx, traceIDKey, "abc-123-xyz")
	ctx = context.WithValue(ctx, userIDKey, "user-42")

	fmt.Println("  模拟处理一个 HTTP 请求:")
	handleRequest(ctx)

	fmt.Println()
	fmt.Println("  ⚠️  最佳实践：")
	fmt.Println("    - 只用 WithValue 传递请求级元数据（Trace ID、认证信息）")
	fmt.Println("    - 不要用它替代函数参数")
	fmt.Println("    - 使用自定义类型作为 key，避免冲突")
	fmt.Println()
}

// =============================
// 演示 4：Context 树 —— 父取消级联到子
// =============================

// demonstrateContextTree 展示 context 的树状结构和级联取消
// 当父 context 被取消时，所有子 context 也会自动取消
func demonstrateContextTree() {
	fmt.Println("=== Context 树：父取消级联到子 ===")

	// 构建 context 树：
	//     root (WithCancel)
	//     ├── child1 (WithTimeout 2s)
	//     │   └── grandchild1 (WithValue)
	//     └── child2 (WithCancel)

	// 创建根 context
	root, rootCancel := context.WithCancel(context.Background())

	// 创建子 context：child1 有自己的 2 秒超时
	child1, child1Cancel := context.WithTimeout(root, 2*time.Second)
	defer child1Cancel()

	// 创建孙 context：继承 child1 的超时，并附加 value
	grandchild1 := context.WithValue(child1, traceIDKey, "tree-trace-001")

	// 创建另一个子 context
	child2, child2Cancel := context.WithCancel(root)
	defer child2Cancel()

	fmt.Println("  Context 树结构：")
	fmt.Println("    root (WithCancel)")
	fmt.Println("    ├── child1 (WithTimeout 2s)")
	fmt.Println("    │   └── grandchild1 (WithValue: traceID)")
	fmt.Println("    └── child2 (WithCancel)")
	fmt.Println()

	// 启动 goroutine 监听每个 context
	var wg sync.WaitGroup
	watchContext := func(name string, ctx context.Context) {
		defer wg.Done()
		<-ctx.Done()
		fmt.Printf("    🔴 %s 被取消: %v\n", name, ctx.Err())
	}

	wg.Add(3)
	go watchContext("child1", child1)
	go watchContext("grandchild1", grandchild1)
	go watchContext("child2", child2)

	// 等待一下让所有 goroutine 启动
	time.Sleep(50 * time.Millisecond)

	// 取消根 context —— 所有子孙 context 都会被级联取消
	fmt.Println("  📢 取消 root context...")
	rootCancel()

	// 等待所有监听 goroutine 确认取消
	wg.Wait()
	fmt.Println("  ✅ 所有子 context 已级联取消")
	fmt.Println()
}

// =============================
// 演示 5：微服务链路超时传播
// =============================

// microservice 模拟一个微服务节点
// 每个微服务接收 context 并传递给下游，超时会沿链路传播
func microservice(ctx context.Context, name string, processingTime time.Duration, next func(context.Context) error) error {
	fmt.Printf("    🔷 [%s] 开始处理（预计 %v）...\n", name, processingTime)

	select {
	case <-time.After(processingTime):
		fmt.Printf("    ✅ [%s] 处理完成\n", name)
		// 如果有下游服务，继续传播 context
		if next != nil {
			return next(ctx)
		}
		return nil
	case <-ctx.Done():
		fmt.Printf("    ❌ [%s] 被取消: %v\n", name, ctx.Err())
		return fmt.Errorf("服务 %s: %w", name, ctx.Err())
	}
}

// demonstrateMicroserviceChain 模拟微服务调用链中的超时传播
// API Gateway → 用户服务 → 订单服务 → 支付服务
// 总超时从入口传入，沿调用链自动传播
func demonstrateMicroserviceChain() {
	fmt.Println("=== 微服务链路超时传播 ===")

	// API Gateway 设置总超时为 1 秒
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	fmt.Println("  调用链: Gateway → 用户服务 → 订单服务 → 支付服务")
	fmt.Println("  总超时: 1 秒")
	fmt.Println()

	// 模拟调用链：每个服务处理需要一定时间
	// 用户服务(200ms) → 订单服务(300ms) → 支付服务(800ms)
	// 总计 1300ms > 1000ms 超时，支付服务会被超时取消
	err := microservice(ctx, "用户服务", 200*time.Millisecond, func(ctx context.Context) error {
		return microservice(ctx, "订单服务", 300*time.Millisecond, func(ctx context.Context) error {
			return microservice(ctx, "支付服务", 800*time.Millisecond, nil)
		})
	})

	if err != nil {
		fmt.Printf("  ⚠️  调用链失败: %v\n", err)
	} else {
		fmt.Println("  ✅ 调用链全部成功")
	}
	fmt.Println()
}

// =============================
// 演示 6：模拟 HTTP 请求的 Context 超时
// =============================

// mockHTTPGet 模拟一个 HTTP GET 请求
// 使用 context 控制请求超时
func mockHTTPGet(ctx context.Context, url string) (string, error) {
	// 模拟网络延迟：随机 200ms 到 1500ms
	delay := time.Duration(200+rand.Intn(1300)) * time.Millisecond
	fmt.Printf("    🌐 GET %s（模拟延迟 %v）\n", url, delay)

	select {
	case <-time.After(delay):
		return fmt.Sprintf("<html><title>%s 的页面</title></html>", url), nil
	case <-ctx.Done():
		return "", fmt.Errorf("请求 %s 超时: %w", url, ctx.Err())
	}
}

// demonstrateHTTPWithContext 展示使用 context 控制 HTTP 请求超时
func demonstrateHTTPWithContext() {
	fmt.Println("=== 模拟 HTTP 请求 + Context 超时 ===")

	urls := []string{
		"https://api.example.com/users",
		"https://api.example.com/orders",
		"https://api.example.com/products",
	}

	// 每个请求的超时为 800ms
	fmt.Println("  每个请求超时: 800ms")
	fmt.Println()

	for _, url := range urls {
		// 为每个请求创建独立的超时 context
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)

		body, err := mockHTTPGet(ctx, url)
		if err != nil {
			fmt.Printf("    ❌ %v\n", err)
		} else {
			// 只打印前 50 个字符
			if len(body) > 50 {
				body = body[:50] + "..."
			}
			fmt.Printf("    ✅ 响应: %s\n", body)
		}

		// 每次循环结束时取消 context 释放资源
		cancel()
	}
	fmt.Println()
}

// =============================
// 演示 7：Context 树传播过程可视化
// =============================

// demonstrateContextPropagation 可视化展示 context 树的传播过程
func demonstrateContextPropagation() {
	fmt.Println("=== Context 树传播过程 ===")

	// 创建一棵更复杂的 context 树并逐步观察取消传播
	fmt.Println("  构建 context 树：")
	fmt.Println("    Background")
	fmt.Println("    └── A (WithCancel)")
	fmt.Println("        ├── B (WithTimeout 500ms)")
	fmt.Println("        │   ├── D (WithValue: reqID=1)")
	fmt.Println("        │   └── E (WithValue: reqID=2)")
	fmt.Println("        └── C (WithTimeout 1s)")
	fmt.Println("            └── F (WithCancel)")
	fmt.Println()

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	ctxB, cancelB := context.WithTimeout(ctxA, 500*time.Millisecond)
	defer cancelB()

	ctxD := context.WithValue(ctxB, contextKey("reqID"), "1")
	ctxE := context.WithValue(ctxB, contextKey("reqID"), "2")

	ctxC, cancelC := context.WithTimeout(ctxA, 1*time.Second)
	defer cancelC()

	ctxF, cancelF := context.WithCancel(ctxC)
	defer cancelF()

	// 为每个节点设置监听
	var mu sync.Mutex
	events := make([]string, 0)

	record := func(name string, ctx context.Context) {
		<-ctx.Done()
		mu.Lock()
		events = append(events, fmt.Sprintf("%s 被取消 (%v)", name, ctx.Err()))
		mu.Unlock()
	}

	go record("B", ctxB)
	go record("D", ctxD)
	go record("E", ctxE)
	go record("C", ctxC)
	go record("F", ctxF)

	// 等待 goroutine 启动
	time.Sleep(50 * time.Millisecond)

	// 场景：让 B 的超时先触发（500ms），然后手动取消 A
	fmt.Println("  ⏳ 等待 B 的 500ms 超时...")
	time.Sleep(550 * time.Millisecond)

	mu.Lock()
	fmt.Println("  B 超时后的取消事件：")
	for _, e := range events {
		fmt.Printf("    🔴 %s\n", e)
	}
	countAfterB := len(events)
	mu.Unlock()

	fmt.Println()
	fmt.Println("  📢 取消 A（根节点），级联到 C 和 F...")
	cancelA()

	// 等待级联取消完成
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	fmt.Println("  A 取消后的新增事件：")
	for i := countAfterB; i < len(events); i++ {
		fmt.Printf("    🔴 %s\n", events[i])
	}
	mu.Unlock()
	fmt.Println()
}

func main() {
	fmt.Println("╔═══════════════════════════════════╗")
	fmt.Println("║   Day 07 - Context 深入详解       ║")
	fmt.Println("╚═══════════════════════════════════╝")
	fmt.Println()

	// 1. context.WithTimeout 自动超时
	demonstrateWithTimeout()

	// 2. context.WithCancel 手动取消
	demonstrateWithCancel()

	// 3. context.WithValue 请求级数据
	demonstrateWithValue()

	// 4. Context 树级联取消
	demonstrateContextTree()

	// 5. 微服务链路超时传播
	demonstrateMicroserviceChain()

	// 6. 模拟 HTTP 请求 + Context 超时
	demonstrateHTTPWithContext()

	// 7. Context 树传播过程可视化
	demonstrateContextPropagation()

	fmt.Println("📚 小结：Context 是 Go 请求生命周期管理的核心；超时、取消、Value 三大功能确保资源不泄漏、链路可追踪！")
}
