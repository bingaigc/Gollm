// Day 20 - MCP 协议进阶 (Model Context Protocol Advanced)
//
// 本示例演示 MCP 协议的进阶特性：
// - 资源端点（resources/list, resources/read）让 LLM 读取外部数据
// - Prompt 模板（prompts/list, prompts/get）提供可复用的提示词模板
// - 会话管理：基于 session ID 的状态追踪与多客户端并发
// - 工具组合调用：LLM 按顺序调用多个工具完成复杂任务
// - 错误恢复：工具调用失败时的重试与降级策略
// - 协议合规校验：验证 JSON-RPC 请求格式与必填字段
//
// 🔑 Day 19 实现了 tools/list 和 tools/call，本节扩展资源、模板、会话等能力。

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// JSON-RPC 2.0 协议类型（请求、响应、错误）
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

const (
	CodeParseError     = -32700 // 解析错误
	CodeInvalidRequest = -32600 // 无效请求
	CodeMethodNotFound = -32601 // 方法未找到
	CodeInvalidParams  = -32602 // 无效参数
	CodeInternalError  = -32603 // 内部错误
)

// MCP 资源类型 —— 让 LLM 通过 URI 读取外部数据源
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// MCP Prompt 模板 —— 提供可复用的提示词
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}
type PromptMessage struct {
	Role    string               `json:"role"`
	Content PromptMessageContent `json:"content"`
}
type PromptMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// 工具调用相关类型
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}
type ToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// 会话管理器 —— 追踪多客户端状态（sync.RWMutex 保证并发安全）
type Session struct {
	ID        string
	CreatedAt time.Time
	CallCount int
	History   []string
}
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// GetOrCreate 获取或创建会话
func (sm *SessionManager) GetOrCreate(id string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[id]; ok {
		return s
	}
	s := &Session{ID: id, CreatedAt: time.Now()}
	sm.sessions[id] = s
	return s
}
func (sm *SessionManager) RecordCall(sid, tool string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sid]; ok {
		s.CallCount++
		s.History = append(s.History, tool)
	}
}
func (sm *SessionManager) GetStats(sid string) (int, []string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if s, ok := sm.sessions[sid]; ok {
		h := make([]string, len(s.History))
		copy(h, s.History)
		return s.CallCount, h
	}
	return 0, nil
}
func (sm *SessionManager) ActiveCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// MCP 进阶服务器 —— 集成资源、模板、工具、会话
type MCPServer struct {
	resources map[string]Resource
	resData   map[string]string
	resOrder  []string
	prompts   map[string]Prompt
	pOrder    []string
	tools     map[string]func(map[string]interface{}) (*ToolResult, error)
	sessions  *SessionManager
}
func NewMCPServer() *MCPServer {
	s := &MCPServer{
		resources: make(map[string]Resource),
		resData:   make(map[string]string),
		prompts:   make(map[string]Prompt),
		tools:     make(map[string]func(map[string]interface{}) (*ToolResult, error)),
		sessions:  NewSessionManager(),
	}
	s.setup()
	return s
}
func (s *MCPServer) setup() {
	s.addResource(Resource{
		URI: "data://products/catalog", Name: "产品目录",
		Description: "公司产品列表及价格", MimeType: "application/json",
	}, `[{"name":"Go语言实战","price":79.9},{"name":"MCP开发套件","price":299.0}]`)
	s.addResource(Resource{
		URI: "config://system/settings", Name: "系统配置",
		Description: "运行参数", MimeType: "application/json",
	}, `{"max_tokens":4096,"temperature":0.7,"model":"gpt-4"}`)
	s.addPrompt(Prompt{
		Name: "code_review", Description: "专业代码审查",
		Arguments: []PromptArgument{
			{Name: "language", Description: "编程语言", Required: true},
			{Name: "code", Description: "待审查代码", Required: true},
		},
	})
	s.addPrompt(Prompt{
		Name: "translate", Description: "文本翻译",
		Arguments: []PromptArgument{
			{Name: "target_lang", Description: "目标语言", Required: true},
			{Name: "text", Description: "待翻译文本", Required: true},
		},
	})

	s.tools["search_product"] = func(args map[string]interface{}) (*ToolResult, error) {
		kw, _ := args["keyword"].(string)
		if kw == "" {
			return errorResult("缺少参数: keyword"), nil
		}
		catalog := s.resData["data://products/catalog"]
		if strings.Contains(catalog, kw) {
			return textResult(fmt.Sprintf("找到含 \"%s\" 的产品: %s", kw, catalog)), nil
		}
		return textResult(fmt.Sprintf("未找到含 \"%s\" 的产品", kw)), nil
	}
	s.tools["format_report"] = func(args map[string]interface{}) (*ToolResult, error) {
		data, _ := args["data"].(string)
		if data == "" {
			return errorResult("缺少参数: data"), nil
		}
		return textResult(fmt.Sprintf("📊 报告\n━━━━━━━━━━\n%s\n━━━━━━━━━━\n时间: %s",
			data, time.Now().Format("2006-01-02 15:04"))), nil
	}
	var callCount int64 // 原子计数器，保证并发安全
	s.tools["unstable_api"] = func(args map[string]interface{}) (*ToolResult, error) {
		n := atomic.AddInt64(&callCount, 1)
		if n <= 2 {
			return errorResult(fmt.Sprintf("第 %d 次: 服务暂时不可用", n)), nil
		}
		return textResult(fmt.Sprintf("第 %d 次: 成功获取数据", n)), nil
	}
}
func (s *MCPServer) addResource(r Resource, data string) {
	s.resources[r.URI] = r
	s.resData[r.URI] = data
	s.resOrder = append(s.resOrder, r.URI)
}
func (s *MCPServer) addPrompt(p Prompt) {
	s.prompts[p.Name] = p
	s.pOrder = append(s.pOrder, p.Name)
}
func textResult(t string) *ToolResult {
	return &ToolResult{Content: []ToolResultContent{{Type: "text", Text: t}}}
}
func errorResult(t string) *ToolResult {
	return &ToolResult{Content: []ToolResultContent{{Type: "text", Text: t}}, IsError: true}
}

// ServeHTTP 根据 JSON-RPC method 分发请求，包含协议合规校验
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, nil, CodeParseError, "无法读取请求体")
		return
	}
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, nil, CodeParseError, "JSON 解析失败: "+err.Error())
		return
	}
	// 协议校验：版本、方法、ID
	if req.JSONRPC != "2.0" {
		writeErr(w, req.ID, CodeInvalidRequest, "jsonrpc 字段必须为 \"2.0\"")
		return
	}
	if req.Method == "" {
		writeErr(w, req.ID, CodeInvalidRequest, "method 字段不能为空")
		return
	}
	if req.ID == nil {
		writeErr(w, nil, CodeInvalidRequest, "id 字段不能为空")
		return
	}
	// 会话追踪
	if sid := r.Header.Get("X-Session-ID"); sid != "" {
		s.sessions.GetOrCreate(sid)
	}
	// 方法路由（资源、模板、工具三大类）
	switch req.Method {
	case "resources/list":
		res := make([]Resource, 0, len(s.resOrder))
		for _, uri := range s.resOrder {
			res = append(res, s.resources[uri])
		}
		writeOK(w, req.ID, map[string]interface{}{"resources": res})
	case "resources/read":
		var p struct{ URI string `json:"uri"` }
		if json.Unmarshal(req.Params, &p) != nil || p.URI == "" {
			writeErr(w, req.ID, CodeInvalidParams, "参数解析失败")
			return
		}
		data, ok := s.resData[p.URI]
		if !ok {
			writeErr(w, req.ID, CodeInvalidParams, "资源不存在: "+p.URI)
			return
		}
		writeOK(w, req.ID, map[string]interface{}{
			"contents": []ResourceContent{{URI: p.URI, MimeType: s.resources[p.URI].MimeType, Text: data}},
		})
	case "prompts/list":
		list := make([]Prompt, 0, len(s.pOrder))
		for _, n := range s.pOrder {
			list = append(list, s.prompts[n])
		}
		writeOK(w, req.ID, map[string]interface{}{"prompts": list})
	case "prompts/get":
		s.handlePromptsGet(w, req)
	case "tools/call":
		s.handleToolsCall(w, r, req)
	default:
		writeErr(w, req.ID, CodeMethodNotFound, "未知方法: "+req.Method)
	}
}
func (s *MCPServer) handlePromptsGet(w http.ResponseWriter, req JSONRPCRequest) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &p) != nil {
		writeErr(w, req.ID, CodeInvalidParams, "参数解析失败")
		return
	}
	prompt, ok := s.prompts[p.Name]
	if !ok {
		writeErr(w, req.ID, CodeInvalidParams, "Prompt 不存在: "+p.Name)
		return
	}
	// 校验必填参数
	for _, a := range prompt.Arguments {
		if a.Required {
			if _, ok := p.Arguments[a.Name]; !ok {
				writeErr(w, req.ID, CodeInvalidParams, "缺少必填参数: "+a.Name)
				return
			}
		}
	}
	// 渲染模板：根据名称填充消息
	var msgs []PromptMessage
	mk := func(role, text string) PromptMessage {
		return PromptMessage{Role: role, Content: PromptMessageContent{Type: "text", Text: text}}
	}
	switch p.Name {
	case "code_review":
		msgs = []PromptMessage{
			mk("system", fmt.Sprintf("你是资深 %s 开发者，请审查代码。", p.Arguments["language"])),
			mk("user", fmt.Sprintf("审查以下代码：\n```%s\n%s\n```", p.Arguments["language"], p.Arguments["code"])),
		}
	case "translate":
		msgs = []PromptMessage{
			mk("system", fmt.Sprintf("请将文本翻译为%s。", p.Arguments["target_lang"])),
			mk("user", "请翻译："+p.Arguments["text"]),
		}
	}
	writeOK(w, req.ID, map[string]interface{}{"description": prompt.Description, "messages": msgs})
}
func (s *MCPServer) handleToolsCall(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	var p ToolCallParams
	if json.Unmarshal(req.Params, &p) != nil {
		writeErr(w, req.ID, CodeInvalidParams, "参数解析失败")
		return
	}
	handler, ok := s.tools[p.Name]
	if !ok {
		writeErr(w, req.ID, CodeInvalidParams, "工具不存在: "+p.Name)
		return
	}
	if sid := r.Header.Get("X-Session-ID"); sid != "" {
		s.sessions.RecordCall(sid, p.Name)
	}
	result, err := handler(p.Arguments)
	if err != nil {
		writeErr(w, req.ID, CodeInternalError, err.Error())
		return
	}
	writeOK(w, req.ID, result)
}
func writeOK(w http.ResponseWriter, id, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}
func writeErr(w http.ResponseWriter, id interface{}, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &JSONRPCError{Code: code, Message: msg}})
}

// 客户端辅助函数
func mcpCall(url, method string, params interface{}, id interface{}, headers map[string]string) (*JSONRPCResponse, error) {
	req := JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		p, _ := json.Marshal(params)
		req.Params = p
	}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	var resp JSONRPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
func call(url, method string, params interface{}, id interface{}) (*JSONRPCResponse, error) {
	return mcpCall(url, method, params, id, nil)
}
func extractText(resp *JSONRPCResponse) string {
	if resp == nil || resp.Error != nil {
		return ""
	}
	b, _ := json.Marshal(resp.Result)
	var r ToolResult
	if json.Unmarshal(b, &r) == nil && len(r.Content) > 0 {
		return r.Content[0].Text
	}
	return string(b)
}

// callWithRetry 带重试的工具调用（maxRetries 次尝试后放弃）
func callWithRetry(url string, params ToolCallParams, baseID, maxRetries int) (*JSONRPCResponse, int) {
	for i := 1; i <= maxRetries; i++ {
		resp, err := call(url, "tools/call", params, baseID+i)
		if err != nil {
			fmt.Printf("    尝试 %d/%d: 网络错误 - %v\n", i, maxRetries, err)
			continue
		}
		b, _ := json.Marshal(resp.Result)
		var r ToolResult
		if json.Unmarshal(b, &r) == nil && !r.IsError {
			fmt.Printf("    尝试 %d/%d: ✅ 成功\n", i, maxRetries)
			return resp, i
		}
		if len(r.Content) > 0 {
			fmt.Printf("    尝试 %d/%d: ⚠️  %s\n", i, maxRetries, r.Content[0].Text)
		}
	}
	return nil, maxRetries
}

func main() {
	fmt.Println("=== Day 20: MCP 协议进阶 ===")
	fmt.Println()
	srv := NewMCPServer()
	ts := httptest.NewServer(srv)
	defer ts.Close()
	fmt.Printf("🚀 MCP 进阶服务器已启动: %s\n", ts.URL)
	fmt.Println()
	demoResources(ts.URL)
	demoPrompts(ts.URL)
	demoSessions(ts.URL, srv)
	demoComposition(ts.URL)
	demoRetry(ts.URL)
	demoValidation(ts.URL)
	fmt.Println("🎓 学习要点：")
	fmt.Println("  1. resources/list 和 resources/read 让 LLM 动态获取外部数据作为上下文")
	fmt.Println("  2. prompts/list 和 prompts/get 提供结构化的提示词模板，提升一致性")
	fmt.Println("  3. 会话管理通过 session ID 追踪客户端状态，支持多轮对话")
	fmt.Println("  4. 工具组合调用让 LLM 按步骤完成复杂任务（搜索 → 格式化）")
	fmt.Println("  5. 错误恢复策略（重试 + 降级）保障工具调用的鲁棒性")
	fmt.Println("  6. 协议合规校验确保请求符合 JSON-RPC 2.0 和 MCP 规范")
}
func demoResources(url string) {
	fmt.Println("--- 1️⃣  资源端点 (Resources) ---")
	fmt.Println("  📂 resources/list — 发现可用资源：")
	resp, _ := call(url, "resources/list", nil, 1)
	printJSON("    ", resp.Result)
	fmt.Println("  📖 resources/read — 读取产品目录：")
	resp, _ = call(url, "resources/read", map[string]string{"uri": "data://products/catalog"}, 2)
	printJSON("    ", resp.Result)
	fmt.Println("  ⚠️  读取不存在的资源：")
	resp, _ = call(url, "resources/read", map[string]string{"uri": "data://nonexistent"}, 3)
	if resp.Error != nil {
		fmt.Printf("    JSON-RPC 错误 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
	}
	fmt.Println()
}
func demoPrompts(url string) {
	fmt.Println("--- 2️⃣  Prompt 模板 ---")
	fmt.Println("  📋 prompts/list — 发现可用模板：")
	resp, _ := call(url, "prompts/list", nil, 10)
	printJSON("    ", resp.Result)
	fmt.Println("  🔍 prompts/get — 渲染代码审查模板：")
	resp, _ = call(url, "prompts/get", map[string]interface{}{
		"name":      "code_review",
		"arguments": map[string]string{"language": "Go", "code": "func add(a, b int) int { return a+b }"},
	}, 11)
	printJSON("    ", resp.Result)
	fmt.Println("  ⚠️  缺少必填参数：")
	resp, _ = call(url, "prompts/get", map[string]interface{}{
		"name": "translate", "arguments": map[string]string{"target_lang": "English"},
	}, 12)
	if resp.Error != nil {
		fmt.Printf("    JSON-RPC 错误 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
	}
	fmt.Println()
}
func demoSessions(url string, srv *MCPServer) {
	fmt.Println("--- 3️⃣  会话管理 ---")
	fmt.Println("  🔄 模拟两个客户端并发调用：")
	var wg sync.WaitGroup
	clients := []struct{ sid, kw string }{
		{"session-alice", "Go"},
		{"session-bob", "MCP"},
	}
	results := make([]string, len(clients))
	for i, c := range clients {
		wg.Add(1)
		go func(idx int, sid, kw string) {
			defer wg.Done()
			h := map[string]string{"X-Session-ID": sid}
			mcpCall(url, "tools/call", ToolCallParams{Name: "search_product",
				Arguments: map[string]interface{}{"keyword": kw}}, 20+idx*10, h)
			mcpCall(url, "tools/call", ToolCallParams{Name: "format_report",
				Arguments: map[string]interface{}{"data": kw + " 报告"}}, 21+idx*10, h)
			cnt, hist := srv.sessions.GetStats(sid)
			results[idx] = fmt.Sprintf("    客户端 %s: 调用 %d 次, 历史: %v", sid, cnt, hist)
		}(i, c.sid, c.kw)
	}
	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}
	fmt.Printf("    活跃会话数: %d\n", srv.sessions.ActiveCount())
	fmt.Println()
}
func demoComposition(url string) {
	fmt.Println("--- 4️⃣  工具组合调用 ---")
	fmt.Println("  📝 场景：\"查找 Go 产品并生成报告\"")
	fmt.Println("  LLM 拆解：①搜索产品 → ②格式化报告")
	fmt.Println("  步骤 1: search_product(\"Go\")")
	resp, _ := call(url, "tools/call", ToolCallParams{
		Name: "search_product", Arguments: map[string]interface{}{"keyword": "Go"},
	}, 40)
	searchText := extractText(resp)
	fmt.Printf("    结果: %s\n", truncate(searchText, 80))
	// 步骤 2：前一步的输出作为下一步的输入
	fmt.Println("  步骤 2: format_report(搜索结果)")
	resp, _ = call(url, "tools/call", ToolCallParams{
		Name: "format_report", Arguments: map[string]interface{}{"data": searchText},
	}, 41)
	for _, line := range strings.Split(extractText(resp), "\n") {
		fmt.Printf("    %s\n", line)
	}
	fmt.Println()
}
func demoRetry(url string) {
	fmt.Println("--- 5️⃣  错误恢复 ---")
	fmt.Println("  🔁 重试策略 — 调用不稳定 API（最多 5 次）：")
	resp, attempts := callWithRetry(url, ToolCallParams{
		Name: "unstable_api", Arguments: map[string]interface{}{},
	}, 50, 5)
	if resp != nil {
		fmt.Printf("    最终结果（第 %d 次成功）: %s\n", attempts, extractText(resp))
	} else {
		fmt.Printf("    全部 %d 次尝试失败\n", attempts)
	}
	fmt.Println()
	// 降级策略：主工具不可用时自动切换备用工具
	fmt.Println("  🔄 降级策略 — 主工具失败后使用备用：")
	fmt.Println("    尝试 \"premium_search\"...")
	resp, err := call(url, "tools/call", ToolCallParams{
		Name: "premium_search", Arguments: map[string]interface{}{"keyword": "Go"},
	}, 60)
	if err != nil || resp.Error != nil {
		fmt.Println("    主工具不可用，降级为 \"search_product\"...")
		resp, _ = call(url, "tools/call", ToolCallParams{
			Name: "search_product", Arguments: map[string]interface{}{"keyword": "Go"},
		}, 61)
		if resp != nil && resp.Error == nil {
			fmt.Printf("    ✅ 降级成功: %s\n", truncate(extractText(resp), 60))
		}
	}
	fmt.Println()
}
func demoValidation(url string) {
	fmt.Println("--- 6️⃣  协议合规校验 ---")
	cases := []struct {
		name, payload string
	}{
		{"无效 JSON", `{bad json}`},
		{"缺少 jsonrpc 版本", `{"id":1,"method":"resources/list"}`},
		{"错误 jsonrpc 版本", `{"jsonrpc":"1.0","id":1,"method":"resources/list"}`},
		{"缺少 method", `{"jsonrpc":"2.0","id":1}`},
		{"缺少 id", `{"jsonrpc":"2.0","method":"resources/list"}`},
	}
	for _, tc := range cases {
		fmt.Printf("  🧪 %s\n", tc.name)
		httpResp, err := http.Post(url, "application/json", bytes.NewBufferString(tc.payload))
		if err != nil {
			fmt.Printf("    ❌ HTTP 错误: %v\n", err)
			continue
		}
		var resp JSONRPCResponse
		json.NewDecoder(httpResp.Body).Decode(&resp)
		httpResp.Body.Close()
		if resp.Error != nil {
			fmt.Printf("    ✅ 拒绝 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
		}
	}
	fmt.Println()
}

// 辅助函数
func printJSON(prefix string, v interface{}) {
	b, _ := json.MarshalIndent(v, prefix, "  ")
	fmt.Printf("%s%s\n", prefix, b)
	fmt.Println()
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}