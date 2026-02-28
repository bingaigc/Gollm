// Package main 演示 Kubernetes 基础概念
//
// 本示例用纯 Go 标准库模拟 K8s 核心对象模型：
// 1. 声明式 API 模型 — Spec/Status 分离，期望状态 vs 实际状态
// 2. 核心资源类型 — Go 结构体模拟 Pod, Service, Deployment, ConfigMap, Namespace
// 3. 标签选择器 (Label Selector) — matchLabels 过滤逻辑
// 4. 资源 CRUD — 模拟 etcd 存储，实现创建、读取、更新、删除
// 5. 控制器模式 — 调谐循环 (Reconcile Loop)
// 6. 资源版本与乐观并发 — resourceVersion 实现 CAS 更新
package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ========== 第一部分：声明式 API — Spec/Status 分离 | 第二部分：核心资源类型 ==========

// ObjectMeta 模拟 K8s 对象的通用元数据
type ObjectMeta struct {
	Name            string
	Namespace       string
	Labels          map[string]string // 标签：用于分组和选择
	Annotations     map[string]string // 注解
	ResourceVersion int64             // 用于乐观并发控制
	CreationTime    time.Time
}

// ContainerSpec 描述容器的期望配置
type ContainerSpec struct {
	Name, Image string
	Command     []string          // 启动命令
	Env         map[string]string // 环境变量
	CPUReq      string            // CPU 请求量
	MemReq      string            // 内存请求量
}

// PodSpec 声明 Pod 的期望状态
type PodSpec struct {
	Containers    []ContainerSpec
	RestartPolicy string // Always / OnFailure / Never
	NodeName      string
}

// PodStatus 记录 Pod 的实际运行状态
type PodStatus struct {
	Phase, PodIP, Message string
	StartTime             time.Time
}

// Pod 是 K8s 最小部署单元
type Pod struct {
	ObjectMeta
	Spec   PodSpec
	Status PodStatus
}

// ServicePort 定义服务端口映射
type ServicePort struct {
	Name       string
	Port       int
	TargetPort int
	Protocol   string
}

// Service 为 Pod 提供稳定的网络端点
type Service struct {
	ObjectMeta
	Type      string // ClusterIP / NodePort / LoadBalancer
	Selector  map[string]string
	ClusterIP string
	Ports     []ServicePort
}

type DeploymentStatus struct{ ReadyReplicas, AvailableReplicas, UpdatedReplicas int }

// Deployment 管理 Pod 的副本数和滚动更新
type Deployment struct {
	ObjectMeta
	Replicas int
	Selector map[string]string
	Template PodSpec
	Strategy string // RollingUpdate / Recreate
	Status   DeploymentStatus
}

// ConfigMap 存储非机密配置数据
type ConfigMap struct {
	ObjectMeta
	Data map[string]string
}

// Namespace 提供资源的逻辑隔离
type Namespace struct {
	ObjectMeta
	Phase string // Active / Terminating
}

// ========== 第三部分：标签选择器 ==========

// LabelSelector 实现标签匹配逻辑（AND 语义）
type LabelSelector struct {
	MatchLabels map[string]string
}

// Matches 检查给定的标签集是否满足选择器条件
func (ls *LabelSelector) Matches(labels map[string]string) bool {
	if labels == nil {
		return len(ls.MatchLabels) == 0
	}
	for key, value := range ls.MatchLabels {
		if labels[key] != value {
			return false
		}
	}
	return true
}

// ========== 第四部分：资源 CRUD — 模拟 etcd 存储 ==========

type ResourceKind string

const (
	KindPod        ResourceKind = "Pod"
	KindService    ResourceKind = "Service"
	KindDeployment ResourceKind = "Deployment"
	KindConfigMap  ResourceKind = "ConfigMap"
)

type StoredObject struct {
	Kind    ResourceKind
	Data    interface{}
	Version int64 // 资源版本号
}

// EtcdStore 模拟 etcd 键值存储
type EtcdStore struct {
	mu             sync.RWMutex
	data           map[string]*StoredObject
	versionCounter int64
}

func NewEtcdStore() *EtcdStore { return &EtcdStore{data: make(map[string]*StoredObject)} }

func makeKey(kind ResourceKind, namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return fmt.Sprintf("/%s/%s/%s", kind, namespace, name)
}

func (s *EtcdStore) Create(kind ResourceKind, namespace, name string, obj interface{}) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := makeKey(kind, namespace, name)
	if _, exists := s.data[key]; exists {
		return 0, fmt.Errorf("资源已存在: %s", key)
	}
	s.versionCounter++
	s.data[key] = &StoredObject{Kind: kind, Data: obj, Version: s.versionCounter}
	return s.versionCounter, nil
}

func (s *EtcdStore) Get(kind ResourceKind, namespace, name string) (*StoredObject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := makeKey(kind, namespace, name)
	if obj, exists := s.data[key]; exists {
		return obj, nil
	}
	return nil, fmt.Errorf("资源不存在: %s", key)
}

func (s *EtcdStore) Update(kind ResourceKind, namespace, name string, expectedVersion int64, obj interface{}) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := makeKey(kind, namespace, name)
	existing, exists := s.data[key]
	if !exists {
		return 0, fmt.Errorf("资源不存在，无法更新: %s", key)
	}
	// 乐观并发：检查版本号是否匹配（0 表示跳过检查）
	if expectedVersion != 0 && existing.Version != expectedVersion {
		return existing.Version, fmt.Errorf("版本冲突: 期望 %d，实际 %d", expectedVersion, existing.Version)
	}
	s.versionCounter++
	s.data[key] = &StoredObject{Kind: kind, Data: obj, Version: s.versionCounter}
	return s.versionCounter, nil
}

func (s *EtcdStore) Delete(kind ResourceKind, namespace, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := makeKey(kind, namespace, name)
	if _, exists := s.data[key]; !exists {
		return fmt.Errorf("资源不存在，无法删除: %s", key)
	}
	delete(s.data, key)
	return nil
}

func (s *EtcdStore) List(kind ResourceKind, namespace string) []*StoredObject {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := fmt.Sprintf("/%s/%s/", kind, namespace)
	var results []*StoredObject
	for key, obj := range s.data {
		if strings.HasPrefix(key, prefix) {
			results = append(results, obj)
		}
	}
	return results
}

// ========== 第五部分：控制器模式 — 调谐循环 ==========

// ReconcileResult 表示调谐结果
type ReconcileResult struct {
	Requeue      bool
	RequeueAfter time.Duration
	Message      string
}
type DeploymentController struct{ store *EtcdStore }

func NewDeploymentController(store *EtcdStore) *DeploymentController {
	return &DeploymentController{store: store}
}

// Reconcile 执行调谐循环：对比期望与实际 Pod 数
func (dc *DeploymentController) Reconcile(namespace, deployName string) ReconcileResult {
	obj, err := dc.store.Get(KindDeployment, namespace, deployName)
	if err != nil {
		return ReconcileResult{Message: fmt.Sprintf("获取 Deployment 失败: %v", err)}
	}
	deploy := obj.Data.(*Deployment)
	selector := &LabelSelector{MatchLabels: deploy.Selector}
	var managedPods []*Pod
	for _, stored := range dc.store.List(KindPod, namespace) {
		pod := stored.Data.(*Pod)
		if selector.Matches(pod.Labels) {
			managedPods = append(managedPods, pod)
		}
	}
	current, desired := len(managedPods), deploy.Replicas
	switch {
	case current < desired:
		for i := 0; i < desired-current; i++ {
			podName := fmt.Sprintf("%s-pod-%d", deployName, current+i)
			newPod := &Pod{
				ObjectMeta: ObjectMeta{Name: podName, Namespace: namespace, Labels: copyLabels(deploy.Selector)},
				Spec:       deploy.Template,
				Status:     PodStatus{Phase: "Running", PodIP: fmt.Sprintf("10.244.0.%d", current+i+1), StartTime: time.Now()},
			}
			if _, cerr := dc.store.Create(KindPod, namespace, podName, newPod); cerr != nil {
				return ReconcileResult{Message: fmt.Sprintf("创建 Pod %s 失败: %v", podName, cerr), Requeue: true, RequeueAfter: time.Second}
			}
		}
		deploy.Status = DeploymentStatus{ReadyReplicas: desired, AvailableReplicas: desired, UpdatedReplicas: desired}
		dc.store.Update(KindDeployment, namespace, deployName, deploy.ResourceVersion, deploy)
		return ReconcileResult{Message: fmt.Sprintf("扩容: 创建了 %d 个 Pod，副本数 %d → %d", desired-current, current, desired)}
	case current > desired:
		for i := 0; i < current-desired; i++ {
			dc.store.Delete(KindPod, namespace, managedPods[current-1-i].Name)
		}
		deploy.Status.ReadyReplicas = desired
		deploy.Status.AvailableReplicas = desired
		dc.store.Update(KindDeployment, namespace, deployName, deploy.ResourceVersion, deploy)
		return ReconcileResult{Message: fmt.Sprintf("缩容: 删除了 %d 个 Pod，副本数 %d → %d", current-desired, current, desired)}
	default:
		return ReconcileResult{Message: fmt.Sprintf("状态一致: %d/%d 副本就绪", current, desired)}
	}
}

func copyLabels(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// ========== 第六部分：资源版本与乐观并发 ==========

type VersionedStore struct {
	mu             sync.Mutex
	data           map[string]versionedEntry
	versionCounter int64
}
type versionedEntry struct{ Version int64; Data interface{} }

func NewVersionedStore() *VersionedStore {
	return &VersionedStore{data: make(map[string]versionedEntry)}
}

func (vs *VersionedStore) Put(key string, value interface{}) int64 {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.versionCounter++
	vs.data[key] = versionedEntry{Version: vs.versionCounter, Data: value}
	return vs.versionCounter
}

// CAS 比较并交换，只有版本一致时才更新
func (vs *VersionedStore) CAS(key string, expectedVersion int64, newValue interface{}) (int64, bool) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	existing, exists := vs.data[key]
	if !exists || existing.Version != expectedVersion {
		if exists {
			return existing.Version, false
		}
		return 0, false
	}
	vs.versionCounter++
	vs.data[key] = versionedEntry{Version: vs.versionCounter, Data: newValue}
	return vs.versionCounter, true
}

func (vs *VersionedStore) GetVersioned(key string) (interface{}, int64, bool) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	obj, exists := vs.data[key]
	if !exists {
		return nil, 0, false
	}
	return obj.Data, obj.Version, true
}

// ========== 辅助函数 ==========

func printHeader(emoji, title string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 56))
	fmt.Printf("%s %s\n", emoji, title)
	fmt.Println(strings.Repeat("=", 56))
}

func printSubHeader(emoji, title string) {
	fmt.Println()
	fmt.Printf("--- %s %s ---\n", emoji, title)
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}

// ========== main ==========
func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║     📘 Day 22: Kubernetes 基础概念                      ║")
	fmt.Println("║     用 Go 模拟 K8s 核心对象模型                         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	// === 演示1：声明式 API ===
	printHeader("📋", "第一部分：声明式 API 模型 — Spec/Status 分离")
	fmt.Println("💡 K8s 核心哲学: Spec=期望状态 | Status=实际状态 | 控制器=调谐逻辑")

	demoPod := &Pod{
		ObjectMeta: ObjectMeta{
			Name: "web-server", Namespace: "production",
			Labels:       map[string]string{"app": "nginx", "tier": "frontend", "env": "prod"},
			Annotations:  map[string]string{"description": "生产环境前端 Web 服务器"},
			CreationTime: time.Now(), ResourceVersion: 1,
		},
		Spec: PodSpec{
			Containers: []ContainerSpec{{
				Name: "nginx", Image: "nginx:1.21",
				Command: []string{"nginx", "-g", "daemon off;"},
				Env: map[string]string{"ENV": "production"}, CPUReq: "200m", MemReq: "256Mi",
			}},
			RestartPolicy: "Always",
		},
		Status: PodStatus{Phase: "Pending", Message: "等待调度器分配节点..."},
	}
	printSubHeader("🔍", "初始状态（创建后）")
	fmt.Printf("  Spec 期望: 运行容器 %s (镜像 %s)\n", demoPod.Spec.Containers[0].Name, demoPod.Spec.Containers[0].Image)
	fmt.Printf("  Status 实际: Phase=%s (%s)\n", demoPod.Status.Phase, demoPod.Status.Message)
	fmt.Println("  ⚠️ 期望 ≠ 实际 → 控制器需要介入调谐")

	demoPod.Spec.NodeName = "node-01"
	demoPod.Status = PodStatus{Phase: "Running", PodIP: "10.244.1.5", StartTime: time.Now(), Message: "所有容器正常运行"}
	printSubHeader("✅", "调谐后状态")
	fmt.Printf("  Spec→运行在 %s | Status→Phase=%s, IP=%s\n", demoPod.Spec.NodeName, demoPod.Status.Phase, demoPod.Status.PodIP)
	fmt.Println("  ✅ 期望 == 实际 → 系统已收敛")

	// === 演示2：核心资源类型 ===
	printHeader("🏗️", "第二部分：核心资源类型")
	printSubHeader("📦", "Pod — 最小调度单元")
	fmt.Printf("  📦 Pod: %s/%s | %s | IP: %s | 镜像: %s\n",
		demoPod.Namespace, demoPod.Name, demoPod.Status.Phase, demoPod.Status.PodIP, demoPod.Spec.Containers[0].Image)
	fmt.Printf("     标签: %s\n", formatLabels(demoPod.Labels))
	printSubHeader("🌐", "Service — 服务发现与负载均衡")
	svc := &Service{
		ObjectMeta: ObjectMeta{Name: "web-service", Namespace: "production"},
		Type: "ClusterIP", Selector: map[string]string{"app": "nginx", "tier": "frontend"},
		ClusterIP: "10.96.0.100",
		Ports: []ServicePort{{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"}},
	}
	fmt.Printf("  🌐 Service: %s (类型: %s, ClusterIP: %s)\n", svc.Name, svc.Type, svc.ClusterIP)
	fmt.Printf("     选择器: %s | 端口: %s %d→%d/%s\n",
		formatLabels(svc.Selector), svc.Ports[0].Name, svc.Ports[0].Port, svc.Ports[0].TargetPort, svc.Ports[0].Protocol)
	printSubHeader("🚀", "Deployment — 声明式部署")
	deploy := &Deployment{
		ObjectMeta: ObjectMeta{Name: "web-deploy", Namespace: "production"},
		Replicas: 3, Selector: map[string]string{"app": "nginx", "tier": "frontend"},
		Template: PodSpec{Containers: []ContainerSpec{{Name: "nginx", Image: "nginx:1.21"}}, RestartPolicy: "Always"},
		Strategy: "RollingUpdate",
	}
	fmt.Printf("  🚀 Deployment: %s (副本: %d, 策略: %s)\n", deploy.Name, deploy.Replicas, deploy.Strategy)
	fmt.Printf("     选择器: %s\n", formatLabels(deploy.Selector))

	printSubHeader("📝", "ConfigMap — 配置管理")
	cm := &ConfigMap{
		ObjectMeta: ObjectMeta{Name: "app-config", Namespace: "production"},
		Data:       map[string]string{"database.host": "mysql.production.svc.cluster.local", "database.port": "3306", "log.level": "info"},
	}
	fmt.Printf("  📝 ConfigMap: %s\n", cm.Name)
	for _, k := range []string{"database.host", "database.port", "log.level"} {
		fmt.Printf("     %s = %s\n", k, cm.Data[k])
	}
	printSubHeader("📁", "Namespace — 资源隔离")
	for _, ns := range []Namespace{
		{ObjectMeta: ObjectMeta{Name: "default"}, Phase: "Active"},
		{ObjectMeta: ObjectMeta{Name: "production"}, Phase: "Active"},
		{ObjectMeta: ObjectMeta{Name: "kube-system"}, Phase: "Active"},
	} {
		fmt.Printf("  📁 Namespace: %-15s (状态: %s)\n", ns.Name, ns.Phase)
	}
	// === 演示3：标签选择器 ===
	printHeader("🏷️", "第三部分：标签选择器 (Label Selector)")
	fmt.Println("💡 标签选择器是 K8s 松耦合的核心机制")
	testPods := []*Pod{
		{ObjectMeta: ObjectMeta{Name: "web-1", Labels: map[string]string{"app": "nginx", "tier": "frontend", "env": "prod"}}},
		{ObjectMeta: ObjectMeta{Name: "api-1", Labels: map[string]string{"app": "api", "tier": "backend", "env": "prod"}}},
		{ObjectMeta: ObjectMeta{Name: "api-2", Labels: map[string]string{"app": "api", "tier": "backend", "env": "staging"}}},
		{ObjectMeta: ObjectMeta{Name: "db-1", Labels: map[string]string{"app": "mysql", "tier": "database", "env": "prod"}}},
	}
	for _, sel := range []struct{ desc string; labels map[string]string }{
		{"选择所有前端 Pod", map[string]string{"tier": "frontend"}},
		{"选择生产环境的 API Pod", map[string]string{"app": "api", "env": "prod"}},
		{"选择所有生产环境 Pod", map[string]string{"env": "prod"}},
	} {
		printSubHeader("🔎", sel.desc)
		ls := &LabelSelector{MatchLabels: sel.labels}
		fmt.Printf("  选择器: %s\n", formatLabels(sel.labels))
		var matched []string
		for _, pod := range testPods {
			if ls.Matches(pod.Labels) {
				matched = append(matched, pod.Name)
			}
		}
		if len(matched) > 0 {
			fmt.Printf("  匹配结果: %s\n", strings.Join(matched, ", "))
		} else {
			fmt.Println("  匹配结果: <无匹配>")
		}
	}

	// === 演示4：资源 CRUD ===
	printHeader("💾", "第四部分：资源 CRUD — 模拟 etcd 存储")
	store := NewEtcdStore()
	printSubHeader("➕", "创建 (Create)")
	v1, _ := store.Create(KindPod, "default", "nginx-pod-1", &Pod{
		ObjectMeta: ObjectMeta{Name: "nginx-pod-1", Namespace: "default", Labels: map[string]string{"app": "nginx"}},
		Spec:       PodSpec{Containers: []ContainerSpec{{Name: "nginx", Image: "nginx:1.21"}}},
	})
	fmt.Printf("  ✅ 创建 Pod/nginx-pod-1 成功 (版本: %d)\n", v1)
	v2, _ := store.Create(KindService, "default", "nginx-svc", &Service{
		ObjectMeta: ObjectMeta{Name: "nginx-svc", Namespace: "default"},
		Type: "ClusterIP", ClusterIP: "10.96.0.50",
	})
	fmt.Printf("  ✅ 创建 Service/nginx-svc 成功 (版本: %d)\n", v2)
	_, err := store.Create(KindPod, "default", "nginx-pod-1", nil)
	if err != nil {
		fmt.Printf("  ❌ 重复创建失败（符合预期）: %v\n", err)
	}
	printSubHeader("📖", "读取 (Get)")
	obj, err := store.Get(KindPod, "default", "nginx-pod-1")
	if err == nil {
		pod := obj.Data.(*Pod)
		fmt.Printf("  ✅ 读取成功: %s/%s (类型: %s)\n", pod.Namespace, pod.Name, obj.Kind)
	}
	_, err = store.Get(KindPod, "default", "not-exist")
	if err != nil {
		fmt.Printf("  ❌ 读取不存在的资源（符合预期）: %v\n", err)
	}
	printSubHeader("✏️", "更新 (Update)")
	updatedPod := &Pod{
		ObjectMeta: ObjectMeta{Name: "nginx-pod-1", Namespace: "default", Labels: map[string]string{"app": "nginx", "version": "v2"}},
		Spec: PodSpec{Containers: []ContainerSpec{{Name: "nginx", Image: "nginx:1.22"}}},
	}
	newVer, err := store.Update(KindPod, "default", "nginx-pod-1", 1, updatedPod)
	if err == nil {
		fmt.Printf("  ✅ 更新 Pod/nginx-pod-1 成功 (新版本: %d, 镜像: nginx:1.21→1.22)\n", newVer)
	}
	printSubHeader("🗑️", "删除 (Delete)")
	err = store.Delete(KindService, "default", "nginx-svc")
	if err == nil {
		fmt.Println("  ✅ 删除 Service/nginx-svc 成功")
	}
	_, err = store.Get(KindService, "default", "nginx-svc")
	if err != nil {
		fmt.Printf("  ✅ 验证: 已确认资源被删除 (%v)\n", err)
	}

	// === 演示5：控制器模式 ===
	printHeader("🔄", "第五部分：控制器模式 — 调谐循环 (Reconcile Loop)")
	fmt.Println("💡 控制器核心: 观测期望→观测实际→计算差异→执行调谐→重复")
	ctrlStore := NewEtcdStore()
	controller := NewDeploymentController(ctrlStore)
	ctrlDeploy := &Deployment{
		ObjectMeta: ObjectMeta{Name: "web-app", Namespace: "default", Labels: map[string]string{"app": "web"}, ResourceVersion: 1},
		Replicas: 3, Selector: map[string]string{"app": "web", "managed-by": "web-app"},
		Template: PodSpec{Containers: []ContainerSpec{{Name: "web", Image: "myapp:v1"}}, RestartPolicy: "Always"},
		Strategy: "RollingUpdate",
	}
	ctrlStore.Create(KindDeployment, "default", "web-app", ctrlDeploy)
	printSubHeader("🔄", "调谐循环 #1: 初始调谐（0 → 3 副本）")
	result := controller.Reconcile("default", "web-app")
	fmt.Printf("  📊 结果: %s\n", result.Message)
	for _, p := range ctrlStore.List(KindPod, "default") {
		pod := p.Data.(*Pod)
		fmt.Printf("     - %s (IP: %s, 状态: %s)\n", pod.Name, pod.Status.PodIP, pod.Status.Phase)
	}

	printSubHeader("🔄", "调谐循环 #2: 状态已一致")
	result = controller.Reconcile("default", "web-app")
	fmt.Printf("  📊 结果: %s\n", result.Message)
	printSubHeader("🔄", "调谐循环 #3: 模拟缩容（3 → 2 副本）")
	ctrlDeploy.Replicas = 2
	ctrlStore.Update(KindDeployment, "default", "web-app", 1, ctrlDeploy)
	result = controller.Reconcile("default", "web-app")
	fmt.Printf("  📊 结果: %s\n", result.Message)
	fmt.Printf("  📦 当前 Pod 数量: %d\n", len(ctrlStore.List(KindPod, "default")))

	// === 演示6：资源版本与乐观并发 ===
	printHeader("🔒", "第六部分：资源版本与乐观并发 (CAS)")
	fmt.Println("💡 乐观并发: 读取获取版本→修改后携带版本提交→版本不匹配则冲突重试")
	vStore := NewVersionedStore()
	ver1 := vStore.Put("config/replicas", 3)
	fmt.Printf("  写入 config/replicas = 3 (版本: %d)\n", ver1)

	printSubHeader("🔀", "模拟并发更新")
	val1, rv1, _ := vStore.GetVersioned("config/replicas")
	val2, rv2, _ := vStore.GetVersioned("config/replicas")
	fmt.Printf("  客户端 A 读取: value=%v, version=%d\n", val1, rv1)
	fmt.Printf("  客户端 B 读取: value=%v, version=%d\n", val2, rv2)
	newRV, ok := vStore.CAS("config/replicas", rv1, 5)
	if ok {
		fmt.Printf("  ✅ 客户端 A 更新成功: 3→5 (新版本: %d)\n", newRV)
	}
	_, ok = vStore.CAS("config/replicas", rv2, 10)
	if !ok {
		fmt.Println("  ❌ 客户端 B 更新失败: 版本冲突（符合预期）")
	}
	// 客户端 B 重试: Read-Modify-Write
	val3, rv3, _ := vStore.GetVersioned("config/replicas")
	fmt.Printf("  重试: 重新读取 value=%v, version=%d\n", val3, rv3)
	newRV, ok = vStore.CAS("config/replicas", rv3, 10)
	if ok {
		fmt.Printf("  ✅ 重试成功: 5→10 (新版本: %d)\n", newRV)
	}
	finalVal, finalVer, _ := vStore.GetVersioned("config/replicas")
	fmt.Printf("  📊 最终状态: value=%v, version=%d\n", finalVal, finalVer)

	// === 总结 ===
	printHeader("📚", "Day 22 总结")
	fmt.Println("🎯 Kubernetes 核心概念:")
	fmt.Println("  1️⃣  声明式 API: Spec/Status 分离  2️⃣  核心资源: Pod/Service/Deployment/ConfigMap/Namespace")
	fmt.Println("  3️⃣  标签选择器: 松耦合关联  4️⃣  CRUD: 统一 API + etcd")
	fmt.Println("  5️⃣  控制器模式: 观测+差异+调谐  6️⃣  乐观并发: resourceVersion + CAS")
	fmt.Println("✅ Day 22 Kubernetes 基础概念演示完成！")
}
