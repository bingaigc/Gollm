// Day 14 - 性能分析（pprof）
// 本示例演示如何使用 Go 内置的 pprof 工具进行性能分析
// pprof 可以分析 CPU 使用、内存分配、goroutine 状态等
//
// ============================================================
// pprof 使用方法：
//
// 1. 启动本程序后，访问以下端点：
//    - http://localhost:6060/debug/pprof/           概览页面
//    - http://localhost:6060/debug/pprof/profile    CPU 分析（默认30秒采样）
//    - http://localhost:6060/debug/pprof/heap        堆内存分析
//    - http://localhost:6060/debug/pprof/goroutine   goroutine 分析
//    - http://localhost:6060/debug/pprof/allocs      内存分配分析
//    - http://localhost:6060/debug/pprof/block       阻塞分析
//    - http://localhost:6060/debug/pprof/mutex       互斥锁分析
//
// 2. 命令行分析工具：
//    # CPU 分析（采样 10 秒）
//    go tool pprof http://localhost:6060/debug/pprof/profile?seconds=10
//
//    # 堆内存分析
//    go tool pprof http://localhost:6060/debug/pprof/heap
//
//    # goroutine 分析
//    go tool pprof http://localhost:6060/debug/pprof/goroutine
//
// 3. pprof 交互式命令：
//    (pprof) top          # 显示最耗资源的函数
//    (pprof) top -cum     # 按累计值排序
//    (pprof) list 函数名   # 查看函数级别的分析
//    (pprof) web           # 生成调用图（需要 graphviz）
//    (pprof) flame         # 生成火焰图
//
// 4. 生成可视化报告：
//    go tool pprof -http=:8080 profile.pb.gz
//
// ============================================================

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof" // 空导入：自动注册 /debug/pprof/ 路由
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"
)

// ============================================================
// CPU 密集型工作负载
// ============================================================

// cpuIntensiveSort 对大切片排序，产生 CPU 负载
// 排序是经典的 CPU 密集型操作，适合用 pprof 的 CPU profile 分析
func cpuIntensiveSort(size int) time.Duration {
	start := time.Now()

	// 生成随机数据
	data := make([]int, size)
	for i := range data {
		data[i] = rand.Intn(size * 10)
	}

	// 排序（标准库使用混合排序算法）
	sort.Ints(data)

	return time.Since(start)
}

// cpuIntensivePrimes 寻找素数，纯 CPU 计算
// 使用埃拉托斯特尼筛法（Sieve of Eratosthenes）
func cpuIntensivePrimes(limit int) (int, time.Duration) {
	start := time.Now()

	// 初始化布尔数组
	sieve := make([]bool, limit+1)
	for i := 2; i <= limit; i++ {
		sieve[i] = true
	}

	// 筛选
	for i := 2; i*i <= limit; i++ {
		if sieve[i] {
			for j := i * i; j <= limit; j += i {
				sieve[j] = false
			}
		}
	}

	// 计数
	count := 0
	for i := 2; i <= limit; i++ {
		if sieve[i] {
			count++
		}
	}

	return count, time.Since(start)
}

// ============================================================
// 内存密集型工作负载
// ============================================================

// memoryIntensiveMap 构建大型映射，产生堆内存分配
// 在 pprof 的 heap profile 中可以看到这个函数的内存使用
func memoryIntensiveMap(entries int) (int, time.Duration) {
	start := time.Now()

	// 创建大型 map（每个条目都会在堆上分配）
	data := make(map[string][]byte, entries)

	for i := 0; i < entries; i++ {
		key := fmt.Sprintf("key-%d", i)
		// 每个值分配 1KB 的数据
		value := make([]byte, 1024)
		for j := range value {
			value[j] = byte(i % 256)
		}
		data[key] = value
	}

	// 返回总大小（防止编译器优化掉分配）
	totalSize := 0
	for _, v := range data {
		totalSize += len(v)
	}

	return totalSize, time.Since(start)
}

// memoryIntensiveSlice 创建大量小切片，产生频繁的内存分配
// 在 pprof 的 allocs profile 中可以看到分配次数
func memoryIntensiveSlice(count int) time.Duration {
	start := time.Now()

	// 创建大量小切片（重点是分配次数而非总量）
	results := make([][]int, count)
	for i := 0; i < count; i++ {
		// 每次迭代分配一个新切片
		s := make([]int, 10)
		for j := range s {
			s[j] = i + j
		}
		results[i] = s
	}

	// 防止优化
	_ = results

	return time.Since(start)
}

// ============================================================
// Goroutine 工作负载
// ============================================================

// goroutineDemo 创建多个 goroutine，可在 pprof 的 goroutine profile 中观察
// 注意：受控地创建和回收 goroutine，避免真正的泄漏
func goroutineDemo(count int, lifetime time.Duration) {
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// 模拟 goroutine 工作（等待指定时间）
			// 在 pprof goroutine profile 中可以看到这些 goroutine
			time.Sleep(lifetime)
		}(i)
	}

	// 等待所有 goroutine 完成
	wg.Wait()
}

// goroutineLeakDemo 演示 goroutine "泄漏"场景（受控版本）
// 在实际代码中，忘记关闭 channel 或取消 context 会导致 goroutine 永远阻塞
// 这里使用 context 确保最终会被清理
func goroutineLeakDemo(count int, ctx context.Context) {
	for i := 0; i < count; i++ {
		go func(id int) {
			// 模拟等待一个永远不会到来的消息
			// 如果不使用 ctx.Done()，这个 goroutine 就会泄漏
			ch := make(chan struct{})
			select {
			case <-ch:
				// 永远不会执行（模拟泄漏）
			case <-ctx.Done():
				// 上下文取消时退出（安全网）
				return
			}
		}(i)
	}
}

// ============================================================
// 基准测试风格的性能对比函数
// ============================================================

// benchmark 运行给定函数多次并统计性能
// 类似于 Go testing 包的 Benchmark 功能
func benchmark(name string, iterations int, fn func()) {
	// 预热（让运行时有机会 JIT 优化和内存预分配）
	for i := 0; i < 3; i++ {
		fn()
	}

	// 记录起始内存状态
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	for i := 0; i < iterations; i++ {
		fn()
	}
	totalDuration := time.Since(start)

	// 记录结束内存状态
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	avgDuration := totalDuration / time.Duration(iterations)
	allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc

	fmt.Printf("  %-25s %d 次 | 总耗时: %-12v | 平均: %-10v | 分配: %s\n",
		name, iterations, totalDuration, avgDuration, formatBytes(allocBytes))
}

// formatBytes 格式化字节数为人类可读的字符串
func formatBytes(bytes uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ============================================================
// 运行时信息收集
// ============================================================

// printRuntimeStats 打印当前运行时统计信息
func printRuntimeStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Println("  --- 运行时统计 ---")
	fmt.Printf("  Goroutine 数量:    %d\n", runtime.NumGoroutine())
	fmt.Printf("  堆内存使用:        %s\n", formatBytes(m.HeapAlloc))
	fmt.Printf("  堆内存系统分配:    %s\n", formatBytes(m.HeapSys))
	fmt.Printf("  堆对象数量:        %d\n", m.HeapObjects)
	fmt.Printf("  累计分配内存:      %s\n", formatBytes(m.TotalAlloc))
	fmt.Printf("  GC 次数:           %d\n", m.NumGC)
	fmt.Printf("  CPU 核数:          %d\n", runtime.NumCPU())
	fmt.Printf("  GOMAXPROCS:        %d\n", runtime.GOMAXPROCS(0))
}

// ============================================================
// HTTP 处理器（模拟业务端点）
// ============================================================

// handleWorkload 触发工作负载的 HTTP 端点
func handleWorkload(w http.ResponseWriter, r *http.Request) {
	workType := r.URL.Query().Get("type")

	switch workType {
	case "cpu":
		// 触发 CPU 密集型工作
		duration := cpuIntensiveSort(100000)
		fmt.Fprintf(w, "CPU 排序完成，耗时: %v\n", duration)

	case "memory":
		// 触发内存密集型工作
		size, duration := memoryIntensiveMap(10000)
		fmt.Fprintf(w, "内存分配完成，大小: %s，耗时: %v\n",
			formatBytes(uint64(size)), duration)

	case "goroutine":
		// 触发 goroutine 创建
		goroutineDemo(100, 5*time.Second)
		fmt.Fprintf(w, "已创建并等待 100 个 goroutine 完成\n")

	default:
		fmt.Fprintf(w, "可用类型: cpu, memory, goroutine\n")
		fmt.Fprintf(w, "示例: /workload?type=cpu\n")
	}
}

// handleStats 返回运行时统计的 HTTP 端点
func handleStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintf(w, "=== 运行时统计 ===\n")
	fmt.Fprintf(w, "Goroutine 数量: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "堆内存使用: %s\n", formatBytes(m.HeapAlloc))
	fmt.Fprintf(w, "堆对象数量: %d\n", m.HeapObjects)
	fmt.Fprintf(w, "GC 次数: %d\n", m.NumGC)
}

// ============================================================
// 主函数
// ============================================================

func main() {
	fmt.Println("=== Day 14: 性能分析（pprof）===")
	fmt.Println()

	// ----------------------------------------------------------
	// 1. 打印初始运行时状态
	// ----------------------------------------------------------
	fmt.Println("--- 初始运行时状态 ---")
	printRuntimeStats()
	fmt.Println()

	// ----------------------------------------------------------
	// 2. CPU 密集型工作负载演示
	// ----------------------------------------------------------
	fmt.Println("--- CPU 密集型工作负载 ---")

	duration := cpuIntensiveSort(500000)
	fmt.Printf("  排序 500,000 个整数: %v\n", duration)

	primeCount, duration := cpuIntensivePrimes(1000000)
	fmt.Printf("  1,000,000 以内的素数: %d 个, 耗时: %v\n", primeCount, duration)
	fmt.Println()

	// ----------------------------------------------------------
	// 3. 内存密集型工作负载演示
	// ----------------------------------------------------------
	fmt.Println("--- 内存密集型工作负载 ---")

	totalSize, duration := memoryIntensiveMap(5000)
	fmt.Printf("  创建 5,000 个 1KB 条目的 map: %s, 耗时: %v\n",
		formatBytes(uint64(totalSize)), duration)

	duration = memoryIntensiveSlice(100000)
	fmt.Printf("  创建 100,000 个小切片: %v\n", duration)

	// 手动触发 GC 并查看效果
	fmt.Println("  执行 GC...")
	runtime.GC()
	printRuntimeStats()
	fmt.Println()

	// ----------------------------------------------------------
	// 4. Goroutine 工作负载演示
	// ----------------------------------------------------------
	fmt.Println("--- Goroutine 工作负载 ---")

	fmt.Printf("  当前 goroutine 数量: %d\n", runtime.NumGoroutine())

	// 创建受控的 "泄漏" goroutine（使用 context 确保清理）
	leakCtx, leakCancel := context.WithTimeout(context.Background(), 3*time.Second)
	goroutineLeakDemo(50, leakCtx)
	fmt.Printf("  创建 50 个阻塞 goroutine 后: %d\n", runtime.NumGoroutine())

	// 取消上下文，清理 goroutine
	leakCancel()
	time.Sleep(100 * time.Millisecond) // 等待 goroutine 退出
	fmt.Printf("  取消上下文后: %d\n", runtime.NumGoroutine())
	fmt.Println()

	// ----------------------------------------------------------
	// 5. 基准测试风格的性能对比
	// ----------------------------------------------------------
	fmt.Println("--- 基准测试 ---")

	benchmark("排序(10000元素)", 100, func() {
		cpuIntensiveSort(10000)
	})

	benchmark("排序(50000元素)", 20, func() {
		cpuIntensiveSort(50000)
	})

	benchmark("Map分配(1000条目)", 50, func() {
		memoryIntensiveMap(1000)
	})

	benchmark("切片分配(10000个)", 100, func() {
		memoryIntensiveSlice(10000)
	})
	fmt.Println()

	// ----------------------------------------------------------
	// 6. 启动 pprof HTTP 服务器
	// ----------------------------------------------------------
	fmt.Println("--- pprof HTTP 服务器 ---")

	// 注册业务端点
	http.HandleFunc("/workload", handleWorkload)
	http.HandleFunc("/stats", handleStats)
	// /debug/pprof/ 路由由 _ "net/http/pprof" 自动注册

	// 动态分配端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()

	fmt.Printf("  pprof 服务器已启动: http://127.0.0.1:%d\n", port)
	fmt.Println()
	fmt.Println("  可用端点:")
	fmt.Printf("    http://127.0.0.1:%d/debug/pprof/           概览页面\n", port)
	fmt.Printf("    http://127.0.0.1:%d/debug/pprof/profile    CPU 分析\n", port)
	fmt.Printf("    http://127.0.0.1:%d/debug/pprof/heap        堆内存分析\n", port)
	fmt.Printf("    http://127.0.0.1:%d/debug/pprof/goroutine   Goroutine 分析\n", port)
	fmt.Printf("    http://127.0.0.1:%d/workload?type=cpu       触发 CPU 负载\n", port)
	fmt.Printf("    http://127.0.0.1:%d/workload?type=memory    触发内存负载\n", port)
	fmt.Printf("    http://127.0.0.1:%d/stats                   运行时统计\n", port)
	fmt.Println()
	fmt.Println("  分析命令:")
	fmt.Printf("    go tool pprof http://127.0.0.1:%d/debug/pprof/profile?seconds=10\n", port)
	fmt.Printf("    go tool pprof http://127.0.0.1:%d/debug/pprof/heap\n", port)
	fmt.Printf("    go tool pprof -http=:8080 http://127.0.0.1:%d/debug/pprof/profile\n", port)
	fmt.Println()

	// ----------------------------------------------------------
	// 7. 最终状态
	// ----------------------------------------------------------
	fmt.Println("--- 最终运行时状态 ---")
	runtime.GC()
	printRuntimeStats()
	fmt.Println()

	// ----------------------------------------------------------
	// 总结
	// ----------------------------------------------------------
	fmt.Println("=== 总结 ===")
	fmt.Println("1. net/http/pprof 提供开箱即用的性能分析端点")
	fmt.Println("2. go tool pprof 可交互式分析 CPU、内存、goroutine 等")
	fmt.Println("3. CPU profile 帮助定位计算热点（哪些函数消耗最多 CPU）")
	fmt.Println("4. Heap profile 帮助发现内存泄漏和大量分配")
	fmt.Println("5. Goroutine profile 帮助发现 goroutine 泄漏和死锁")
	fmt.Println("6. 在生产环境中，pprof 端点应有访问控制保护")
	fmt.Println("7. 可使用 -http 标志启动 Web UI 查看火焰图")
	fmt.Println()

	// 优雅关闭
	fmt.Println("按 Ctrl+C 关闭服务器（演示模式下自动退出）...")

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 演示模式：设置超时自动退出
	select {
	case sig := <-sigCh:
		fmt.Printf("\n收到信号: %v，正在关闭...\n", sig)
	case <-time.After(2 * time.Second):
		fmt.Println("演示模式：自动关闭服务器")
	}

	signal.Stop(sigCh)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭服务器失败: %v", err)
	}
	fmt.Println("服务器已关闭")
}
