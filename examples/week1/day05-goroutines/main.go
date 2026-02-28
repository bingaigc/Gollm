// Day 05 - Goroutine 并发编程
// 本示例演示 Go 的并发核心机制：
// - 使用 goroutine 并发执行任务
// - 使用 sync.WaitGroup 等待所有 goroutine 完成
// - 使用 channel 收集并发结果
// - 模拟 errgroup.Group 的错误传播模式（标准库实现）
// - 使用 sync.Mutex 保护共享状态
// - 通过计时信息展示并发的性能优势
//
// 注意：由于沙箱环境无法真正发起 HTTP 请求，
// 本示例使用 time.Sleep 模拟网络延迟，返回模拟数据。

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// =============================
// 数据结构定义
// =============================

// FetchResult 表示一次网页抓取的结果
type FetchResult struct {
	// URL 是目标网址
	URL string
	// Title 是抓取到的页面标题
	Title string
	// Duration 是本次抓取耗时
	Duration time.Duration
	// Err 是抓取过程中遇到的错误（如果有）
	Err error
}

// PageStats 是需要 Mutex 保护的共享统计数据
type PageStats struct {
	// mu 保护下面所有字段的并发访问
	mu sync.Mutex
	// TotalBytes 累计抓取字节数（模拟）
	TotalBytes int
	// SuccessCount 成功抓取的页面数
	SuccessCount int
	// FailCount 失败的页面数
	FailCount int
}

// =============================
// 模拟 HTTP 抓取函数
// =============================

// mockFetchTitle 模拟 HTTP 请求抓取网页标题
// 使用 time.Sleep 模拟网络延迟（100ms-500ms）
func mockFetchTitle(url string) (string, int, error) {
	// 模拟网络延迟：随机 100ms 到 500ms
	delay := time.Duration(100+rand.Intn(400)) * time.Millisecond
	time.Sleep(delay)

	// 模拟不同 URL 返回不同的标题和大小
	mockData := map[string]struct {
		title string
		size  int
	}{
		"https://httpbin.org/html":       {"httpbin - HTML 测试页面", 3200},
		"https://httpbin.org/json":       {"httpbin - JSON 响应示例", 1500},
		"https://httpbin.org/xml":        {"httpbin - XML 响应示例", 2800},
		"https://httpbin.org/robots.txt": {"httpbin - Robots.txt", 450},
		"https://httpbin.org/forms/post": {"httpbin - 表单提交测试", 5100},
		"https://example.com":           {"Example Domain", 1256},
	}

	if data, ok := mockData[url]; ok {
		return data.title, data.size, nil
	}
	// 未知 URL 返回错误
	return "", 0, fmt.Errorf("无法连接到 %s", url)
}

// =============================
// 演示 1：基础 Goroutine + WaitGroup
// =============================

// demonstrateWaitGroup 展示 sync.WaitGroup 的基本用法
// WaitGroup 用于等待一组 goroutine 全部完成
func demonstrateWaitGroup() {
	fmt.Println("=== 基础 Goroutine + WaitGroup ===")

	urls := []string{
		"https://httpbin.org/html",
		"https://httpbin.org/json",
		"https://httpbin.org/xml",
	}

	// 创建 WaitGroup，用于等待所有 goroutine 完成
	var wg sync.WaitGroup

	fmt.Printf("  即将并发抓取 %d 个页面...\n", len(urls))
	start := time.Now()

	for _, url := range urls {
		// 每启动一个 goroutine 前，计数器加 1
		wg.Add(1)

		// 启动 goroutine；注意：将 url 作为参数传入，
		// 避免闭包捕获循环变量导致的竞态问题
		go func(u string) {
			// goroutine 结束时，计数器减 1
			defer wg.Done()

			title, _, err := mockFetchTitle(u)
			if err != nil {
				fmt.Printf("  ❌ %s: %v\n", u, err)
				return
			}
			fmt.Printf("  ✅ %s → %q\n", u, title)
		}(url)
	}

	// 阻塞等待所有 goroutine 完成（计数器归零）
	wg.Wait()

	elapsed := time.Since(start)
	fmt.Printf("  ⏱  并发完成，总耗时: %v（远小于串行耗时）\n\n", elapsed)
}

// =============================
// 演示 2：Channel 收集结果
// =============================

// demonstrateChannelResults 展示使用 channel 从多个 goroutine 收集结果
// 这是 Go 并发的惯用模式：goroutine 通过 channel 返回结果
func demonstrateChannelResults() {
	fmt.Println("=== Channel 收集并发结果 ===")

	urls := []string{
		"https://httpbin.org/html",
		"https://httpbin.org/json",
		"https://httpbin.org/xml",
		"https://httpbin.org/robots.txt",
		"https://unknown-host.test/page",
	}

	// 创建带缓冲的 channel，容量等于 URL 数量
	// 缓冲 channel 允许发送方无需等待接收方即可写入
	results := make(chan FetchResult, len(urls))

	start := time.Now()

	// 启动所有抓取 goroutine
	for _, url := range urls {
		go func(u string) {
			fetchStart := time.Now()
			title, _, err := mockFetchTitle(u)
			results <- FetchResult{
				URL:      u,
				Title:    title,
				Duration: time.Since(fetchStart),
				Err:      err,
			}
		}(url)
	}

	// 从 channel 接收所有结果
	// 已知 goroutine 数量，所以用 for-range 计数接收
	fmt.Println("  抓取结果：")
	for i := 0; i < len(urls); i++ {
		r := <-results
		if r.Err != nil {
			fmt.Printf("  ❌ [%v] %s: %v\n", r.Duration, r.URL, r.Err)
		} else {
			fmt.Printf("  ✅ [%v] %s → %q\n", r.Duration, r.URL, r.Title)
		}
	}

	fmt.Printf("  ⏱  总耗时: %v\n\n", time.Since(start))
}

// =============================
// 演示 3：模拟 errgroup 的错误传播模式
// =============================
// errgroup.Group 来自 golang.org/x/sync/errgroup（非标准库）
// 这里用标准库 channel + goroutine 模拟其核心行为：
// - 并发执行一组任务
// - 任一任务出错时，收集第一个错误
// - 等待所有任务完成后返回错误

// simpleErrGroup 是用标准库模拟的简化版 errgroup
type simpleErrGroup struct {
	wg      sync.WaitGroup
	errOnce sync.Once
	err     error
}

// Go 方法启动一个 goroutine 执行 f，如果 f 返回错误则记录
func (g *simpleErrGroup) Go(f func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := f(); err != nil {
			// sync.Once 保证只记录第一个错误
			g.errOnce.Do(func() {
				g.err = err
			})
		}
	}()
}

// Wait 等待所有 goroutine 完成并返回第一个错误（如果有）
func (g *simpleErrGroup) Wait() error {
	g.wg.Wait()
	return g.err
}

// demonstrateErrGroup 展示 errgroup 模式的错误传播
func demonstrateErrGroup() {
	fmt.Println("=== 模拟 errgroup 错误传播 ===")

	urls := []string{
		"https://httpbin.org/html",
		"https://unknown-host.test/fail", // 这个 URL 会返回错误
		"https://httpbin.org/json",
	}

	var g simpleErrGroup

	for _, url := range urls {
		u := url // 捕获循环变量
		g.Go(func() error {
			title, _, err := mockFetchTitle(u)
			if err != nil {
				return fmt.Errorf("抓取 %s 失败: %w", u, err)
			}
			fmt.Printf("  ✅ %s → %q\n", u, title)
			return nil
		})
	}

	// Wait 返回第一个遇到的错误
	if err := g.Wait(); err != nil {
		fmt.Printf("  ⚠️  errgroup 捕获到错误: %v\n", err)
	} else {
		fmt.Println("  ✅ 所有任务成功完成")
	}
	fmt.Println()
}

// =============================
// 演示 4：Mutex 保护共享状态
// =============================

// demonstrateMutex 展示使用 sync.Mutex 保护并发写入的共享数据
// 不使用 Mutex 时，多个 goroutine 同时读写同一变量会导致数据竞争（race condition）
func demonstrateMutex() {
	fmt.Println("=== Mutex 保护共享状态 ===")

	urls := []string{
		"https://httpbin.org/html",
		"https://httpbin.org/json",
		"https://httpbin.org/xml",
		"https://httpbin.org/robots.txt",
		"https://httpbin.org/forms/post",
		"https://unknown-host.test/page",
	}

	// stats 是所有 goroutine 共享的统计数据
	stats := &PageStats{}
	var wg sync.WaitGroup

	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()

			_, size, err := mockFetchTitle(u)

			// 使用 Mutex 加锁保护共享状态的写入
			// Lock() 和 Unlock() 之间的代码同一时刻只有一个 goroutine 可以执行
			stats.mu.Lock()
			if err != nil {
				stats.FailCount++
			} else {
				stats.SuccessCount++
				stats.TotalBytes += size
			}
			stats.mu.Unlock()
			// 提示：也可以使用 defer stats.mu.Unlock()
			// 但在临界区很短时，显式 Unlock 更清晰
		}(url)
	}

	wg.Wait()

	// 此时所有 goroutine 已完成，无需加锁即可安全读取
	fmt.Printf("  📊 统计结果:\n")
	fmt.Printf("     成功: %d 个页面\n", stats.SuccessCount)
	fmt.Printf("     失败: %d 个页面\n", stats.FailCount)
	fmt.Printf("     总字节: %d bytes\n", stats.TotalBytes)
	fmt.Println()
}

// =============================
// 演示 5：串行 vs 并发性能对比
// =============================

// demonstrateTimingComparison 对比串行和并发执行的耗时差异
func demonstrateTimingComparison() {
	fmt.Println("=== 串行 vs 并发 性能对比 ===")

	urls := []string{
		"https://httpbin.org/html",
		"https://httpbin.org/json",
		"https://httpbin.org/xml",
		"https://httpbin.org/robots.txt",
		"https://example.com",
	}

	// 串行执行
	serialStart := time.Now()
	for _, url := range urls {
		mockFetchTitle(url)
	}
	serialDuration := time.Since(serialStart)

	// 并发执行
	concurrentStart := time.Now()
	var wg sync.WaitGroup
	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			mockFetchTitle(u)
		}(url)
	}
	wg.Wait()
	concurrentDuration := time.Since(concurrentStart)

	fmt.Printf("  🐢 串行耗时: %v\n", serialDuration)
	fmt.Printf("  🚀 并发耗时: %v\n", concurrentDuration)
	if serialDuration > 0 {
		speedup := float64(serialDuration) / float64(concurrentDuration)
		fmt.Printf("  ⚡ 加速比: %.1fx\n", speedup)
	}
	fmt.Println()
}

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║   Day 05 - Goroutine 并发编程        ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// 1. 基础 WaitGroup 用法
	demonstrateWaitGroup()

	// 2. Channel 收集结果
	demonstrateChannelResults()

	// 3. errgroup 错误传播模式
	demonstrateErrGroup()

	// 4. Mutex 保护共享状态
	demonstrateMutex()

	// 5. 串行 vs 并发 性能对比
	demonstrateTimingComparison()

	fmt.Println("📚 小结：goroutine 是 Go 并发的基石；WaitGroup 用于等待、Channel 用于通信、Mutex 用于保护共享状态！")
}
