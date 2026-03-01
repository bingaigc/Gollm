// Day 18 - SSE 进阶模式 (Server-Sent Events Advanced Patterns)
//
// 本示例演示 SSE 流式传输的进阶技术：
// - 多频道 SSE 广播 (Pub/Sub)：Broadcaster 分发消息到不同频道的订阅者
// - Last-Event-ID 重连机制：服务端事件缓冲，断线后通过 Last-Event-ID 续传
// - SSE 事件类型过滤：不同 event type（data/notification），客户端按类型过滤
// - 背压控制 (Backpressure)：非阻塞发送 + 丢弃策略管理慢消费者
// - 心跳保活：定期发送 SSE 注释行 (: heartbeat) 保持连接
// - 优雅关闭：context 控制 SSE 连接生命周期
//
// 🔑 核心概念：生产级 SSE 系统需要处理多客户端广播、断线重连、
// 慢消费者背压、心跳保活等问题。本示例逐一演示这些进阶模式。

package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 多频道 SSE 广播器 (Pub/Sub Broadcaster)
// ============================================================

// SSEEvent 表示一条完整的 SSE 事件
type SSEEvent struct {
	ID    int    // 事件 ID，用于 Last-Event-ID 重连
	Type  string // 事件类型：data / notification 等
	Data  string // 事件数据负载
	Retry int    // 建议重连间隔（毫秒）
}

// Format 将事件格式化为 SSE 协议文本
func (e SSEEvent) Format() string {
	var sb strings.Builder
	if e.ID > 0 {
		sb.WriteString(fmt.Sprintf("id: %d\n", e.ID))
	}
	if e.Type != "" && e.Type != "message" {
		sb.WriteString(fmt.Sprintf("event: %s\n", e.Type))
	}
	if e.Retry > 0 {
		sb.WriteString(fmt.Sprintf("retry: %d\n", e.Retry))
	}
	sb.WriteString(fmt.Sprintf("data: %s\n\n", e.Data))
	return sb.String()
}

// Subscriber 代表一个 SSE 客户端订阅者
type Subscriber struct {
	Channel string
	Events  chan SSEEvent // 带缓冲通道（背压控制关键）
	Done    chan struct{}
	Dropped int        // 因背压丢弃的事件计数
	mu      sync.Mutex // 保护 Dropped
}

// Broadcaster 多频道广播器，管理订阅者并分发消息
type Broadcaster struct {
	subscribers map[string][]*Subscriber
	eventLog    []SSEEvent   // 事件历史缓冲（用于重连续传）
	maxLog      int          // 缓冲最大容量
	nextID      int
	mu          sync.RWMutex
}

func NewBroadcaster(maxLogSize int) *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string][]*Subscriber),
		eventLog:    make([]SSEEvent, 0, maxLogSize),
		maxLog:      maxLogSize,
		nextID:      1,
	}
}

func (b *Broadcaster) Subscribe(channel string, bufferSize int) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub := &Subscriber{
		Channel: channel,
		Events:  make(chan SSEEvent, bufferSize),
		Done:    make(chan struct{}),
	}
	b.subscribers[channel] = append(b.subscribers[channel], sub)
	return sub
}

func (b *Broadcaster) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subscribers[sub.Channel] {
		if s == sub {
			b.subscribers[sub.Channel] = append(b.subscribers[sub.Channel][:i], b.subscribers[sub.Channel][i+1:]...)
			close(sub.Done)
			return
		}
	}
}

// Publish 向频道发布事件，非阻塞：通道满则丢弃（背压策略）
func (b *Broadcaster) Publish(channel, eventType, data string) SSEEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	event := SSEEvent{ID: b.nextID, Type: eventType, Data: data}
	b.nextID++
	b.eventLog = append(b.eventLog, event)
	if len(b.eventLog) > b.maxLog {
		b.eventLog = b.eventLog[len(b.eventLog)-b.maxLog:]
	}
	for _, sub := range b.subscribers[channel] {
		select {
		case sub.Events <- event:
		default: // 通道满，丢弃（非阻塞背压策略）
			sub.mu.Lock()
			sub.Dropped++
			sub.mu.Unlock()
		}
	}
	return event
}

// GetEventsSince 获取 lastID 之后的事件（断线重连用）
func (b *Broadcaster) GetEventsSince(lastID int) []SSEEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var result []SSEEvent
	for _, e := range b.eventLog {
		if e.ID > lastID {
			result = append(result, e)
		}
	}
	return result
}

// ============================================================
// SSE 服务器处理器 —— 多频道 + 重连 + 心跳
// ============================================================

func newAdvancedSSEHandler(b *Broadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			channel = "default"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// 解析 Last-Event-ID（断线重连）
		lastEventID := 0
		if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
			if id, err := strconv.Atoi(idStr); err == nil {
				lastEventID = id
			}
		}
		sub := b.Subscribe(channel, 5)
		defer b.Unsubscribe(sub)
		// 补发断线期间错过的事件
		if lastEventID > 0 {
			for _, event := range b.GetEventsSince(lastEventID) {
				fmt.Fprint(w, event.Format())
			}
			flusher.Flush()
		}
		fmt.Fprintf(w, "retry: 3000\n\n")
		flusher.Flush()
		heartbeat := time.NewTicker(2 * time.Second)
		defer heartbeat.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-sub.Events:
				fmt.Fprint(w, event.Format())
				flusher.Flush()
			case <-heartbeat.C:
				fmt.Fprintf(w, ": heartbeat %s\n\n", time.Now().Format("15:04:05"))
				flusher.Flush()
			}
		}
	}
}

// ============================================================
// 演示 1：多频道 Pub/Sub 广播
// ============================================================

func demonstratePubSub() {
	fmt.Println("📡 演示 1：多频道 Pub/Sub 广播")
	fmt.Println(strings.Repeat("-", 50))
	b := NewBroadcaster(100)
	server := httptest.NewServer(newAdvancedSSEHandler(b))
	defer server.Close()

	var wg sync.WaitGroup
	collected := make(map[string][]string)
	var mu sync.Mutex

	// 客户端 A 订阅 news，客户端 B 订阅 alerts
	for _, ch := range []string{"news", "alerts"} {
		wg.Add(1)
		go func(channel string) {
			defer wg.Done()
			events := readSSEEvents(server.URL+"?channel="+channel, "", 3*time.Second)
			mu.Lock()
			collected[channel] = events
			mu.Unlock()
		}(ch)
	}

	time.Sleep(200 * time.Millisecond) // 等待连接建立

	// 向不同频道发布消息
	b.Publish("news", "data", "今日头条：Go 1.22 发布")
	b.Publish("alerts", "notification", "系统告警：CPU 使用率 95%")
	b.Publish("news", "data", "科技新闻：AI 助手更新")
	b.Publish("alerts", "notification", "警告：磁盘空间不足")
	b.Publish("news", "data", "财经快报：市场大涨")
	fmt.Println("  📤 news 频道: 3 条, alerts 频道: 2 条")

	wg.Wait()

	mu.Lock()
	for _, ch := range []string{"news", "alerts"} {
		fmt.Printf("  📥 %s 频道收到 %d 条:\n", ch, len(collected[ch]))
		for _, e := range collected[ch] {
			fmt.Printf("    • %s\n", e)
		}
	}
	mu.Unlock()

	fmt.Println("  💡 Broadcaster 用 map[channel][]subscriber 路由消息到正确订阅者")
	fmt.Println()
}

// ============================================================
// 演示 2：Last-Event-ID 断线重连
// ============================================================

func demonstrateReconnect() {
	fmt.Println("🔄 演示 2：Last-Event-ID 断线重连机制")
	fmt.Println(strings.Repeat("-", 50))
	b := NewBroadcaster(100)
	server := httptest.NewServer(newAdvancedSSEHandler(b))
	defer server.Close()

	fmt.Println("  📤 预发布 5 条事件到缓冲区")
	for i := 1; i <= 5; i++ {
		b.Publish("default", "data", fmt.Sprintf("事件 #%d", i))
	}

	// 模拟：客户端收到 ID=3 后断线，重连带 Last-Event-ID: 3
	fmt.Println("  🔌 模拟断线重连，Last-Event-ID: 3")
	events := readSSEEvents(server.URL, "3", 2*time.Second)

	fmt.Printf("  📥 重连后收到 %d 条事件:\n", len(events))
	for _, e := range events {
		fmt.Printf("    • %s\n", e)
	}

	fmt.Println("  💡 服务端维护事件缓冲，客户端通过 Last-Event-ID 头续传")
	fmt.Println()
}

// ============================================================
// 演示 3：SSE 事件类型过滤
// ============================================================

func demonstrateEventTypes() {
	fmt.Println("🏷️  演示 3：SSE 事件类型过滤")
	fmt.Println(strings.Repeat("-", 50))
	b := NewBroadcaster(100)
	server := httptest.NewServer(newAdvancedSSEHandler(b))
	defer server.Close()
	var wg sync.WaitGroup
	var allEvents []SSEEvent
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		parsed := readSSEEventsTyped(server.URL+"?channel=mixed", 3*time.Second)
		mu.Lock()
		allEvents = parsed
		mu.Unlock()
	}()

	time.Sleep(200 * time.Millisecond)

	// 发布多种类型事件
	b.Publish("mixed", "data", "普通数据消息")
	b.Publish("mixed", "notification", "系统通知：部署完成")
	b.Publish("mixed", "data", "又一条数据消息")
	b.Publish("mixed", "notification", "提醒：会议即将开始")
	fmt.Println("  📤 发布: data×2, notification×2")

	wg.Wait()

	// 按类型分组展示
	mu.Lock()
	groups := make(map[string][]string)
	for _, e := range allEvents {
		t := e.Type
		if t == "" {
			t = "message"
		}
		groups[t] = append(groups[t], e.Data)
	}
	mu.Unlock()

	fmt.Println("  📥 按类型过滤：")
	for _, t := range []string{"data", "notification", "message"} {
		if msgs, ok := groups[t]; ok {
			fmt.Printf("    [%s] %d 条:", t, len(msgs))
			for _, m := range msgs {
				fmt.Printf(" %s;", m)
			}
			fmt.Println()
		}
	}

	fmt.Println("  💡 SSE event 字段分类事件，客户端用 addEventListener 按类型监听")
	fmt.Println()
}

// ============================================================
// 演示 4：背压控制 (Backpressure)
// ============================================================

func demonstrateBackpressure() {
	fmt.Println("⚡ 演示 4：背压控制 (Backpressure)")
	fmt.Println(strings.Repeat("-", 50))
	b := NewBroadcaster(100)
	fmt.Println("  📝 缓冲区大小=3，快速发布 10 条，超出部分将丢弃")
	sub := b.Subscribe("pressure", 3)
	for i := 1; i <= 10; i++ {
		b.Publish("pressure", "data", fmt.Sprintf("消息 #%d", i))
	}
	buffered := len(sub.Events)
	sub.mu.Lock()
	dropped := sub.Dropped
	sub.mu.Unlock()
	fmt.Printf("  📊 缓冲: %d 条, 丢弃: %d 条, 总计: %d\n", buffered, dropped, buffered+dropped)
	fmt.Println("  📥 消费缓冲区：")
	for {
		select {
		case event := <-sub.Events:
			fmt.Printf("    • %s (ID=%d)\n", event.Data, event.ID)
		default:
			goto done
		}
	}
done:
	b.Unsubscribe(sub)
	fmt.Println("  💡 select+default 非阻塞丢弃策略，防止慢消费者阻塞广播系统")
	fmt.Println("     可选策略：丢弃最新（当前）/ 丢弃最旧（环形缓冲）/ 阻塞等待")
	fmt.Println()
}

// ============================================================
// 演示 5：心跳保活机制
// ============================================================

func demonstrateHeartbeat() {
	fmt.Println("💓 演示 5：心跳保活机制")
	fmt.Println(strings.Repeat("-", 50))
	handler := func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ctx := r.Context()
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-tick.C:
				count++
				if count%3 == 0 {
					fmt.Fprintf(w, "data: 数据事件 #%d\n\n", count/3)
				} else {
					fmt.Fprintf(w, ": heartbeat %s\n\n", t.Format("15:04:05.000"))
				}
				flusher.Flush()
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	fmt.Println("  📝 SSE 注释行 (:) 浏览器忽略但保持连接，穿透代理超时")

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("  ❌ 连接失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	heartbeats, dataEvents := 0, 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			heartbeats++
			if heartbeats <= 3 {
				fmt.Printf("    💗 心跳: %s\n", strings.TrimPrefix(line, ": "))
			}
		} else if strings.HasPrefix(line, "data: ") {
			dataEvents++
			fmt.Printf("    📦 数据: %s\n", strings.TrimPrefix(line, "data: "))
		}
	}

	fmt.Printf("  📊 统计: %d 次心跳, %d 条数据事件\n", heartbeats, dataEvents)
	fmt.Println("  💡 心跳行不触发 onmessage，仅用于保持连接存活")
	fmt.Println()
}

// ============================================================
// 演示 6：优雅关闭 (Graceful Shutdown)
// ============================================================

func demonstrateGracefulShutdown() {
	fmt.Println("🛑 演示 6：优雅关闭 (Graceful Shutdown)")
	fmt.Println(strings.Repeat("-", 50))
	b := NewBroadcaster(100)
	server := httptest.NewServer(newAdvancedSSEHandler(b))
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var received []string
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"?channel=shutdown", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				mu.Lock()
				received = append(received, strings.TrimPrefix(line, "data: "))
				mu.Unlock()
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)

	b.Publish("shutdown", "data", "消息 A")
	b.Publish("shutdown", "data", "消息 B")
	time.Sleep(100 * time.Millisecond)

	fmt.Println("  ⏹️  触发 context 取消...")
	cancel()
	wg.Wait()
	server.Close()

	mu.Lock()
	fmt.Printf("  📥 关闭前收到 %d 条消息:", len(received))
	for _, msg := range received {
		fmt.Printf(" %s;", msg)
	}
	fmt.Println()
	mu.Unlock()

	fmt.Println("  ✅ 连接已优雅关闭，无 goroutine 泄漏")
	fmt.Println("  💡 context.WithCancel 控制 SSE 生命周期，取消后请求自动中断")
	fmt.Println()
}

// ============================================================
// SSE 客户端辅助函数
// ============================================================

// readSSEEvents 连接 SSE 端点，收集 data 行
func readSSEEvents(url, lastEventID string, timeout time.Duration) []string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var events []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}
	return events
}

// readSSEEventsTyped 解析完整 SSE 事件（含类型和 ID）
func readSSEEventsTyped(url string, timeout time.Duration) []SSEEvent {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var events []SSEEvent
	var current SSEEvent
	hasData := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if hasData {
				events = append(events, current)
				current = SSEEvent{}
				hasData = false
			}
			continue
		}
		if strings.HasPrefix(line, "id: ") {
			id, _ := strconv.Atoi(strings.TrimPrefix(line, "id: "))
			current.ID = id
		} else if strings.HasPrefix(line, "event: ") {
			current.Type = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
			hasData = true
		} else if strings.HasPrefix(line, "retry: ") {
			val, _ := strconv.Atoi(strings.TrimPrefix(line, "retry: "))
			current.Retry = val
		}
	}
	return events
}

// ============================================================
// 主函数
// ============================================================

func main() {
	fmt.Println("=== Day 18: SSE 进阶模式 (Server-Sent Events Advanced Patterns) ===")
	fmt.Println()
	fmt.Println("🔑 进阶主题：Pub/Sub广播 | 断线重连 | 类型过滤 | 背压控制 | 心跳 | 优雅关闭")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	demos := []func(){
		demonstratePubSub, demonstrateReconnect, demonstrateEventTypes,
		demonstrateBackpressure, demonstrateHeartbeat, demonstrateGracefulShutdown,
	}
	for _, demo := range demos {
		demo()
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println()
	}

	fmt.Println("🎓 Day 18 学习要点总结：")
	fmt.Println("  1. Pub/Sub 用 map[channel][]subscriber 实现消息路由")
	fmt.Println("  2. Last-Event-ID 是 SSE 内置重连机制，服务端需维护事件缓冲")
	fmt.Println("  3. event 字段使同一连接可传输多种类型事件")
	fmt.Println("  4. select+default 非阻塞丢弃防止慢消费者拖垮系统")
	fmt.Println("  5. 心跳注释行 (: comment) 穿透中间代理超时")
	fmt.Println("  6. context.WithCancel 是控制 SSE 连接生命周期的标准方式")
}
