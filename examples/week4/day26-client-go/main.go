// Package main 模拟 Kubernetes client-go 的核心模式
//
// 本示例不依赖真实的 K8s 集群和 client-go 库，而是用纯标准库
// 重新实现 client-go 中最重要的设计模式：
//
//   - Informer 模式（ListWatch + 本地缓存 + 事件分发）
//   - WorkQueue（带速率限制的工作队列）
//   - Controller（调谐循环 Reconcile Loop）
//
// 这些模式是所有 K8s 控制器和 Operator 的基础
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 第一部分：K8s 资源类型定义
// 对应 k8s.io/api/core/v1 中的类型
// ============================================================

// ObjectMeta 对应 metav1.ObjectMeta，包含资源的元数据
type ObjectMeta struct {
	Name            string            // 资源名称，在命名空间内唯一
	Namespace       string            // 命名空间
	ResourceVersion string            // 资源版本，用于乐观并发控制
	Labels          map[string]string // 标签，用于选择器匹配
}

// PodPhase 表示 Pod 的运行阶段
type PodPhase string

const (
	PodPending   PodPhase = "Pending"   // 等待调度
	PodRunning   PodPhase = "Running"   // 正在运行
	PodSucceeded PodPhase = "Succeeded" // 成功完成
	PodFailed    PodPhase = "Failed"    // 运行失败
)

// Pod 对应 corev1.Pod
type Pod struct {
	ObjectMeta
	Spec   PodSpec   // 期望状态（声明式）
	Status PodStatus // 实际状态
}

// PodSpec 对应 corev1.PodSpec，描述 Pod 的期望配置
type PodSpec struct {
	Containers []Container // 容器列表
	NodeName   string      // 被调度到的节点
}

// Container 对应 corev1.Container
type Container struct {
	Name  string // 容器名称
	Image string // 容器镜像
}

// PodStatus 对应 corev1.PodStatus
type PodStatus struct {
	Phase   PodPhase // Pod 阶段
	PodIP   string   // Pod IP 地址
	Message string   // 状态消息
}

// PodList 对应 corev1.PodList，表示 Pod 列表
type PodList struct {
	Items           []Pod  // Pod 列表
	ResourceVersion string // 列表的资源版本
}

// ============================================================
// 第二部分：事件类型
// 对应 watch.Event，Informer 通过 Watch 接口接收变更事件
// ============================================================

// EventType 事件类型枚举
type EventType string

const (
	EventAdded    EventType = "ADDED"    // 新增资源
	EventModified EventType = "MODIFIED" // 修改资源
	EventDeleted  EventType = "DELETED"  // 删除资源
)

// WatchEvent 对应 watch.Event
type WatchEvent struct {
	Type   EventType // 事件类型
	Object Pod       // 关联的资源对象
}

// ============================================================
// 第三部分：Indexer（本地缓存）
// 对应 cache.Indexer / cache.Store
// Informer 将 API Server 的数据同步到本地缓存，减少对 API Server 的压力
// ============================================================

// Indexer 使用 sync.Map 实现线程安全的本地缓存
// 真实的 client-go 中，Indexer 还支持自定义索引函数
type Indexer struct {
	store sync.Map // key: namespace/name, value: Pod
}

// keyFunc 生成缓存键，格式为 "namespace/name"
// 对应 cache.MetaNamespaceKeyFunc
func keyFunc(pod Pod) string {
	return pod.Namespace + "/" + pod.Name
}

// Add 添加或更新对象到缓存
func (idx *Indexer) Add(pod Pod) {
	idx.store.Store(keyFunc(pod), pod)
}

// Delete 从缓存中删除对象
func (idx *Indexer) Delete(pod Pod) {
	idx.store.Delete(keyFunc(pod))
}

// Get 从缓存中获取对象
func (idx *Indexer) Get(key string) (Pod, bool) {
	val, ok := idx.store.Load(key)
	if !ok {
		return Pod{}, false
	}
	return val.(Pod), true
}

// List 列出缓存中的所有对象
func (idx *Indexer) List() []Pod {
	var pods []Pod
	idx.store.Range(func(key, value interface{}) bool {
		pods = append(pods, value.(Pod))
		return true
	})
	return pods
}

// ============================================================
// 第四部分：WorkQueue（工作队列）
// 对应 workqueue.RateLimitingInterface
//
// WorkQueue 的关键特性：
//   - 去重：同一个 key 不会被重复入队
//   - 速率限制：失败重试时使用退避策略
//   - 有序处理：FIFO 顺序
// ============================================================

// RateLimitingQueue 带速率限制的工作队列
type RateLimitingQueue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	queue      []string          // FIFO 队列
	dirty      map[string]bool   // 标记正在处理的 key，防止重复入队
	processing map[string]bool   // 正在处理中的 key
	failures   map[string]int    // 每个 key 的失败次数，用于计算退避时间
	shutdown   bool              // 是否已关闭
	baseDelay  time.Duration     // 基础重试延迟
	maxDelay   time.Duration     // 最大重试延迟
	delayed    map[string]func() // 延迟入队的取消函数
}

// NewRateLimitingQueue 创建新的速率限制队列
func NewRateLimitingQueue() *RateLimitingQueue {
	q := &RateLimitingQueue{
		dirty:      make(map[string]bool),
		processing: make(map[string]bool),
		failures:   make(map[string]int),
		baseDelay:  100 * time.Millisecond,
		maxDelay:   2 * time.Second,
		delayed:    make(map[string]func()),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Add 将 key 加入队列（去重）
func (q *RateLimitingQueue) Add(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.shutdown {
		return
	}

	// 如果 key 已在队列或正在处理中，跳过（去重）
	if q.dirty[key] {
		return
	}
	q.dirty[key] = true
	q.queue = append(q.queue, key)
	q.cond.Signal() // 通知等待的消费者
}

// AddRateLimited 按照速率限制延迟添加 key（用于失败重试）
func (q *RateLimitingQueue) AddRateLimited(key string) {
	q.mu.Lock()
	failures := q.failures[key]
	q.mu.Unlock()

	// 计算指数退避延迟：baseDelay * 2^failures，上限为 maxDelay
	delay := q.baseDelay
	for i := 0; i < failures; i++ {
		delay *= 2
		if delay > q.maxDelay {
			delay = q.maxDelay
			break
		}
	}

	fmt.Printf("    [队列] ⏳ key=%s 将在 %v 后重试（第 %d 次失败）\n", key, delay, failures)

	// 延迟入队
	go func() {
		time.Sleep(delay)
		q.Add(key)
	}()
}

// Get 从队列获取下一个 key（阻塞式）
func (q *RateLimitingQueue) Get() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.queue) == 0 && !q.shutdown {
		q.cond.Wait()
	}

	if q.shutdown && len(q.queue) == 0 {
		return "", true // shutdown 为 true 表示队列已关闭
	}

	key := q.queue[0]
	q.queue = q.queue[1:]
	q.processing[key] = true
	delete(q.dirty, key)
	return key, false
}

// Done 标记 key 处理完成
func (q *RateLimitingQueue) Done(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.processing, key)
}

// NumRequeues 返回 key 的重试次数
func (q *RateLimitingQueue) NumRequeues(key string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.failures[key]
}

// AddFailure 记录处理失败
func (q *RateLimitingQueue) AddFailure(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failures[key]++
}

// Forget 清除 key 的失败计数（处理成功后调用）
func (q *RateLimitingQueue) Forget(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.failures, key)
}

// ShutDown 关闭队列
func (q *RateLimitingQueue) ShutDown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shutdown = true
	q.cond.Broadcast()
}

// Len 返回队列当前长度
func (q *RateLimitingQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue)
}

// ============================================================
// 第五部分：ResourceEventHandler（事件处理器）
// 对应 cache.ResourceEventHandlerFuncs
// 由用户注册到 Informer 上，当资源发生变化时被回调
// ============================================================

// ResourceEventHandler 定义事件处理回调函数
type ResourceEventHandler struct {
	AddFunc    func(obj Pod)           // 新增资源时调用
	UpdateFunc func(oldObj, newObj Pod) // 更新资源时调用
	DeleteFunc func(obj Pod)           // 删除资源时调用
}

// ============================================================
// 第六部分：Informer（核心组件）
// 对应 cache.SharedInformer
//
// Informer 的工作流程：
//   1. List: 启动时获取所有资源的全量列表
//   2. Watch: 持续监听增量变更事件
//   3. 更新本地缓存（Indexer）
//   4. 触发注册的事件处理器
//
// 这避免了每次需要资源信息时都直接查询 API Server
// ============================================================

// Informer 模拟 SharedInformer 的核心功能
type Informer struct {
	indexer  *Indexer                // 本地缓存
	handlers []ResourceEventHandler // 注册的事件处理器
	eventCh  chan WatchEvent         // 模拟 Watch 事件流
	stopCh   chan struct{}           // 停止信号
}

// NewInformer 创建新的 Informer
func NewInformer() *Informer {
	return &Informer{
		indexer: &Indexer{},
		eventCh: make(chan WatchEvent, 100),
		stopCh:  make(chan struct{}),
	}
}

// AddEventHandler 注册事件处理器
// 对应 informer.AddEventHandler()
func (inf *Informer) AddEventHandler(handler ResourceEventHandler) {
	inf.handlers = append(inf.handlers, handler)
}

// GetIndexer 返回本地缓存
func (inf *Informer) GetIndexer() *Indexer {
	return inf.indexer
}

// Run 启动 Informer 的主循环
// 真实的 Informer 会先执行 List 获取全量数据，然后持续 Watch 增量变更
func (inf *Informer) Run(stopCh chan struct{}) {
	fmt.Println("[Informer] 启动 ListWatch 循环...")

	// 步骤 1：模拟 List（获取现有资源）
	fmt.Println("[Informer] 执行初始 List 同步...")
	initialPods := inf.listFromAPIServer()
	for _, pod := range initialPods {
		inf.indexer.Add(pod)
		inf.notifyHandlers(WatchEvent{Type: EventAdded, Object: pod})
	}
	fmt.Printf("[Informer] 初始同步完成，缓存了 %d 个 Pod\n", len(initialPods))

	// 步骤 2：模拟 Watch（持续监听变更）
	fmt.Println("[Informer] 开始 Watch 增量变更...")
	for {
		select {
		case event := <-inf.eventCh:
			inf.processEvent(event)
		case <-stopCh:
			fmt.Println("[Informer] 收到停止信号，退出 Watch 循环")
			return
		}
	}
}

// listFromAPIServer 模拟从 API Server 获取 Pod 列表
func (inf *Informer) listFromAPIServer() []Pod {
	return []Pod{
		{
			ObjectMeta: ObjectMeta{
				Name:            "nginx-abc123",
				Namespace:       "default",
				ResourceVersion: "100",
				Labels:          map[string]string{"app": "nginx", "tier": "frontend"},
			},
			Spec: PodSpec{
				Containers: []Container{{Name: "nginx", Image: "nginx:1.21"}},
				NodeName:   "node-1",
			},
			Status: PodStatus{Phase: PodRunning, PodIP: "10.0.0.1"},
		},
		{
			ObjectMeta: ObjectMeta{
				Name:            "redis-def456",
				Namespace:       "default",
				ResourceVersion: "101",
				Labels:          map[string]string{"app": "redis", "tier": "backend"},
			},
			Spec: PodSpec{
				Containers: []Container{{Name: "redis", Image: "redis:7.0"}},
				NodeName:   "node-2",
			},
			Status: PodStatus{Phase: PodRunning, PodIP: "10.0.0.2"},
		},
	}
}

// processEvent 处理单个 Watch 事件：更新缓存并通知处理器
func (inf *Informer) processEvent(event WatchEvent) {
	switch event.Type {
	case EventAdded:
		inf.indexer.Add(event.Object)
	case EventModified:
		inf.indexer.Add(event.Object) // 更新也是 Add（覆盖）
	case EventDeleted:
		inf.indexer.Delete(event.Object)
	}
	inf.notifyHandlers(event)
}

// notifyHandlers 通知所有注册的事件处理器
func (inf *Informer) notifyHandlers(event WatchEvent) {
	for _, handler := range inf.handlers {
		switch event.Type {
		case EventAdded:
			if handler.AddFunc != nil {
				handler.AddFunc(event.Object)
			}
		case EventModified:
			if handler.UpdateFunc != nil {
				// 简化：这里用同一个对象同时作为 old 和 new
				handler.UpdateFunc(event.Object, event.Object)
			}
		case EventDeleted:
			if handler.DeleteFunc != nil {
				handler.DeleteFunc(event.Object)
			}
		}
	}
}

// InjectEvent 向 Informer 注入模拟事件（替代真实的 API Server Watch）
func (inf *Informer) InjectEvent(event WatchEvent) {
	inf.eventCh <- event
}

// ============================================================
// 第七部分：Controller（控制器）
// 对应 controller-runtime 中的 Reconciler 模式
//
// 控制器的核心职责：
//   1. 从 WorkQueue 获取待处理的 key
//   2. 从 Indexer（缓存）获取资源的当前状态
//   3. 执行调谐逻辑（使实际状态趋向期望状态）
//   4. 处理错误和重试
//
// 这就是 K8s 声明式 API 的核心：
//   用户声明期望状态 → 控制器不断调谐 → 实际状态 = 期望状态
// ============================================================

// Controller 模拟 K8s 控制器
type Controller struct {
	name     string             // 控制器名称
	informer *Informer          // 关联的 Informer
	queue    *RateLimitingQueue // 工作队列
}

// NewController 创建新的控制器
func NewController(name string, informer *Informer, queue *RateLimitingQueue) *Controller {
	return &Controller{
		name:     name,
		informer: informer,
		queue:    queue,
	}
}

// Run 启动控制器的工作循环
// workerCount 指定并发 worker 数量
func (c *Controller) Run(workerCount int, stopCh chan struct{}) {
	fmt.Printf("[控制器:%s] 启动 %d 个 worker\n", c.name, workerCount)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fmt.Printf("  [Worker-%d] 启动\n", id)
			c.runWorker(id)
			fmt.Printf("  [Worker-%d] 退出\n", id)
		}(i)
	}

	<-stopCh
	fmt.Printf("[控制器:%s] 收到停止信号，关闭工作队列...\n", c.name)
	c.queue.ShutDown()
	wg.Wait()
	fmt.Printf("[控制器:%s] 所有 worker 已退出\n", c.name)
}

// runWorker 单个 worker 的处理循环
func (c *Controller) runWorker(id int) {
	for {
		key, shutdown := c.queue.Get()
		if shutdown {
			return
		}
		c.processItem(id, key)
	}
}

// processItem 处理单个工作项
func (c *Controller) processItem(workerID int, key string) {
	defer c.queue.Done(key)

	// 调用调谐逻辑
	err := c.reconcile(workerID, key)
	if err != nil {
		// 处理失败，检查重试次数
		if c.queue.NumRequeues(key) < 3 {
			fmt.Printf("  [Worker-%d] ❌ 调谐失败: key=%s, err=%v, 将重试\n", workerID, key, err)
			c.queue.AddFailure(key)
			c.queue.AddRateLimited(key) // 按速率限制重新入队
			return
		}
		// 超过最大重试次数，放弃处理
		fmt.Printf("  [Worker-%d] ⛔ 放弃处理: key=%s（已重试 %d 次）\n",
			workerID, key, c.queue.NumRequeues(key))
		c.queue.Forget(key)
		return
	}

	// 处理成功，清除失败计数
	c.queue.Forget(key)
}

// reconcile 执行调谐逻辑（核心业务逻辑所在）
// 在真实的 Operator 中，这里会比较期望状态和实际状态，然后执行必要的操作
func (c *Controller) reconcile(workerID int, key string) error {
	fmt.Printf("  [Worker-%d] 🔄 开始调谐: key=%s\n", workerID, key)

	// 从本地缓存获取对象（而非直接访问 API Server）
	pod, exists := c.informer.GetIndexer().Get(key)
	if !exists {
		// 对象已被删除，执行清理逻辑
		fmt.Printf("  [Worker-%d] 🗑️  对象已删除: key=%s，执行清理\n", workerID, key)
		return nil
	}

	// 模拟调谐逻辑：检查 Pod 状态并采取行动
	fmt.Printf("  [Worker-%d] 📋 Pod 状态: name=%s, phase=%s, ip=%s\n",
		workerID, pod.Name, pod.Status.Phase, pod.Status.PodIP)

	// 模拟偶发错误（演示重试机制）
	if rand.Intn(5) == 0 {
		return fmt.Errorf("模拟的临时错误")
	}

	fmt.Printf("  [Worker-%d] ✅ 调谐成功: key=%s\n", workerID, key)

	// 模拟处理耗时
	time.Sleep(100 * time.Millisecond)
	return nil
}

// ============================================================
// 主函数：组装并运行完整的事件处理流水线
// ============================================================

func main() {
	fmt.Println("=== Kubernetes client-go 模式模拟 ===")
	fmt.Println()

	// 固定随机种子以获得可复现的输出
	rand.New(rand.NewSource(42))

	// 打印架构概览
	printArchitecture()

	// ===== 第一步：创建核心组件 =====
	fmt.Println("--- 第一步：创建核心组件 ---")
	informer := NewInformer()
	queue := NewRateLimitingQueue()
	controller := NewController("pod-controller", informer, queue)
	fmt.Println()

	// ===== 第二步：注册事件处理器 =====
	// 事件处理器的职责是将资源的 key 放入工作队列
	// 真实代码中不应在处理器中执行耗时操作
	fmt.Println("--- 第二步：注册事件处理器 ---")
	informer.AddEventHandler(ResourceEventHandler{
		AddFunc: func(pod Pod) {
			key := keyFunc(pod)
			fmt.Printf("  [事件处理器] ➕ 收到 ADD 事件: %s (phase=%s)\n", key, pod.Status.Phase)
			queue.Add(key) // 将 key 放入工作队列，而非对象本身
		},
		UpdateFunc: func(oldPod, newPod Pod) {
			key := keyFunc(newPod)
			fmt.Printf("  [事件处理器] 🔄 收到 UPDATE 事件: %s (phase=%s)\n", key, newPod.Status.Phase)
			queue.Add(key)
		},
		DeleteFunc: func(pod Pod) {
			key := keyFunc(pod)
			fmt.Printf("  [事件处理器] ➖ 收到 DELETE 事件: %s\n", key)
			queue.Add(key)
		},
	})
	fmt.Println("  已注册 AddFunc / UpdateFunc / DeleteFunc")
	fmt.Println()

	// ===== 第三步：启动 Informer 和 Controller =====
	fmt.Println("--- 第三步：启动 Informer 和 Controller ---")
	stopCh := make(chan struct{})

	// 启动 Informer（后台）
	go informer.Run(stopCh)

	// 等待初始同步完成
	time.Sleep(500 * time.Millisecond)
	fmt.Println()

	// 启动 Controller（后台）
	go controller.Run(2, stopCh) // 2 个并发 worker

	// 等待初始 Pod 被处理
	time.Sleep(1 * time.Second)
	fmt.Println()

	// ===== 第四步：模拟 API Server 发送的事件 =====
	fmt.Println("--- 第四步：模拟运行时事件 ---")
	fmt.Println()

	// 模拟新 Pod 创建
	fmt.Println(">>> 模拟事件：创建新 Pod")
	informer.InjectEvent(WatchEvent{
		Type: EventAdded,
		Object: Pod{
			ObjectMeta: ObjectMeta{
				Name:            "app-xyz789",
				Namespace:       "production",
				ResourceVersion: "200",
				Labels:          map[string]string{"app": "myapp", "version": "v2"},
			},
			Spec: PodSpec{
				Containers: []Container{{Name: "app", Image: "myapp:v2.0"}},
				NodeName:   "node-3",
			},
			Status: PodStatus{Phase: PodRunning, PodIP: "10.0.1.5"},
		},
	})
	time.Sleep(500 * time.Millisecond)
	fmt.Println()

	// 模拟 Pod 更新（滚动更新场景）
	fmt.Println(">>> 模拟事件：更新 Pod（镜像升级）")
	informer.InjectEvent(WatchEvent{
		Type: EventModified,
		Object: Pod{
			ObjectMeta: ObjectMeta{
				Name:            "nginx-abc123",
				Namespace:       "default",
				ResourceVersion: "300",
				Labels:          map[string]string{"app": "nginx", "tier": "frontend"},
			},
			Spec: PodSpec{
				Containers: []Container{{Name: "nginx", Image: "nginx:1.25"}}, // 镜像升级
				NodeName:   "node-1",
			},
			Status: PodStatus{Phase: PodRunning, PodIP: "10.0.0.1"},
		},
	})
	time.Sleep(500 * time.Millisecond)
	fmt.Println()

	// 模拟 Pod 删除
	fmt.Println(">>> 模拟事件：删除 Pod")
	informer.InjectEvent(WatchEvent{
		Type: EventDeleted,
		Object: Pod{
			ObjectMeta: ObjectMeta{
				Name:      "redis-def456",
				Namespace: "default",
			},
		},
	})
	time.Sleep(500 * time.Millisecond)
	fmt.Println()

	// ===== 第五步：打印缓存状态 =====
	fmt.Println("--- 第五步：检查本地缓存状态 ---")
	pods := informer.GetIndexer().List()
	fmt.Printf("  缓存中共 %d 个 Pod:\n", len(pods))
	for _, pod := range pods {
		fmt.Printf("    • %s/%s (phase=%s, image=%s)\n",
			pod.Namespace, pod.Name, pod.Status.Phase,
			pod.Spec.Containers[0].Image)
	}
	fmt.Println()

	// ===== 第六步：关闭 =====
	fmt.Println("--- 第六步：优雅关闭 ---")
	close(stopCh)
	time.Sleep(500 * time.Millisecond)

	// 打印总结
	fmt.Println()
	printSummary()
}

// printArchitecture 打印 client-go 架构图
func printArchitecture() {
	fmt.Println("client-go 事件处理架构：")
	fmt.Println()
	fmt.Println("  ┌──────────────┐    List/Watch     ┌────────────┐")
	fmt.Println("  │  API Server  │ ────────────────→  │  Informer  │")
	fmt.Println("  └──────────────┘                    └─────┬──────┘")
	fmt.Println("                                            │")
	fmt.Println("                                    ┌───────▼───────┐")
	fmt.Println("                                    │   Indexer     │")
	fmt.Println("                                    │ （本地缓存）  │")
	fmt.Println("                                    └───────┬───────┘")
	fmt.Println("                                            │")
	fmt.Println("                                  事件回调（Add/Update/Delete）")
	fmt.Println("                                            │")
	fmt.Println("                                    ┌───────▼───────┐")
	fmt.Println("                                    │  WorkQueue    │")
	fmt.Println("                                    │（速率限制队列）│")
	fmt.Println("                                    └───────┬───────┘")
	fmt.Println("                                            │")
	fmt.Println("                                    ┌───────▼───────┐")
	fmt.Println("                                    │  Controller   │")
	fmt.Println("                                    │（调谐循环）    │")
	fmt.Println("                                    └───────────────┘")
	fmt.Println()
}

// printSummary 打印 client-go 模式总结
func printSummary() {
	fmt.Println("=== client-go 核心模式总结 ===")
	fmt.Println()

	summaries := []struct {
		component   string
		realPkg     string
		description string
	}{
		{"Informer", "client-go/tools/cache", "ListWatch + 本地缓存 + 事件分发"},
		{"Indexer", "client-go/tools/cache", "线程安全的本地缓存，支持自定义索引"},
		{"WorkQueue", "client-go/util/workqueue", "去重 + 速率限制 + FIFO 工作队列"},
		{"Controller", "controller-runtime", "从队列获取 key → 从缓存获取对象 → 调谐"},
		{"Reconcile", "controller-runtime", "核心业务逻辑：使实际状态趋向期望状态"},
	}

	// 格式化打印
	maxComp := 0
	maxPkg := 0
	for _, s := range summaries {
		if len(s.component) > maxComp {
			maxComp = len(s.component)
		}
		if len(s.realPkg) > maxPkg {
			maxPkg = len(s.realPkg)
		}
	}

	header := fmt.Sprintf("  %-*s  %-*s  %s", maxComp, "组件", maxPkg, "真实包", "说明")
	fmt.Println(header)
	fmt.Println("  " + strings.Repeat("-", len(header)))
	for _, s := range summaries {
		fmt.Printf("  %-*s  %-*s  %s\n", maxComp, s.component, maxPkg, s.realPkg, s.description)
	}

	fmt.Println()
	fmt.Println("  关键设计原则：")
	fmt.Println("  1. 声明式 API：用户声明期望状态，控制器负责调谐")
	fmt.Println("  2. Level-triggered：基于当前状态调谐，而非基于事件顺序")
	fmt.Println("  3. 本地缓存：减少对 API Server 的请求压力")
	fmt.Println("  4. 速率限制：避免故障时的雪崩效应")
}
