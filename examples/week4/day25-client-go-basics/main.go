// Package main 演示 Kubernetes client-go 的基础概念
//
// 本示例用纯 Go 标准库模拟 client-go 的核心机制：
//   - RESTful API 客户端：模拟 K8s API Server 的 CRUD 操作
//   - 资源序列化：JSON 编解码，GVR（Group-Version-Resource）概念
//   - 认证机制：Bearer Token / ServiceAccount 认证模式
//   - Watch 机制：长连接监听资源变更事件
//   - 分页列表：continue token 分页查询
//   - 错误处理：K8s API 标准错误格式（Status 对象）
//
// Day 26 在这些基础上讲解 Informer、WorkQueue、Controller 模式
package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 第一部分：GVR 与资源类型定义
// ============================================================

// GroupVersionResource K8s 资源的唯一坐标（Group + Version + Resource）
type GroupVersionResource struct {
	Group, Version, Resource string
}

// String 返回 API 路径：核心组 /api/v1/pods，扩展组 /apis/apps/v1/deployments
func (gvr GroupVersionResource) String() string {
	if gvr.Group == "" {
		return fmt.Sprintf("/api/%s/%s", gvr.Version, gvr.Resource)
	}
	return fmt.Sprintf("/apis/%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}

// ObjectMeta 对应 metav1.ObjectMeta —— 所有 K8s 资源的公共元数据
type ObjectMeta struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion,omitempty"` // 乐观并发控制
	Labels          map[string]string `json:"labels,omitempty"`
	UID             string            `json:"uid,omitempty"`
}

// Pod / Service 等核心资源
type Pod struct {
	Kind       string    `json:"kind"`
	APIVersion string    `json:"apiVersion"`
	ObjectMeta `json:"metadata"`
	Spec       PodSpec   `json:"spec"`
	Status     PodStatus `json:"status,omitempty"`
}
type PodSpec struct {
	Containers []Container `json:"containers"`
}
type Container struct {
	Name, Image string
}
type PodStatus struct {
	Phase, PodIP string
}
type Service struct {
	Kind       string      `json:"kind"`
	APIVersion string      `json:"apiVersion"`
	ObjectMeta `json:"metadata"`
	Spec       ServiceSpec `json:"spec"`
}
type ServiceSpec struct {
	Type     string            `json:"type"`
	Selector map[string]string `json:"selector,omitempty"`
	Ports    []ServicePort     `json:"ports"`
}
type ServicePort struct {
	Port, TargetPort int
	Protocol         string
}

// ============================================================
// 第二部分：Status 错误对象 —— K8s API 标准错误格式
// ============================================================

type Status struct {
	Code       int    `json:"code"`
	StatusText string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message"`
}

func (s *Status) Error() string { return fmt.Sprintf("[%d %s] %s", s.Code, s.Reason, s.Message) }

func errNotFound(r, n string) *Status {
	return &Status{404, "Failure", "NotFound", fmt.Sprintf("%s %q not found", r, n)}
}
func errConflict(r, n string) *Status {
	return &Status{409, "Failure", "Conflict", fmt.Sprintf("conflict on %s %q: object modified", r, n)}
}
func errExists(r, n string) *Status {
	return &Status{409, "Failure", "AlreadyExists", fmt.Sprintf("%s %q already exists", r, n)}
}
func errUnauth(m string) *Status { return &Status{401, "Failure", "Unauthorized", m} }

// ============================================================
// 第三部分：认证机制
// ============================================================

// AuthProvider 认证接口 —— client-go 通过 rest.Config 注入认证信息
type AuthProvider interface {
	Headers() map[string]string
	Validate() *Status
}

// BearerTokenAuth 来自 kubeconfig 或命令行 --token
type BearerTokenAuth struct{ Token string }

func (b *BearerTokenAuth) Headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + b.Token}
}
func (b *BearerTokenAuth) Validate() *Status {
	if b.Token == "" {
		return errUnauth("bearer token is empty")
	}
	if !strings.HasPrefix(b.Token, "valid-") {
		return errUnauth("invalid bearer token")
	}
	return nil
}

// ServiceAccountAuth Pod 内自动挂载的 SA Token
type ServiceAccountAuth struct{ Name, Namespace, Token string }

func (sa *ServiceAccountAuth) Headers() map[string]string {
	return map[string]string{
		"Authorization":    "Bearer " + sa.Token,
		"Impersonate-User": fmt.Sprintf("system:serviceaccount:%s:%s", sa.Namespace, sa.Name),
	}
}
func (sa *ServiceAccountAuth) Validate() *Status {
	if sa.Token == "" {
		return errUnauth("service account token not found")
	}
	return nil
}

// ============================================================
// 第四部分：模拟 API Server（CRUD + Watch + 分页）
// ============================================================

// WatchEvent ADDED / MODIFIED / DELETED 事件
type WatchEvent struct {
	Type   string
	Object Pod
}

// APIServer 模拟 K8s API Server 的核心功能
type APIServer struct {
	mu         sync.RWMutex
	pods       map[string]Pod
	rv         int
	watchChans []chan WatchEvent
}

func newServer() *APIServer           { return &APIServer{pods: make(map[string]Pod)} }
func (s *APIServer) nextRV() string   { s.rv++; return strconv.Itoa(s.rv) }
func pkey(ns, name string) string     { return ns + "/" + name }

func (s *APIServer) broadcast(e WatchEvent) {
	for _, ch := range s.watchChans {
		select {
		case ch <- e:
		default:
		}
	}
}

// CreatePod 对应 POST /api/v1/namespaces/{ns}/pods
func (s *APIServer) CreatePod(p Pod) (Pod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := pkey(p.Namespace, p.Name)
	if _, ok := s.pods[k]; ok {
		return Pod{}, errExists("pods", p.Name)
	}
	p.ResourceVersion, p.UID = s.nextRV(), "uid-"+p.Namespace+"-"+p.Name
	p.Kind, p.APIVersion = "Pod", "v1"
	if p.Status.Phase == "" {
		p.Status.Phase = "Pending"
	}
	s.pods[k] = p
	s.broadcast(WatchEvent{"ADDED", p})
	return p, nil
}

// GetPod 对应 GET /api/v1/namespaces/{ns}/pods/{name}
func (s *APIServer) GetPod(ns, name string) (Pod, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pods[pkey(ns, name)]
	if !ok {
		return Pod{}, errNotFound("pods", name)
	}
	return p, nil
}

// UpdatePod 对应 PUT —— ResourceVersion 必须匹配（乐观并发控制）
func (s *APIServer) UpdatePod(p Pod) (Pod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := pkey(p.Namespace, p.Name)
	old, ok := s.pods[k]
	if !ok {
		return Pod{}, errNotFound("pods", p.Name)
	}
	if p.ResourceVersion != old.ResourceVersion {
		return Pod{}, errConflict("pods", p.Name)
	}
	p.ResourceVersion, p.UID = s.nextRV(), old.UID
	s.pods[k] = p
	s.broadcast(WatchEvent{"MODIFIED", p})
	return p, nil
}

// DeletePod 对应 DELETE /api/v1/namespaces/{ns}/pods/{name}
func (s *APIServer) DeletePod(ns, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := pkey(ns, name)
	p, ok := s.pods[k]
	if !ok {
		return errNotFound("pods", name)
	}
	delete(s.pods, k)
	s.broadcast(WatchEvent{"DELETED", p})
	return nil
}

// PodList 分页列表结果
type PodList struct {
	Items    []Pod  `json:"items"`
	Continue string `json:"continue,omitempty"` // 下一页的游标
	Total    int    `json:"total"`
}

// ListPods 分页列出 Pod（limit + continue token + 标签选择器）
func (s *APIServer) ListPods(ns string, limit int, cont string, sel map[string]string) PodList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []Pod
	for _, p := range s.pods {
		if ns != "" && p.Namespace != ns {
			continue
		}
		if len(sel) > 0 {
			ok := true
			for k, v := range sel {
				if p.Labels[k] != v {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		all = append(all, p)
	}
	start := 0
	if cont != "" {
		if i, err := strconv.Atoi(cont); err == nil && i < len(all) {
			start = i
		}
	}
	if limit <= 0 {
		limit = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	r := PodList{Items: all[start:end], Total: len(all)}
	if end < len(all) {
		r.Continue = strconv.Itoa(end)
	}
	return r
}

// Watch 创建事件监听通道（对应 GET ?watch=true）
func (s *APIServer) Watch() chan WatchEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan WatchEvent, 50)
	s.watchChans = append(s.watchChans, ch)
	return ch
}

// ============================================================
// 演示函数
// ============================================================

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║        📦 Day 25: client-go 入门 — K8s API 客户端基础        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	demoGVR()
	demoSerialization()
	demoAuth()
	demoCRUD()
	demoWatch()
	demoPagination()
	demoErrors()

	fmt.Println("🎓 Day 25 总结")
	fmt.Println(strings.Repeat("=", 55))
	fmt.Println("✅ GVR 是 K8s 资源的唯一坐标（Group/Version/Resource）")
	fmt.Println("✅ RESTful CRUD 是 client-go 与 API Server 交互的基础")
	fmt.Println("✅ Bearer Token / ServiceAccount 是两种主要认证方式")
	fmt.Println("✅ Watch 通过长连接实时推送 ADDED/MODIFIED/DELETED 事件")
	fmt.Println("✅ 分页列表使用 limit + continue token 遍历大量资源")
	fmt.Println("✅ Status 对象是 K8s API 的标准错误格式")
	fmt.Println()
	fmt.Println("📌 下一步: Day 26 讲解 Informer、WorkQueue、Controller")
}

func demoGVR() {
	fmt.Println("📌 1. GVR（Group-Version-Resource）概念")
	fmt.Println(strings.Repeat("─", 55))
	gvrs := []struct {
		n string
		g GroupVersionResource
	}{
		{"Pod", GroupVersionResource{"", "v1", "pods"}},
		{"Service", GroupVersionResource{"", "v1", "services"}},
		{"Deployment", GroupVersionResource{"apps", "v1", "deployments"}},
		{"Ingress", GroupVersionResource{"networking.k8s.io", "v1", "ingresses"}},
	}
	for _, x := range gvrs {
		fmt.Printf("  %-12s → %s\n", x.n, x.g.String())
	}
	fmt.Println("核心组 Group 为空 → /api/v1/...  扩展组 → /apis/{group}/v1/...")
	fmt.Println()
}

func demoSerialization() {
	fmt.Println("📌 2. 资源序列化 — JSON 编解码")
	fmt.Println(strings.Repeat("─", 55))

	pod := Pod{Kind: "Pod", APIVersion: "v1",
		ObjectMeta: ObjectMeta{Name: "web", Namespace: "default",
			Labels: map[string]string{"app": "web"}},
		Spec:   PodSpec{Containers: []Container{{Name: "nginx", Image: "nginx:1.25"}}},
		Status: PodStatus{Phase: "Running", PodIP: "10.244.1.5"},
	}
	data, _ := json.MarshalIndent(pod, "", "  ")
	fmt.Println("Pod → JSON:")
	fmt.Println(string(data))
	var back Pod
	_ = json.Unmarshal(data, &back)
	fmt.Printf("JSON → Pod: name=%s image=%s phase=%s\n", back.Name,
		back.Spec.Containers[0].Image, back.Status.Phase)
	fmt.Println()

	svc := Service{Kind: "Service", APIVersion: "v1",
		ObjectMeta: ObjectMeta{Name: "web-svc", Namespace: "default"},
		Spec: ServiceSpec{Type: "ClusterIP", Selector: map[string]string{"app": "web"},
			Ports: []ServicePort{{Port: 80, TargetPort: 8080, Protocol: "TCP"}}}}
	sd, _ := json.MarshalIndent(svc, "", "  ")
	fmt.Println("Service → JSON:")
	fmt.Println(string(sd))
	fmt.Println()
}

func demoAuth() {
	fmt.Println("📌 3. 认证机制 — Bearer Token & ServiceAccount")
	fmt.Println(strings.Repeat("─", 55))

	ok := &BearerTokenAuth{Token: "valid-abc123"}
	if err := ok.Validate(); err == nil {
		fmt.Printf("  ✅ Token 验证通过  头: %v\n", ok.Headers())
	}
	bad := &BearerTokenAuth{Token: "expired-999"}
	if err := bad.Validate(); err != nil {
		fmt.Printf("  ❌ Token 失败: %v\n", err)
	}

	sa := &ServiceAccountAuth{Name: "ctrl", Namespace: "kube-system", Token: "eyJhbGci..."}
	if err := sa.Validate(); err == nil {
		fmt.Printf("  ✅ SA 验证通过  头: %v\n", sa.Headers())
	}
	fmt.Println("  流程: kubeconfig → rest.Config → 自动注入 Authorization 头")
	fmt.Println()
}

func demoCRUD() {
	fmt.Println("📌 4. CRUD 操作 — 完整资源生命周期")
	fmt.Println(strings.Repeat("─", 55))

	s := newServer()
	p, _ := s.CreatePod(Pod{
		ObjectMeta: ObjectMeta{Name: "nginx", Namespace: "default",
			Labels: map[string]string{"app": "nginx"}},
		Spec: PodSpec{Containers: []Container{{Name: "nginx", Image: "nginx:1.25"}}}})
	fmt.Printf("  CREATE ✅ name=%s rv=%s uid=%s\n", p.Name, p.ResourceVersion, p.UID)

	g, _ := s.GetPod("default", "nginx")
	fmt.Printf("  GET    ✅ phase=%s rv=%s\n", g.Status.Phase, g.ResourceVersion)

	g.Status = PodStatus{Phase: "Running", PodIP: "10.244.1.10"}
	u, _ := s.UpdatePod(g)
	fmt.Printf("  UPDATE ✅ phase=%s ip=%s rv=%s→%s\n",
		u.Status.Phase, u.Status.PodIP, g.ResourceVersion, u.ResourceVersion)

	g.Status.Phase = "Failed" // 旧 rv → 冲突
	_, err := s.UpdatePod(g)
	fmt.Printf("  UPDATE ⚠️  冲突: %v\n", err)

	_ = s.DeletePod("default", "nginx")
	fmt.Println("  DELETE ✅ 已删除")
	fmt.Println()
}

func demoWatch() {
	fmt.Println("📌 5. Watch 机制 — 实时监听变更事件")
	fmt.Println(strings.Repeat("─", 55))
	fmt.Println("  原理: GET /api/v1/pods?watch=true 长连接推送事件")

	s := newServer()
	ch := s.Watch()
	var wg sync.WaitGroup
	var evts []string
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 4; i++ {
			e := <-ch
			mu.Lock()
			evts = append(evts, fmt.Sprintf("  📡 %-10s %s/%s rv=%s",
				e.Type, e.Object.Namespace, e.Object.Name, e.Object.ResourceVersion))
			mu.Unlock()
		}
	}()

	p, _ := s.CreatePod(Pod{ObjectMeta: ObjectMeta{Name: "demo", Namespace: "ns"},
		Spec: PodSpec{Containers: []Container{{Name: "a", Image: "a:v1"}}}})
	p.Status.Phase = "Running"
	p, _ = s.UpdatePod(p)
	p.Spec.Containers[0].Image = "a:v2"
	p, _ = s.UpdatePod(p)
	_ = s.DeletePod("ns", "demo")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
	mu.Lock()
	for _, e := range evts {
		fmt.Println(e)
	}
	mu.Unlock()
	fmt.Println()
}

func demoPagination() {
	fmt.Println("📌 6. 分页列表 — Limit & Continue Token")
	fmt.Println(strings.Repeat("─", 55))

	s := newServer()
	for _, n := range []string{"web", "api", "db", "cache", "worker", "sched", "gw"} {
		_, _ = s.CreatePod(Pod{
			ObjectMeta: ObjectMeta{Name: n, Namespace: "prod",
				Labels: map[string]string{"app": n}},
			Spec: PodSpec{Containers: []Container{{Name: n, Image: n + ":1"}}}})
	}
	cont := ""
	for pg := 1; ; pg++ {
		r := s.ListPods("prod", 3, cont, nil)
		fmt.Printf("  第%d页 (%d/%d): ", pg, len(r.Items), r.Total)
		for i, p := range r.Items {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(p.Name)
		}
		fmt.Println()
		if r.Continue == "" {
			break
		}
		cont = r.Continue
	}
	// 标签过滤
	f := s.ListPods("prod", 0, "", map[string]string{"app": "web"})
	fmt.Printf("  标签过滤 app=web: %d 结果\n", len(f.Items))
	fmt.Println()
}

func demoErrors() {
	fmt.Println("📌 7. 错误处理 — K8s Status 对象")
	fmt.Println(strings.Repeat("─", 55))

	s := newServer()

	_, err := s.GetPod("default", "ghost")
	pErr("NotFound", err)

	_, _ = s.CreatePod(Pod{ObjectMeta: ObjectMeta{Name: "x", Namespace: "d"},
		Spec: PodSpec{Containers: []Container{{Name: "a", Image: "a:1"}}}})
	_, err = s.CreatePod(Pod{ObjectMeta: ObjectMeta{Name: "x", Namespace: "d"},
		Spec: PodSpec{Containers: []Container{{Name: "a", Image: "a:2"}}}})
	pErr("AlreadyExists", err)

	p, _ := s.CreatePod(Pod{ObjectMeta: ObjectMeta{Name: "c", Namespace: "d"},
		Spec: PodSpec{Containers: []Container{{Name: "a", Image: "a:1"}}}})
	stale := p.ResourceVersion
	p.Status.Phase = "Running"
	_, _ = s.UpdatePod(p)
	p.ResourceVersion = stale
	_, err = s.UpdatePod(p)
	pErr("Conflict", err)

	pErr("Unauthorized", (&BearerTokenAuth{}).Validate())

	fmt.Println()
	fmt.Println("  Status JSON 示例:")
	d, _ := json.MarshalIndent(errNotFound("pods", "my-pod"), "    ", "  ")
	fmt.Printf("    %s\n", string(d))
	fmt.Println()
	fmt.Println("  最佳实践: 409→RetryOnConflict  404→幂等处理  429→指数退避")
	fmt.Println()
}

func pErr(label string, err error) {
	if st, ok := err.(*Status); ok {
		fmt.Printf("  [%s] %d %s — %s\n", label, st.Code, st.Reason, st.Message)
	}
}
