// Package main 演示 Go 性能基准测试与优化技巧
//
// 本示例实现了一个简易的基准测试框架，对比以下场景的性能差异：
//   - 字符串拼接：+ 运算符 vs strings.Builder vs bytes.Buffer
//   - 小集合查找：map vs slice
//   - 并发读写：sync.Mutex vs sync.RWMutex vs sync.Map
//   - 对象复用：sync.Pool vs 直接分配
//
// 同时演示使用 runtime.MemStats 追踪内存分配情况
//
// 优化提示：
//   - 使用 go test -bench=. -benchmem 运行官方基准测试
//   - 使用 go tool pprof 进行 CPU 和内存 profiling
//   - 优化前先用 benchmark 建立基线，避免盲目优化
//   - 关注分配次数（allocs/op），减少 GC 压力往往比减少 CPU 时间更有效
package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 基准测试框架
// ============================================================

// BenchResult 保存单次基准测试的结果
type BenchResult struct {
	Name       string        // 测试名称
	Duration   time.Duration // 总耗时
	Ops        int           // 操作次数
	AllocBytes uint64        // 分配的字节数
	AllocCount uint64        // 分配次数
}

// OpsPerSec 计算每秒操作数
func (r BenchResult) OpsPerSec() float64 {
	return float64(r.Ops) / r.Duration.Seconds()
}

// NsPerOp 计算每次操作的纳秒数
func (r BenchResult) NsPerOp() int64 {
	if r.Ops == 0 {
		return 0
	}
	return r.Duration.Nanoseconds() / int64(r.Ops)
}

// BytesPerOp 计算每次操作的分配字节数
func (r BenchResult) BytesPerOp() uint64 {
	if r.Ops == 0 {
		return 0
	}
	return r.AllocBytes / uint64(r.Ops)
}

// AllocsPerOp 计算每次操作的分配次数
func (r BenchResult) AllocsPerOp() uint64 {
	if r.Ops == 0 {
		return 0
	}
	return r.AllocCount / uint64(r.Ops)
}

// runBenchmark 运行基准测试，自动追踪内存分配
// iterations: 执行次数
// fn: 被测试的函数
func runBenchmark(name string, iterations int, fn func()) BenchResult {
	// 强制 GC，获取干净的内存基线
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	for i := 0; i < iterations; i++ {
		fn()
	}
	duration := time.Since(start)

	// 再次 GC 后读取内存统计
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	return BenchResult{
		Name:       name,
		Duration:   duration,
		Ops:        iterations,
		AllocBytes: memAfter.TotalAlloc - memBefore.TotalAlloc,
		AllocCount: memAfter.Mallocs - memBefore.Mallocs,
	}
}

// printResults 以表格形式打印基准测试结果
func printResults(title string, results []BenchResult) {
	fmt.Printf("\n%s\n", title)
	fmt.Println(strings.Repeat("─", 90))
	fmt.Printf("%-30s %12s %12s %15s %12s\n",
		"测试项", "ns/op", "ops/sec", "bytes/op", "allocs/op")
	fmt.Println(strings.Repeat("─", 90))

	for _, r := range results {
		fmt.Printf("%-30s %12d %12.0f %15d %12d\n",
			r.Name, r.NsPerOp(), r.OpsPerSec(), r.BytesPerOp(), r.AllocsPerOp())
	}
	fmt.Println(strings.Repeat("─", 90))

	// 标注最快和最慢
	if len(results) > 1 {
		fastest := results[0]
		slowest := results[0]
		for _, r := range results[1:] {
			if r.NsPerOp() < fastest.NsPerOp() {
				fastest = r
			}
			if r.NsPerOp() > slowest.NsPerOp() {
				slowest = r
			}
		}
		if fastest.NsPerOp() > 0 {
			speedup := float64(slowest.NsPerOp()) / float64(fastest.NsPerOp())
			fmt.Printf("🏆 最快: %-25s  🐢 最慢: %-25s  (快 %.1fx)\n",
				fastest.Name, slowest.Name, speedup)
		}
	}
}

// ============================================================
// 基准测试 1：字符串拼接
//
// 优化建议：
//   - 少量拼接（<5次）：直接用 + 即可，编译器会优化
//   - 循环拼接：必须用 strings.Builder，避免 O(n²) 的内存分配
//   - bytes.Buffer 与 Builder 性能接近，但 Builder 更轻量
//   - 如果预知长度，用 Builder.Grow() 预分配可进一步减少分配
// ============================================================

// benchStringConcat 使用 + 运算符拼接（每次拼接都创建新字符串，O(n²) 复杂度）
func benchStringConcat(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += "hello"
	}
	return s
}

// benchStringBuilder 使用 strings.Builder 拼接（Go 1.10+ 推荐方式）
func benchStringBuilder(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString("hello")
	}
	return sb.String()
}

// benchStringBuilderPrealloc 使用预分配的 strings.Builder
func benchStringBuilderPrealloc(n int) string {
	var sb strings.Builder
	sb.Grow(n * 5) // 预分配足够空间，避免扩容
	for i := 0; i < n; i++ {
		sb.WriteString("hello")
	}
	return sb.String()
}

// benchBytesBuffer 使用 bytes.Buffer 拼接
func benchBytesBuffer(n int) string {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.WriteString("hello")
	}
	return buf.String()
}

func runStringBenchmarks() {
	const strCount = 1000 // 每次拼接 1000 个字符串
	const iterations = 100

	results := []BenchResult{
		runBenchmark("string + 运算符", iterations, func() {
			_ = benchStringConcat(strCount)
		}),
		runBenchmark("strings.Builder", iterations, func() {
			_ = benchStringBuilder(strCount)
		}),
		runBenchmark("strings.Builder+Grow", iterations, func() {
			_ = benchStringBuilderPrealloc(strCount)
		}),
		runBenchmark("bytes.Buffer", iterations, func() {
			_ = benchBytesBuffer(strCount)
		}),
	}

	printResults("📝 字符串拼接性能对比（拼接 1000 个 \"hello\"）", results)
}

// ============================================================
// 基准测试 2：小集合查找 — Map vs Slice
//
// 优化建议：
//   - 元素数量 < ~20 时，slice 线性查找可能比 map 更快
//     （因为 slice 内存连续，CPU 缓存友好）
//   - 元素数量 > ~50 时，map 的 O(1) 查找优势明显
//   - 如果 key 是 int 且范围小，考虑直接用数组索引
// ============================================================

func runCollectionBenchmarks() {
	// 测试不同集合大小
	for _, size := range []int{5, 20, 100} {
		runCollectionBenchmarkForSize(size)
	}
}

func runCollectionBenchmarkForSize(size int) {
	const iterations = 10000

	// 准备测试数据
	rng := rand.New(rand.NewSource(42))
	keys := make([]string, size)
	for i := 0; i < size; i++ {
		keys[i] = fmt.Sprintf("key-%04d", i)
	}

	// 准备 map
	m := make(map[string]int, size)
	for i, k := range keys {
		m[k] = i
	}

	// 准备 slice（键值对）
	type kv struct {
		key   string
		value int
	}
	slice := make([]kv, size)
	for i, k := range keys {
		slice[i] = kv{key: k, value: i}
	}

	// 随机选择要查找的 key
	lookupKeys := make([]string, iterations)
	for i := range lookupKeys {
		lookupKeys[i] = keys[rng.Intn(size)]
	}

	results := []BenchResult{
		runBenchmark(fmt.Sprintf("map[string]int (n=%d)", size), iterations, func() {
			for _, k := range lookupKeys {
				_ = m[k]
			}
		}),
		runBenchmark(fmt.Sprintf("slice 线性查找 (n=%d)", size), iterations, func() {
			for _, k := range lookupKeys {
				for _, item := range slice {
					if item.key == k {
						_ = item.value
						break
					}
				}
			}
		}),
	}

	printResults(fmt.Sprintf("🔍 集合查找性能对比（集合大小=%d）", size), results)
}

// ============================================================
// 基准测试 3：并发读写 — Mutex vs RWMutex vs sync.Map
//
// 优化建议：
//   - 读多写少（读占比 > 90%）：RWMutex 或 sync.Map 更优
//   - 读写均衡：普通 Mutex 最简单也够用
//   - sync.Map 适合以下两种场景：
//     1. key 只写入一次但读取多次（如缓存）
//     2. 多个 goroutine 读写不同的 key（无竞争）
//   - sync.Map 不适合频繁写入同一个 key 的场景
// ============================================================

func runConcurrencyBenchmarks() {
	const (
		goroutines = 8     // 并发 goroutine 数量
		opsPerGoroutine = 1000
		readRatio  = 90    // 读操作占比 (%)
	)

	results := []BenchResult{
		benchMutex(goroutines, opsPerGoroutine, readRatio),
		benchRWMutex(goroutines, opsPerGoroutine, readRatio),
		benchSyncMap(goroutines, opsPerGoroutine, readRatio),
	}

	printResults(fmt.Sprintf("🔒 并发读写性能对比（%d goroutine，读占比 %d%%）",
		goroutines, readRatio), results)
}

// benchMutex 使用 sync.Mutex 的并发读写基准
func benchMutex(goroutines, opsPerGoroutine, readRatio int) BenchResult {
	return runBenchmark("sync.Mutex", 1, func() {
		var mu sync.Mutex
		m := make(map[string]int)
		// 预填充数据
		for i := 0; i < 100; i++ {
			m[fmt.Sprintf("key-%d", i)] = i
		}

		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(id)))
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("key-%d", rng.Intn(100))
					if rng.Intn(100) < readRatio {
						// 读操作
						mu.Lock()
						_ = m[key]
						mu.Unlock()
					} else {
						// 写操作
						mu.Lock()
						m[key] = rng.Int()
						mu.Unlock()
					}
				}
			}(g)
		}
		wg.Wait()
	})
}

// benchRWMutex 使用 sync.RWMutex 的并发读写基准
func benchRWMutex(goroutines, opsPerGoroutine, readRatio int) BenchResult {
	return runBenchmark("sync.RWMutex", 1, func() {
		var mu sync.RWMutex
		m := make(map[string]int)
		for i := 0; i < 100; i++ {
			m[fmt.Sprintf("key-%d", i)] = i
		}

		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(id)))
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("key-%d", rng.Intn(100))
					if rng.Intn(100) < readRatio {
						// 读操作：使用 RLock，允许多个 reader 并发
						mu.RLock()
						_ = m[key]
						mu.RUnlock()
					} else {
						// 写操作：使用 Lock，独占
						mu.Lock()
						m[key] = rng.Int()
						mu.Unlock()
					}
				}
			}(g)
		}
		wg.Wait()
	})
}

// benchSyncMap 使用 sync.Map 的并发读写基准
func benchSyncMap(goroutines, opsPerGoroutine, readRatio int) BenchResult {
	return runBenchmark("sync.Map", 1, func() {
		var m sync.Map
		for i := 0; i < 100; i++ {
			m.Store(fmt.Sprintf("key-%d", i), i)
		}

		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(id)))
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("key-%d", rng.Intn(100))
					if rng.Intn(100) < readRatio {
						// 读操作
						v, _ := m.Load(key)
						_ = v
					} else {
						// 写操作
						m.Store(key, rng.Int())
					}
				}
			}(g)
		}
		wg.Wait()
	})
}

// ============================================================
// 基准测试 4：对象池 — sync.Pool vs 直接分配
//
// 优化建议：
//   - sync.Pool 适合高频创建/销毁的临时对象（如 buffer、临时结构体）
//   - Pool 中的对象可能在任意 GC 周期被回收，不要依赖其持久性
//   - 使用 Pool 前记得重置对象状态（如 buffer.Reset()）
//   - 对于生命周期明确的对象，直接分配可能更简单高效
// ============================================================

// LargeObject 模拟一个较大的临时对象
type LargeObject struct {
	Data [4096]byte // 4KB 数据
	ID   int
}

func runPoolBenchmarks() {
	const iterations = 10000

	// 创建 sync.Pool
	pool := &sync.Pool{
		New: func() interface{} {
			return &LargeObject{}
		},
	}

	results := []BenchResult{
		runBenchmark("直接分配 (new)", iterations, func() {
			obj := &LargeObject{}
			obj.ID = 42
			obj.Data[0] = 1
			// 对象在函数返回后成为垃圾，等待 GC 回收
			_ = obj
		}),
		runBenchmark("sync.Pool 复用", iterations, func() {
			// 从 Pool 获取对象（可能是复用的，也可能是新创建的）
			obj := pool.Get().(*LargeObject)
			obj.ID = 42
			obj.Data[0] = 1
			// 使用完后归还到 Pool，供后续复用
			pool.Put(obj)
		}),
	}

	printResults("♻️  对象池性能对比（4KB 对象）", results)
}

// ============================================================
// 内存统计
// ============================================================

// printMemStats 打印当前内存使用统计
func printMemStats(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("\n📊 内存统计 [%s]\n", label)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  堆内存分配:     %10s\n", formatBytes(m.HeapAlloc))
	fmt.Printf("  堆内存使用:     %10s\n", formatBytes(m.HeapInuse))
	fmt.Printf("  堆内存系统占用: %10s\n", formatBytes(m.HeapSys))
	fmt.Printf("  累计分配总量:   %10s\n", formatBytes(m.TotalAlloc))
	fmt.Printf("  系统内存:       %10s\n", formatBytes(m.Sys))
	fmt.Printf("  GC 次数:        %10d\n", m.NumGC)
	fmt.Printf("  Goroutine 数:   %10d\n", runtime.NumGoroutine())
	fmt.Println(strings.Repeat("─", 50))
}

// formatBytes 将字节数格式化为人类可读的字符串
func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func main() {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║   Go 性能基准测试与优化                  ║")
	fmt.Println("╚══════════════════════════════════════════╝")

	// 打印环境信息
	fmt.Printf("\n运行环境: Go %s, %s/%s, CPU 核心数: %d\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())

	// 初始内存状态
	printMemStats("基准测试开始前")

	// 运行各组基准测试
	fmt.Println("\n========================================")
	fmt.Println("  基准测试 1：字符串拼接")
	fmt.Println("========================================")
	runStringBenchmarks()

	fmt.Println("\n========================================")
	fmt.Println("  基准测试 2：小集合查找")
	fmt.Println("========================================")
	runCollectionBenchmarks()

	fmt.Println("\n========================================")
	fmt.Println("  基准测试 3：并发读写")
	fmt.Println("========================================")
	runConcurrencyBenchmarks()

	fmt.Println("\n========================================")
	fmt.Println("  基准测试 4：对象池")
	fmt.Println("========================================")
	runPoolBenchmarks()

	// 最终内存状态
	printMemStats("所有基准测试完成后")

	// 打印优化建议总结
	fmt.Println()
	fmt.Println("=== 性能优化建议总结 ===")
	fmt.Println()
	fmt.Println("  📝 字符串拼接:")
	fmt.Println("     • 循环中务必使用 strings.Builder，避免 + 运算符的 O(n²) 问题")
	fmt.Println("     • 已知长度时使用 Grow() 预分配，减少扩容开销")
	fmt.Println()
	fmt.Println("  🔍 集合查找:")
	fmt.Println("     • 小集合（<20）时 slice 可能更快（CPU 缓存友好）")
	fmt.Println("     • 大集合必须使用 map（O(1) vs O(n)）")
	fmt.Println()
	fmt.Println("  🔒 并发控制:")
	fmt.Println("     • 读多写少场景使用 sync.RWMutex 或 sync.Map")
	fmt.Println("     • 读写均衡场景 sync.Mutex 最简单")
	fmt.Println("     • sync.Map 适合 key 稳定的缓存场景")
	fmt.Println()
	fmt.Println("  ♻️  对象池:")
	fmt.Println("     • 高频创建的大对象适合用 sync.Pool")
	fmt.Println("     • 使用前重置对象状态，归还后不再访问")
	fmt.Println()
	fmt.Println("  🔧 通用工具:")
	fmt.Println("     • go test -bench=. -benchmem   — 官方基准测试")
	fmt.Println("     • go tool pprof                 — CPU/内存 profiling")
	fmt.Println("     • go test -race                 — 竞态检测")
	fmt.Println("     • GOGC=off go test -bench=.     — 禁用 GC 测试纯计算性能")
}
