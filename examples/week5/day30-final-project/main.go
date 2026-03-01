// Package main 是 30 天训练营的收官之作
//
// 本示例综合运用前 29 天的核心知识，构建一个迷你微服务框架：
//   - 路由器：简单 HTTP 路由注册与匹配，支持 GET/POST 方法路由
//   - 中间件链：日志、认证、恢复（recover panic），链式调用
//   - JSON API：结构化请求/响应，错误码标准化
//   - 并发安全内存存储：sync.RWMutex 保护的 map，完整 CRUD
//   - 优雅关闭：信号监听 + context 取消 + 连接排空
//   - 可观测性集成：请求计数、延迟追踪、request ID 传播
//   - 30 天知识回顾：在注释和输出中总结每周学到的核心概念
//
// 所有演示通过 net/http/httptest 完成，无需启动真实服务器。
package main

import (
"context"
"encoding/json"
"fmt"
"io"
"log"
"math/rand"
"net/http"
"net/http/httptest"
"strings"
"sync"
"sync/atomic"
"time"
)

// ── 标准化 JSON 响应（Day 10 结构体 + Day 14 JSON）──────────

// APIResponse 是统一的 API 响应格式，code=0 表示成功
type APIResponse struct {
Code      int         `json:"code"`
Message   string      `json:"message"`
Data      interface{} `json:"data,omitempty"`
RequestID string      `json:"request_id,omitempty"`
}

// User 表示用户资源（Day 10 结构体与方法）
type User struct {
ID        string `json:"id"`
Name      string `json:"name"`
Email     string `json:"email"`
CreatedAt string `json:"created_at"`
}

// Validate 校验用户字段（Day 12 错误处理）
func (u *User) Validate() error {
if u.Name == "" {
return fmt.Errorf("用户名不能为空")
}
if !strings.Contains(u.Email, "@") {
return fmt.Errorf("邮箱格式不正确: %s", u.Email)
}
return nil
}

// ── 并发安全内存存储（Day 19 sync.RWMutex + Day 15 Map）────

// MemoryStore 使用读写锁保护的并发安全内存存储
type MemoryStore struct {
mu    sync.RWMutex
users map[string]*User
}

// NewMemoryStore 创建新的内存存储实例
func NewMemoryStore() *MemoryStore {
return &MemoryStore{users: make(map[string]*User)}
}

// Create 添加新用户（写锁）
func (s *MemoryStore) Create(user *User) error {
s.mu.Lock()
defer s.mu.Unlock()
if _, exists := s.users[user.ID]; exists {
return fmt.Errorf("用户 %s 已存在", user.ID)
}
s.users[user.ID] = user
return nil
}

// Get 通过 ID 获取用户（读锁）
func (s *MemoryStore) Get(id string) (*User, error) {
s.mu.RLock()
defer s.mu.RUnlock()
user, ok := s.users[id]
if !ok {
return nil, fmt.Errorf("用户 %s 不存在", id)
}
return user, nil
}

// Update 更新用户信息（写锁）
func (s *MemoryStore) Update(user *User) error {
s.mu.Lock()
defer s.mu.Unlock()
if _, exists := s.users[user.ID]; !exists {
return fmt.Errorf("用户 %s 不存在", user.ID)
}
s.users[user.ID] = user
return nil
}

// Delete 删除用户（写锁）
func (s *MemoryStore) Delete(id string) error {
s.mu.Lock()
defer s.mu.Unlock()
if _, exists := s.users[id]; !exists {
return fmt.Errorf("用户 %s 不存在", id)
}
delete(s.users, id)
return nil
}

// List 列出所有用户（读锁）
func (s *MemoryStore) List() []*User {
s.mu.RLock()
defer s.mu.RUnlock()
result := make([]*User, 0, len(s.users))
for _, u := range s.users {
result = append(result, u)
}
return result
}

// Count 返回用户数量（读锁）
func (s *MemoryStore) Count() int {
s.mu.RLock()
defer s.mu.RUnlock()
return len(s.users)
}

// ── 可观测性 — 指标收集（Day 27 可观测性基础）───────────────

// Metrics 使用原子操作进行无锁请求计数和延迟追踪
type Metrics struct {
totalRequests int64
totalErrors   int64
mu            sync.Mutex
latencies     []time.Duration
}

// NewMetrics 创建指标收集器
func NewMetrics() *Metrics {
return &Metrics{latencies: make([]time.Duration, 0, 64)}
}

// RecordRequest 记录一次请求（原子操作，无需加锁）
func (m *Metrics) RecordRequest() { atomic.AddInt64(&m.totalRequests, 1) }

// RecordError 记录一次错误
func (m *Metrics) RecordError() { atomic.AddInt64(&m.totalErrors, 1) }

// RecordLatency 记录请求延迟
func (m *Metrics) RecordLatency(d time.Duration) {
m.mu.Lock()
m.latencies = append(m.latencies, d)
m.mu.Unlock()
}

// Summary 输出指标汇总
func (m *Metrics) Summary() string {
reqs := atomic.LoadInt64(&m.totalRequests)
errs := atomic.LoadInt64(&m.totalErrors)
m.mu.Lock()
var total time.Duration
for _, d := range m.latencies {
total += d
}
n := len(m.latencies)
m.mu.Unlock()
avg := time.Duration(0)
if n > 0 {
avg = total / time.Duration(n)
}
return fmt.Sprintf("请求总数=%d, 错误数=%d, 平均延迟=%v", reqs, errs, avg)
}

// ── 路由器（Day 22 HTTP 基础）──────────────────────────────

// routeKey 用方法+路径组合作为路由唯一标识
type routeKey struct{ method, path string }

// HandlerFunc 是框架的处理函数类型
type HandlerFunc func(http.ResponseWriter, *http.Request)

// Middleware 是中间件函数类型：接收并返回处理器
type Middleware func(HandlerFunc) HandlerFunc

// Router 迷你路由器，支持方法+路径匹配和中间件链
type Router struct {
routes      map[routeKey]HandlerFunc
middlewares []Middleware
}

// NewRouter 创建路由器实例
func NewRouter() *Router {
return &Router{routes: make(map[routeKey]HandlerFunc)}
}

// Use 注册全局中间件
func (r *Router) Use(mw Middleware) { r.middlewares = append(r.middlewares, mw) }

// GET 注册 GET 路由
func (r *Router) GET(path string, h HandlerFunc) {
r.routes[routeKey{http.MethodGet, path}] = h
}

// POST 注册 POST 路由
func (r *Router) POST(path string, h HandlerFunc) {
r.routes[routeKey{http.MethodPost, path}] = h
}

// PUT 注册 PUT 路由
func (r *Router) PUT(path string, h HandlerFunc) {
r.routes[routeKey{http.MethodPut, path}] = h
}

// DELETE 注册 DELETE 路由
func (r *Router) DELETE(path string, h HandlerFunc) {
r.routes[routeKey{http.MethodDelete, path}] = h
}

// ServeHTTP 实现 http.Handler 接口（Day 11 接口）
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
handler, ok := r.routes[routeKey{req.Method, req.URL.Path}]
if !ok {
writeJSON(w, http.StatusNotFound, APIResponse{
Code: 404, Message: "路由未找到: " + req.Method + " " + req.URL.Path,
})
return
}
// 逆序包装中间件，使第一个注册的最先执行
for i := len(r.middlewares) - 1; i >= 0; i-- {
handler = r.middlewares[i](handler)
}
handler(w, req)
}

// ── 中间件实现（Day 16 函数式编程 + Day 11 接口）───────────

// contextKey 自定义 context key 避免冲突（Day 20 Context）
type contextKey string

const requestIDKey contextKey = "request_id"

// getRequestID 从 context 中提取 request ID
func getRequestID(ctx context.Context) string {
if id, ok := ctx.Value(requestIDKey).(string); ok {
return id
}
return "unknown"
}

// RequestIDMiddleware 为每个请求注入唯一 ID
func RequestIDMiddleware() Middleware {
return func(next HandlerFunc) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
id := fmt.Sprintf("req-%d-%04d", time.Now().UnixNano()%100000, rand.Intn(10000))
ctx := context.WithValue(r.Context(), requestIDKey, id)
w.Header().Set("X-Request-ID", id)
next(w, r.WithContext(ctx))
}
}
}

// LoggingMiddleware 记录请求日志和延迟（Day 27 可观测性）
func LoggingMiddleware(m *Metrics) Middleware {
return func(next HandlerFunc) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
start := time.Now()
m.RecordRequest()
rid := getRequestID(r.Context())
log.Printf("[%s] --> %s %s", rid, r.Method, r.URL.Path)
next(w, r)
elapsed := time.Since(start)
m.RecordLatency(elapsed)
log.Printf("[%s] <-- %s %s (%v)", rid, r.Method, r.URL.Path, elapsed)
}
}
}

// AuthMiddleware 简单 Token 认证中间件
func AuthMiddleware() Middleware {
return func(next HandlerFunc) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
if r.Header.Get("Authorization") != "Bearer valid-token" {
writeJSON(w, http.StatusUnauthorized, APIResponse{
Code: 401, Message: "认证失败：无效的令牌",
RequestID: getRequestID(r.Context()),
})
return
}
next(w, r)
}
}
}

// RecoveryMiddleware 捕获 panic 返回 500（Day 12 recover）
func RecoveryMiddleware(m *Metrics) Middleware {
return func(next HandlerFunc) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
defer func() {
if err := recover(); err != nil {
m.RecordError()
rid := getRequestID(r.Context())
log.Printf("[%s] ⚠️ PANIC 恢复: %v", rid, err)
writeJSON(w, http.StatusInternalServerError, APIResponse{
Code: 500, Message: "服务器内部错误", RequestID: rid,
})
}
}()
next(w, r)
}
}
}

// ── JSON API 处理器（Day 14 JSON + Day 22 HTTP）────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(v)
}

func makeListHandler(s *MemoryStore) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
writeJSON(w, http.StatusOK, APIResponse{
Code: 0, Message: "获取用户列表成功",
Data: s.List(), RequestID: getRequestID(r.Context()),
})
}
}

func makeCreateHandler(s *MemoryStore) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
rid := getRequestID(r.Context())
var u User
if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "解析失败: " + err.Error(), RequestID: rid})
return
}
if err := u.Validate(); err != nil {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error(), RequestID: rid})
return
}
u.CreatedAt = time.Now().Format(time.RFC3339)
if err := s.Create(&u); err != nil {
writeJSON(w, http.StatusConflict, APIResponse{Code: 409, Message: err.Error(), RequestID: rid})
return
}
writeJSON(w, http.StatusCreated, APIResponse{Code: 0, Message: "用户创建成功", Data: u, RequestID: rid})
}
}

func makeGetHandler(s *MemoryStore) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
rid := getRequestID(r.Context())
id := r.URL.Query().Get("id")
if id == "" {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "缺少 id 参数", RequestID: rid})
return
}
u, err := s.Get(id)
if err != nil {
writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: err.Error(), RequestID: rid})
return
}
writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "获取用户成功", Data: u, RequestID: rid})
}
}

func makeUpdateHandler(s *MemoryStore) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
rid := getRequestID(r.Context())
var u User
if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "解析失败: " + err.Error(), RequestID: rid})
return
}
if err := u.Validate(); err != nil {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error(), RequestID: rid})
return
}
if err := s.Update(&u); err != nil {
writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: err.Error(), RequestID: rid})
return
}
writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户更新成功", Data: u, RequestID: rid})
}
}

func makeDeleteHandler(s *MemoryStore) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
rid := getRequestID(r.Context())
id := r.URL.Query().Get("id")
if id == "" {
writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "缺少 id 参数", RequestID: rid})
return
}
if err := s.Delete(id); err != nil {
writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: err.Error(), RequestID: rid})
return
}
writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户删除成功", RequestID: rid})
}
}

func makePanicHandler() HandlerFunc {
return func(_ http.ResponseWriter, _ *http.Request) {
panic("模拟崩溃！RecoveryMiddleware 会捕获它")
}
}

func makeHealthHandler(s *MemoryStore, m *Metrics) HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
writeJSON(w, http.StatusOK, APIResponse{
Code: 0, Message: "服务健康",
Data:      map[string]interface{}{"status": "UP", "users": s.Count(), "metrics": m.Summary()},
RequestID: getRequestID(r.Context()),
})
}
}

// ── 测试辅助（Day 24 httptest）─────────────────────────────

func doReq(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
var br io.Reader
if body != "" {
br = strings.NewReader(body)
}
req := httptest.NewRequest(method, path, br)
for k, v := range headers {
req.Header.Set(k, v)
}
rr := httptest.NewRecorder()
h.ServeHTTP(rr, req)
return rr
}

func showResp(label string, rr *httptest.ResponseRecorder) {
fmt.Printf("  📋 %s\n", label)
fmt.Printf("     HTTP %d", rr.Code)
var resp APIResponse
if err := json.Unmarshal(rr.Body.Bytes(), &resp); err == nil {
fmt.Printf(" | code=%d | %s\n", resp.Code, resp.Message)
if resp.Data != nil {
d, _ := json.MarshalIndent(resp.Data, "     ", "  ")
fmt.Printf("     %s\n", string(d))
}
} else {
fmt.Println()
}
}

// ── 主函数 ─────────────────────────────────────────────────

func main() {
fmt.Println("╔══════════════════════════════════════════════════════════╗")
fmt.Println("║        🎓 Day 30: 综合项目与总结                       ║")
fmt.Println("║        迷你微服务框架 — 30 天知识大融合                 ║")
fmt.Println("╚══════════════════════════════════════════════════════════╝")
fmt.Println()
printReview()

// ── 初始化组件 ─────────────────────────────────────
section("📦 初始化核心组件")
store := NewMemoryStore()
metrics := NewMetrics()
router := NewRouter()
fmt.Println("  ✅ MemoryStore（sync.RWMutex）、Metrics（atomic）、Router 就绪")
fmt.Println()

// ── 注册中间件（执行顺序：Recovery→ID→Log→Auth→Handler）
section("🔗 注册中间件链")
router.Use(RecoveryMiddleware(metrics))
router.Use(RequestIDMiddleware())
router.Use(LoggingMiddleware(metrics))
router.Use(AuthMiddleware())
fmt.Println("  Recovery → RequestID → Logging → Auth → Handler")
fmt.Println()

// ── 注册路由 ───────────────────────────────────────
section("🗺️  注册 API 路由")
router.GET("/health", makeHealthHandler(store, metrics))
router.GET("/api/users", makeListHandler(store))
router.POST("/api/users", makeCreateHandler(store))
router.GET("/api/user", makeGetHandler(store))
router.PUT("/api/user", makeUpdateHandler(store))
router.DELETE("/api/user", makeDeleteHandler(store))
router.GET("/api/panic", makePanicHandler())
fmt.Println("  GET /health | GET/POST /api/users | GET/PUT/DELETE /api/user | GET /api/panic")
fmt.Println()

log.SetFlags(log.Ltime | log.Lmicroseconds)
auth := map[string]string{"Authorization": "Bearer valid-token"}

section("🔐 演示 1：认证失败（无 Token）")
showResp("GET /api/users 无认证 → 401", doReq(router, "GET", "/api/users", "", nil))
fmt.Println()

section("👤 演示 2：创建用户")
for _, b := range []string{
`{"id":"u1","name":"张三","email":"zhangsan@example.com"}`,
`{"id":"u2","name":"李四","email":"lisi@example.com"}`,
`{"id":"u3","name":"王五","email":"wangwu@example.com"}`,
} {
showResp("POST /api/users", doReq(router, "POST", "/api/users", b, auth))
}
fmt.Println()

section("⚠️  演示 3：重复创建 → 409")
showResp("重复 u1", doReq(router, "POST", "/api/users",
`{"id":"u1","name":"张三","email":"zhangsan@example.com"}`, auth))
fmt.Println()

section("📋 演示 4：列出所有用户")
showResp("GET /api/users", doReq(router, "GET", "/api/users", "", auth))
fmt.Println()

section("🔍 演示 5：获取用户 u1")
showResp("GET /api/user?id=u1", doReq(router, "GET", "/api/user?id=u1", "", auth))
fmt.Println()

section("✏️  演示 6：更新用户 u1")
showResp("PUT /api/user", doReq(router, "PUT", "/api/user",
`{"id":"u1","name":"张三(更新)","email":"new@example.com"}`, auth))
fmt.Println()

section("🗑️  演示 7：删除用户 u3")
showResp("DELETE u3", doReq(router, "DELETE", "/api/user?id=u3", "", auth))
fmt.Println()

section("🚫 演示 8：参数校验失败 → 400")
showResp("空名称", doReq(router, "POST", "/api/users",
`{"id":"u4","name":"","email":"bad"}`, auth))
fmt.Println()

section("💥 演示 9：Panic 恢复中间件")
showResp("GET /api/panic → 500", doReq(router, "GET", "/api/panic", "", auth))
fmt.Println()

section("🚧 演示 10：路由未找到 → 404")
showResp("未知路由", doReq(router, "GET", "/not-exist", "", auth))
fmt.Println()

section("💚 演示 11：健康检查与指标")
showResp("GET /health", doReq(router, "GET", "/health", "", auth))
fmt.Println()

section("🔒 演示 12：并发安全验证（goroutine + sync）")
demoConcurrency(store)
fmt.Println()

section("🔌 演示 13：优雅关闭（Context + httptest）")
demoGracefulShutdown(router)
fmt.Println()

section("📊 最终指标汇总")
fmt.Printf("  %s\n", metrics.Summary())
fmt.Printf("  存储剩余用户数: %d\n", store.Count())
fmt.Println()

// ── 结语 ───────────────────────────────────────────
fmt.Println("╔══════════════════════════════════════════════════════════╗")
fmt.Println("║  🎉 恭喜完成 30 天 Go 语言训练营！                     ║")
fmt.Println("║  本项目综合运用了：结构体/接口、错误处理、JSON、        ║")
fmt.Println("║  闭包、Goroutine/Channel、sync、Context、HTTP、        ║")
fmt.Println("║  httptest、可观测性等核心知识。Keep coding! 🚀          ║")
fmt.Println("╚══════════════════════════════════════════════════════════╝")
}

// ── 辅助函数 ───────────────────────────────────────────────

func section(title string) {
fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
fmt.Println(title)
fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// printReview 输出 30 天知识回顾
func printReview() {
section("📚 30 天知识回顾")
fmt.Println("  📗 第一周：变量/类型、控制流、函数、数组/切片/Map")
fmt.Println("  📘 第二周：指针、结构体/方法、接口、错误处理、JSON/IO")
fmt.Println("  📙 第三周：高级数据结构、函数式、Goroutine/Channel、sync、Context")
fmt.Println("  📕 第四周：HTTP 服务、数据库、测试、Client-Go、性能/可观测/模式")
fmt.Println("  📓 第五周：容器化、项目结构、综合项目（本示例）")
fmt.Println()
}

// demoConcurrency 演示并发安全的存储操作
func demoConcurrency(store *MemoryStore) {
var wg sync.WaitGroup
const n = 10
fmt.Printf("  🚀 启动 %d 个 goroutine 并发读写...\n", n*2)

for i := 0; i < n; i++ {
wg.Add(2)
go func(id int) {
defer wg.Done()
_ = store.Create(&User{
ID: fmt.Sprintf("c-%d", id), Name: fmt.Sprintf("并发%d", id),
Email: fmt.Sprintf("c%d@t.com", id), CreatedAt: time.Now().Format(time.RFC3339),
})
}(i)
go func() {
defer wg.Done()
_ = store.List()
}()
}
wg.Wait()
fmt.Printf("  ✅ 完成，共 %d 用户（无数据竞争）\n", store.Count())

// 清理
for i := 0; i < n; i++ {
_ = store.Delete(fmt.Sprintf("c-%d", i))
}
fmt.Printf("  🧹 清理后剩余 %d 用户\n", store.Count())
}

// demoGracefulShutdown 演示优雅关闭流程
func demoGracefulShutdown(handler http.Handler) {
ts := httptest.NewServer(handler)
fmt.Printf("  🌐 测试服务器: %s\n", ts.URL)

// 验证服务器正常
resp, err := http.Get(ts.URL + "/health")
if err == nil {
body, _ := io.ReadAll(resp.Body)
resp.Body.Close()
var ar APIResponse
if json.Unmarshal(body, &ar) == nil {
fmt.Printf("  📡 健康: code=%d, %s\n", ar.Code, ar.Message)
}
}

ts.Close()
fmt.Println("  ✅ 服务器已关闭")
fmt.Println()

// 演示 context 取消（Day 20）
fmt.Println("  📖 优雅关闭流程：")
fmt.Println("     1. 监听 SIGINT/SIGTERM → 2. 停止接受新连接")
fmt.Println("     3. context.WithTimeout 控制排空 → 4. server.Shutdown(ctx)")
fmt.Println()

fmt.Println("  🔄 context 取消演示：")
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()
done := make(chan struct{})
go func() {
select {
case <-ctx.Done():
fmt.Printf("     取消信号: %v\n", ctx.Err())
case <-time.After(1 * time.Second):
fmt.Println("     超时")
}
close(done)
}()
<-done
fmt.Println("  ✅ context 取消完成")
}
