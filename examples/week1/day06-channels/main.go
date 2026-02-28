// Day 06 - Channel 通信与 Select 多路复用
// 本示例演示 Go channel 的核心用法：
// - 无缓冲 channel 与有缓冲 channel 的区别
// - 使用 select 进行多路复用和超时控制
// - Fan-out/Fan-in 模式（多个 worker 写入同一 channel）
// - Channel 方向（只发送、只接收）类型约束
// - 死锁场景分析（已注释，附解释）
// - Done channel 优雅退出模式

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// =============================
// 演示 1：无缓冲 vs 有缓冲 Channel
// =============================

// demonstrateBufferedVsUnbuffered 展示两种 channel 的行为差异
// 无缓冲 channel：发送和接收必须同时就绪（同步）
// 有缓冲 channel：缓冲区未满时发送不阻塞（异步）
func demonstrateBufferedVsUnbuffered() {
	fmt.Println("=== 无缓冲 vs 有缓冲 Channel ===")

	// --- 无缓冲 channel ---
	// make(chan T) 创建无缓冲 channel
	// 发送方会阻塞，直到接收方准备好
	unbuffered := make(chan string)

	go func() {
		fmt.Println("  [无缓冲] 发送方: 准备发送数据...")
		unbuffered <- "hello" // 阻塞，直到有人接收
		fmt.Println("  [无缓冲] 发送方: 数据已被接收")
	}()

	// 稍等一下让 goroutine 先启动并阻塞在发送处
	time.Sleep(50 * time.Millisecond)
	msg := <-unbuffered // 接收数据，发送方此时才会解除阻塞
	fmt.Printf("  [无缓冲] 接收方: 收到 %q\n\n", msg)

	// 等待发送方的打印输出
	time.Sleep(50 * time.Millisecond)

	// --- 有缓冲 channel ---
	// make(chan T, capacity) 创建有缓冲 channel
	// 缓冲区未满时，发送不阻塞
	buffered := make(chan int, 3)

	// 不需要另一个 goroutine，因为缓冲区能容纳 3 个值
	buffered <- 10
	buffered <- 20
	buffered <- 30
	fmt.Printf("  [有缓冲] 已发送 3 个值（缓冲容量=3，无需 goroutine 接收）\n")
	fmt.Printf("  [有缓冲] 当前长度: %d, 容量: %d\n", len(buffered), cap(buffered))

	// 逐个接收
	fmt.Printf("  [有缓冲] 接收: %d, %d, %d\n\n", <-buffered, <-buffered, <-buffered)
}

// =============================
// 演示 2：Select 多路复用 + 超时控制
// =============================

// TaskResult 表示一个异步任务的结果
type TaskResult struct {
	// WorkerID 标识完成此任务的 worker
	WorkerID int
	// Data 是任务产出的数据
	Data string
}

// demonstrateSelectTimeout 展示使用 select 监听多个 channel，并设置超时
// select 类似 switch，但每个 case 是一个 channel 操作
// 当多个 case 同时就绪时，Go 会随机选择一个执行
func demonstrateSelectTimeout() {
	fmt.Println("=== Select 多路复用 + 超时 ===")

	results := make(chan TaskResult, 5)

	// 启动几个模拟任务，延迟各不相同
	go func() {
		time.Sleep(100 * time.Millisecond)
		results <- TaskResult{WorkerID: 1, Data: "快速任务完成"}
	}()
	go func() {
		time.Sleep(300 * time.Millisecond)
		results <- TaskResult{WorkerID: 2, Data: "中速任务完成"}
	}()
	go func() {
		time.Sleep(2 * time.Second) // 这个任务会超时
		results <- TaskResult{WorkerID: 3, Data: "慢速任务完成"}
	}()

	// 设置总超时为 500ms，使用 time.After 创建超时 channel
	timeout := time.After(500 * time.Millisecond)
	collected := 0

	fmt.Println("  开始收集任务结果（超时 500ms）：")

loop:
	for {
		select {
		case r := <-results:
			// 某个任务完成了
			collected++
			fmt.Printf("  ✅ Worker %d: %s\n", r.WorkerID, r.Data)
		case <-timeout:
			// 超时触发，停止等待
			fmt.Println("  ⏰ 超时！停止等待剩余任务")
			break loop // 使用标签跳出 for 循环（而非仅跳出 select）
		}
	}

	fmt.Printf("  📊 共收集到 %d 个结果（超时前完成的任务）\n\n", collected)
}

// =============================
// 演示 3：Fan-out / Fan-in 模式
// =============================

// producer 是只发送方向的 channel 参数示例
// chan<- 表示此函数只能向 channel 发送数据，不能接收
func producer(id int, jobs chan<- int, count int) {
	for i := 0; i < count; i++ {
		job := id*1000 + i
		jobs <- job
	}
}

// worker 演示 channel 方向约束
// <-chan 表示只能从 channel 接收（只读）
// chan<- 表示只能向 channel 发送（只写）
func worker(id int, jobs <-chan int, results chan<- string) {
	for job := range jobs {
		// 模拟处理耗时
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
		results <- fmt.Sprintf("Worker-%d 处理了任务 #%d", id, job)
	}
}

// demonstrateFanOutFanIn 展示 Fan-out/Fan-in 并发模式
// Fan-out：多个 goroutine 从同一个 channel 读取任务
// Fan-in：多个 goroutine 将结果写入同一个 channel
func demonstrateFanOutFanIn() {
	fmt.Println("=== Fan-out / Fan-in 模式 ===")

	const (
		numWorkers = 3  // worker 数量（Fan-out）
		numJobs    = 9  // 总任务数
	)

	// jobs channel：所有任务的分发通道
	jobs := make(chan int, numJobs)
	// results channel：所有 worker 汇聚结果的通道（Fan-in）
	results := make(chan string, numJobs)

	// Fan-out：启动多个 worker，它们从同一个 jobs channel 读取
	var wg sync.WaitGroup
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			worker(workerID, jobs, results)
		}(i)
	}

	// 发送任务到 jobs channel
	for j := 0; j < numJobs; j++ {
		jobs <- j
	}
	close(jobs) // 关闭 channel 通知 worker 没有更多任务

	// 等待所有 worker 完成后关闭 results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Fan-in：从 results channel 收集所有结果
	fmt.Println("  任务处理结果：")
	for r := range results {
		fmt.Printf("  ✅ %s\n", r)
	}
	fmt.Println()
}

// =============================
// 演示 4：Channel 方向类型约束
// =============================

// onlySend 只能向 channel 发送数据
// 编译器会阻止在此函数中从 ch 接收数据
func onlySend(ch chan<- string, msg string) {
	ch <- msg
	// _ = <-ch // 编译错误：不能从 send-only channel 接收
}

// onlyReceive 只能从 channel 接收数据
// 编译器会阻止在此函数中向 ch 发送数据
func onlyReceive(ch <-chan string) string {
	return <-ch
	// ch <- "x" // 编译错误：不能向 receive-only channel 发送
}

// demonstrateChannelDirection 展示 channel 方向约束的好处
func demonstrateChannelDirection() {
	fmt.Println("=== Channel 方向约束 ===")

	// 创建的是双向 channel
	ch := make(chan string, 1)

	// 传递给函数时，Go 会自动将双向 channel 转换为单向 channel
	// 这在编译时就能捕获方向错误，提高代码安全性
	onlySend(ch, "方向约束示例")
	msg := onlyReceive(ch)
	fmt.Printf("  ✅ 通过方向约束安全传递: %q\n", msg)
	fmt.Println("  💡 chan<- T 只发送, <-chan T 只接收, chan T 双向")
	fmt.Println()
}

// =============================
// 演示 5：死锁场景（已注释）
// =============================

// demonstrateDeadlockExplanation 解释常见的 channel 死锁场景
// ⚠️ 下面的代码如果取消注释运行会导致死锁（fatal error: all goroutines are asleep）
func demonstrateDeadlockExplanation() {
	fmt.Println("=== 死锁场景分析（仅解释，未实际运行） ===")

	// --- 死锁场景 1：无缓冲 channel 在同一 goroutine 中发送和接收 ---
	// ch := make(chan int)
	// ch <- 42    // 阻塞！没有其他 goroutine 来接收
	// val := <-ch // 永远不会执行到这里
	// 原因：发送操作阻塞等待接收方，但接收代码在同一 goroutine 中，
	//       而 goroutine 已经被发送操作阻塞了，形成死锁。
	fmt.Println("  场景 1: 无缓冲 channel 在同一 goroutine 中先发后收 → 死锁")
	fmt.Println("    ch := make(chan int)")
	fmt.Println("    ch <- 42    // 阻塞等待接收方")
	fmt.Println("    val := <-ch // 永远无法到达")
	fmt.Println()

	// --- 死锁场景 2：两个 goroutine 互相等待 ---
	// ch1 := make(chan int)
	// ch2 := make(chan int)
	// go func() { ch1 <- <-ch2 }()  // 等待 ch2 才能发送到 ch1
	// go func() { ch2 <- <-ch1 }()  // 等待 ch1 才能发送到 ch2
	// 原因：goroutine A 等待从 ch2 接收，goroutine B 等待从 ch1 接收，
	//       两者互相依赖，谁也无法前进。
	fmt.Println("  场景 2: 两个 goroutine 互相等待对方的 channel → 死锁")
	fmt.Println("    go func() { ch1 <- <-ch2 }()")
	fmt.Println("    go func() { ch2 <- <-ch1 }()")
	fmt.Println()

	// --- 死锁场景 3：忘记关闭 channel 导致 range 永远阻塞 ---
	// ch := make(chan int)
	// go func() { for i := 0; i < 3; i++ { ch <- i } }()
	// for v := range ch { fmt.Println(v) } // range 会等待 channel 关闭
	// 原因：range 循环在 channel 关闭前不会退出，
	//       但发送方发完数据后没有 close(ch)，range 永远阻塞。
	fmt.Println("  场景 3: 忘记 close(ch)，for-range 永远阻塞 → 死锁")
	fmt.Println("    解决方法：发送方完成后调用 close(ch)")
	fmt.Println()
}

// =============================
// 演示 6：Done Channel 优雅退出
// =============================

// backgroundWorker 模拟一个后台工作 goroutine
// 它会持续运行直到收到 done 信号
func backgroundWorker(id int, done <-chan struct{}, results chan<- string) {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	count := 0

	for {
		select {
		case <-done:
			// 收到退出信号，执行清理并退出
			results <- fmt.Sprintf("Worker-%d: 已优雅退出（共处理 %d 次）", id, count)
			return
		case <-ticker.C:
			// 定期执行工作
			count++
		}
	}
}

// demonstrateDoneChannel 展示使用 done channel 优雅关闭 goroutine
// 这是 Go 中非常常见的模式：通过关闭一个 channel 来广播退出信号
// 关闭 channel 后，所有从该 channel 接收的操作都会立即返回零值
func demonstrateDoneChannel() {
	fmt.Println("=== Done Channel 优雅退出 ===")

	// done 是退出信号 channel
	// 使用空结构体 struct{} 因为我们不需要传递任何数据，只需要信号
	done := make(chan struct{})
	results := make(chan string, 3)

	// 启动多个后台 worker
	numWorkers := 3
	for i := 1; i <= numWorkers; i++ {
		go backgroundWorker(i, done, results)
	}

	fmt.Println("  🚀 已启动 3 个后台 worker...")

	// 让 worker 运行一段时间
	time.Sleep(300 * time.Millisecond)

	// 关闭 done channel，广播退出信号给所有 worker
	// 这是 done channel 的关键：close() 会使所有 <-done 操作立即返回
	fmt.Println("  📢 发送退出信号（close done channel）...")
	close(done)

	// 收集所有 worker 的退出消息
	for i := 0; i < numWorkers; i++ {
		msg := <-results
		fmt.Printf("  ✅ %s\n", msg)
	}
	fmt.Println()
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════╗")
	fmt.Println("║   Day 06 - Channel 通信与 Select 多路复用  ║")
	fmt.Println("╚═══════════════════════════════════════════╝")
	fmt.Println()

	// 1. 无缓冲 vs 有缓冲 channel
	demonstrateBufferedVsUnbuffered()

	// 2. Select 多路复用 + 超时控制
	demonstrateSelectTimeout()

	// 3. Fan-out / Fan-in 模式
	demonstrateFanOutFanIn()

	// 4. Channel 方向约束
	demonstrateChannelDirection()

	// 5. 死锁场景分析
	demonstrateDeadlockExplanation()

	// 6. Done channel 优雅退出
	demonstrateDoneChannel()

	fmt.Println("📚 小结：Channel 是 Go 并发通信的核心；select 实现多路复用；done channel 是优雅退出的标准模式！")
}
