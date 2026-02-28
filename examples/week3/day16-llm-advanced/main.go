// Day 16 - LLM 高级调用技巧
//
// 本示例演示 LLM API 的六大高级调用模式：
// 1. 函数调用（Function Calling）  2. 对话记忆管理
// 3. Prompt 模板引擎              4. 多模型路由
// 5. 速率限制（Token Bucket）     6. 成本追踪
//
// 🔑 核心概念：生产级 AI 应用需要工具编排、资源管理和成本优化。

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"text/template"
	"time"
)

// ============================================================
// 数据结构 —— 扩展 OpenAI ChatCompletion API，支持函数调用
// ============================================================

// Message 对话消息，新增 FunctionCall 字段支持工具调用
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"` // 模型请求调用的函数
	Name         string        `json:"name,omitempty"`          // role=function 时的函数名
}

// FunctionCall 模型请求调用的函数（名称 + JSON 参数）
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// FunctionDef 工具函数的 schema，告知 LLM 可用工具
type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []Message     `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
	Functions []FunctionDef `json:"functions,omitempty"` // 可用工具列表
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // "stop" 或 "function_call"
}

type ChatCompletion struct {
	ID      string   `json:"id"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// ============================================================
// 1. 函数调用 —— 让 LLM 调用外部工具
// ============================================================

type ToolRegistry struct {
	tools   map[string]func(string) string
	Schemas []FunctionDef
}

func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: make(map[string]func(string) string)} }

func (tr *ToolRegistry) Register(def FunctionDef, fn func(string) string) {
	tr.tools[def.Name] = fn
	tr.Schemas = append(tr.Schemas, def)
}

func (tr *ToolRegistry) Execute(name, args string) (string, error) {
	if fn, ok := tr.tools[name]; ok {
		return fn(args), nil
	}
	return "", fmt.Errorf("未知工具: %s", name)
}

func setupTools() *ToolRegistry {
	reg := NewToolRegistry()
	reg.Register(FunctionDef{
		Name: "get_weather", Description: "获取城市天气",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{
			"city": map[string]interface{}{"type": "string"},
		}},
	}, func(args string) string {
		var p struct{ City string `json:"city"` }
		json.Unmarshal([]byte(args), &p)
		return fmt.Sprintf(`{"city":"%s","temp":"22°C","condition":"晴"}`, p.City)
	})
	reg.Register(FunctionDef{
		Name: "calculate", Description: "数学计算",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{
			"expression": map[string]interface{}{"type": "string"},
		}},
	}, func(args string) string {
		var p struct{ Expression string `json:"expression"` }
		json.Unmarshal([]byte(args), &p)
		return fmt.Sprintf(`{"expression":"%s","result":42}`, p.Expression)
	})
	return reg
}

// ============================================================
// 2. 对话记忆管理 —— 滑动窗口 + Token 估算
// ============================================================

// ConversationMemory 管理对话历史，滑动窗口确保不超出上下文
type ConversationMemory struct {
	messages     []Message
	maxTokens    int
	systemPrompt string
}

func NewConversationMemory(sys string, max int) *ConversationMemory {
	return &ConversationMemory{systemPrompt: sys, maxTokens: max}
}

// estimateTokens 估算 token：中文约 2 token/字，英文约 1 token/4字符
func estimateTokens(text string) int {
	t := 0
	for _, r := range text {
		if r > 127 { t += 2 } else { t++ }
	}
	return int(math.Ceil(float64(t) / 3.0))
}

func (cm *ConversationMemory) totalTokens() int {
	total := estimateTokens(cm.systemPrompt)
	for _, m := range cm.messages { total += estimateTokens(m.Content) + 4 }
	return total
}

// Add 添加消息，超 token 预算时裁剪最早消息（保留至少 2 条）
func (cm *ConversationMemory) Add(msg Message) {
	cm.messages = append(cm.messages, msg)
	for cm.totalTokens() > cm.maxTokens && len(cm.messages) > 2 { cm.messages = cm.messages[1:] }
}

func (cm *ConversationMemory) GetMessages() []Message {
	return append([]Message{{Role: "system", Content: cm.systemPrompt}}, cm.messages...)
}

func (cm *ConversationMemory) Stats() (int, int, int) {
	return len(cm.messages), cm.totalTokens(), cm.maxTokens
}

// ============================================================
// 3. Prompt 模板引擎 —— text/template 构建可复用提示词
// ============================================================

type PromptLibrary struct{ templates map[string]*template.Template }

func NewPromptLibrary() *PromptLibrary {
	lib := &PromptLibrary{templates: make(map[string]*template.Template)}
	lib.MustAdd("translate", `请将以下{{.SourceLang}}文本翻译为{{.TargetLang}}：
原文：{{.Text}}
要求：保持语气风格{{if .Glossary}}
术语表：{{range .Glossary}}
  · {{.}}{{end}}{{end}}`)
	lib.MustAdd("code_review", `请审查以下 {{.Language}} 代码：
`+"```{{.Language}}\n{{.Code}}\n```"+`
审查要点：{{range .Aspects}}
- {{.}}{{end}}`)
	return lib
}

func (pl *PromptLibrary) MustAdd(name, text string) {
	pl.templates[name] = template.Must(template.New(name).Parse(text))
}

func (pl *PromptLibrary) Render(name string, data interface{}) (string, error) {
	t, ok := pl.templates[name]
	if !ok {
		return "", fmt.Errorf("模板 [%s] 不存在", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ============================================================
// 4. 多模型路由 —— 按任务复杂度选模型
// ============================================================

type ModelTier struct {
	Name     string
	CostPerM float64
	Cap      int // 能力等级 1-10
}

type ModelRouter struct{ models []ModelTier }

func NewModelRouter() *ModelRouter {
	return &ModelRouter{models: []ModelTier{
		{"gpt-3.5-turbo", 0.50, 6}, {"gpt-4o-mini", 0.15, 7},
		{"gpt-4o", 5.00, 9}, {"gpt-4-turbo", 10.00, 10},
	}}
}

// analyzeComplexity 根据消息长度、轮次和关键词评估复杂度 1-10
func (mr *ModelRouter) analyzeComplexity(msgs []Message) int {
	score, totalLen := 1, 0
	for _, m := range msgs { totalLen += len(m.Content) }
	if totalLen > 2000 { score += 3 } else if totalLen > 500 { score += 1 }
	if len(msgs) > 6 { score += 2 } else if len(msgs) > 3 { score += 1 }
	for _, m := range msgs {
		for _, kw := range []string{"分析", "推理", "代码", "翻译", "论文", "code"} {
			if strings.Contains(m.Content, kw) { score++; break }
		}
	}
	if score > 10 { score = 10 }
	return score
}

// Route 选择能力 >= 复杂度的最便宜模型
func (mr *ModelRouter) Route(msgs []Message) ModelTier {
	c := mr.analyzeComplexity(msgs)
	var best *ModelTier
	for i := range mr.models {
		m := &mr.models[i]
		if m.Cap >= c && (best == nil || m.CostPerM < best.CostPerM) { best = m }
	}
	if best == nil { best = &mr.models[len(mr.models)-1] }
	return *best
}

// ============================================================
// 5. 速率限制 —— Token Bucket 算法
// ============================================================

// RateLimiter 令牌桶限流器：满桶允许突发，空桶等待补充
type RateLimiter struct {
	mu                            sync.Mutex
	tokens, maxTokens, refillRate float64 // refillRate = 每秒补充令牌数
	lastRefill                    time.Time
}

func NewRateLimiter(max, rate float64) *RateLimiter {
	return &RateLimiter{tokens: max, maxTokens: max, refillRate: rate, lastRefill: time.Now()}
}

func (rl *RateLimiter) refill() {
	now := time.Now()
	rl.tokens = math.Min(rl.maxTokens, rl.tokens+now.Sub(rl.lastRefill).Seconds()*rl.refillRate)
	rl.lastRefill = now
}

func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	if rl.tokens >= 1 { rl.tokens--; return true }
	return false
}

func (rl *RateLimiter) WaitTime() time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	if rl.tokens >= 1 { return 0 }
	return time.Duration((1.0 - rl.tokens) / rl.refillRate * float64(time.Second))
}

func (rl *RateLimiter) Status() (float64, float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	return rl.tokens, rl.maxTokens
}

// ============================================================
// 6. 成本追踪 —— 按模型和 token 计费
// ============================================================

type CostRecord struct {
	Model                        string
	PromptTokens, CompletionTokens int
	Cost                         float64
}

type CostTracker struct {
	records []CostRecord
	pricing map[string][2]float64 // [输入价格/百万tok, 输出价格/百万tok]
}

func NewCostTracker() *CostTracker {
	return &CostTracker{pricing: map[string][2]float64{
		"gpt-3.5-turbo": {0.50, 1.50}, "gpt-4o-mini": {0.15, 0.60},
		"gpt-4o": {5.00, 15.00}, "gpt-4-turbo": {10.00, 30.00},
	}}
}

// Record 记录一次调用的成本: token数/1M * 每M价格
func (ct *CostTracker) Record(model string, prompt, comp int) CostRecord {
	p := ct.pricing[model]
	if p == [2]float64{} { p = [2]float64{1.00, 2.00} }
	r := CostRecord{Model: model, PromptTokens: prompt, CompletionTokens: comp,
		Cost: float64(prompt)/1e6*p[0] + float64(comp)/1e6*p[1]}
	ct.records = append(ct.records, r)
	return r
}

func (ct *CostTracker) Summary() (float64, int, map[string]float64) {
	var cost float64
	var tok int
	bm := make(map[string]float64)
	for _, r := range ct.records {
		cost += r.Cost
		tok += r.PromptTokens + r.CompletionTokens
		bm[r.Model] += r.Cost
	}
	return cost, tok, bm
}

// ============================================================
// Mock 服务器 —— 支持函数调用的模拟 LLM API
// ============================================================

func newMockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "无效请求", http.StatusBadRequest)
			return
		}
		lastUser, hasFR, fr := "", false, ""
		pt := 0
		for _, m := range req.Messages {
			if m.Role == "user" {
				lastUser = m.Content
			}
			if m.Role == "function" {
				hasFR, fr = true, m.Content
			}
			pt += len(m.Content) / 2
		}
		msg, finish := Message{Role: "assistant"}, "stop"
		switch {
		case len(req.Functions) > 0 && !hasFR && strings.Contains(lastUser, "天气"):
			msg.FunctionCall = &FunctionCall{"get_weather", `{"city":"北京"}`}
			finish = "function_call"
		case len(req.Functions) > 0 && !hasFR && strings.Contains(lastUser, "算"):
			msg.FunctionCall = &FunctionCall{"calculate", `{"expression":"23*17+5"}`}
			finish = "function_call"
		case hasFR:
			msg.Content = fmt.Sprintf("根据查询结果 %s，为您整理回答。", fr)
		default:
			msg.Content = fmt.Sprintf("[%s] 收到「%s」的模拟回复。", req.Model, lastUser)
		}
		ct := len(msg.Content) / 2
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatCompletion{
			ID: "mock-001", Created: time.Now().Unix(), Model: req.Model,
			Choices: []Choice{{Message: msg, FinishReason: finish}},
			Usage:   Usage{pt, ct, pt + ct},
		})
	}))
}

func sendChat(url, model string, msgs []Message, fns []FunctionDef) (*ChatCompletion, error) {
	data, _ := json.Marshal(ChatRequest{Model: model, Messages: msgs, MaxTokens: 1024, Functions: fns})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var c ChatCompletion
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &c, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// ============================================================
// 主函数 —— 演示所有高级调用技巧
// ============================================================

func main() {
	fmt.Println("=== Day 16: LLM 高级调用技巧 ===")
	fmt.Println()
	server := newMockServer()
	defer server.Close()
	fmt.Printf("🚀 Mock 服务器已启动: %s\n\n", server.URL)

	// ---- 1. 函数调用 ----
	fmt.Println("============================================================")
	fmt.Println("📋 1. 函数调用（Function Calling）")
	fmt.Println("============================================================")
	fmt.Println("💡 LLM 决定调用哪个工具，客户端执行后回传结果\n")
	tools := setupTools()
	for _, s := range tools.Schemas {
		fmt.Printf("  工具: %s — %s\n", s.Name, s.Description)
	}
	fmt.Println()
	msgs := []Message{
		{Role: "system", Content: "你是助手，可调用工具。"},
		{Role: "user", Content: "北京今天天气怎么样？"},
	}
	fmt.Println("  [步骤1] 发送用户请求...")
	resp, err := sendChat(server.URL, "gpt-4o", msgs, tools.Schemas)
	if err != nil { fmt.Printf("  ❌ 失败: %v\n", err); return }
	if ch := resp.Choices[0]; ch.FinishReason == "function_call" {
		fc := ch.Message.FunctionCall
		fmt.Printf("  [步骤2] LLM 请求调用: %s(%s)\n", fc.Name, fc.Arguments)
		result, _ := tools.Execute(fc.Name, fc.Arguments)
		fmt.Printf("  [步骤3] 执行结果: %s\n", result)
		msgs = append(msgs, ch.Message, Message{Role: "function", Name: fc.Name, Content: result})
		resp, _ = sendChat(server.URL, "gpt-4o", msgs, nil)
		fmt.Printf("  [步骤4] 最终回复: %s\n", resp.Choices[0].Message.Content)
	}

	// ---- 2. 对话记忆管理 ----
	fmt.Println("\n============================================================")
	fmt.Println("📋 2. 对话记忆管理（滑动窗口 + Token 估算）")
	fmt.Println("============================================================")
	fmt.Println("💡 自动裁剪旧消息，保持上下文在窗口内\n")
	mem := NewConversationMemory("你是 Go 导师。", 80)
	for _, m := range []Message{
		{Role: "user", Content: "什么是 goroutine？"}, {Role: "assistant", Content: "goroutine 是 Go 的轻量级线程。"},
		{Role: "user", Content: "channel 怎么用？"}, {Role: "assistant", Content: "channel 用于 goroutine 间通信。"},
		{Role: "user", Content: "什么是 select？"}, {Role: "assistant", Content: "select 监听多个 channel。"},
		{Role: "user", Content: "解释 context 包。"}, {Role: "assistant", Content: "context 传递取消信号。"},
	} {
		mem.Add(m)
		cnt, tok, mx := mem.Stats()
		fmt.Printf("  添加: %-12s | 消息:%d | Token:%d/%d\n", truncate(m.Content, 12), cnt, tok, mx)
	}
	fmt.Printf("  📊 保留 %d 条消息:\n", len(mem.GetMessages()))
	for _, m := range mem.GetMessages() {
		fmt.Printf("    [%s] %s\n", m.Role, truncate(m.Content, 30))
	}

	// ---- 3. Prompt 模板引擎 ----
	fmt.Println("\n============================================================")
	fmt.Println("📋 3. Prompt 模板引擎")
	fmt.Println("============================================================")
	fmt.Println("💡 用 text/template 构建参数化、可复用的提示词\n")
	lib := NewPromptLibrary()
	p, _ := lib.Render("translate", map[string]interface{}{
		"SourceLang": "中文", "TargetLang": "英文", "Text": "Go 是一门优秀的编程语言。",
		"Glossary": []string{"goroutine → goroutine", "channel → channel"},
	})
	fmt.Println("  🔤 翻译模板:")
	for _, l := range strings.Split(p, "\n") { fmt.Printf("    %s\n", l) }
	p, _ = lib.Render("code_review", map[string]interface{}{
		"Language": "Go", "Code": "func add(a, b int) int { return a+b }",
		"Aspects": []string{"可读性", "错误处理", "性能"},
	})
	fmt.Println("\n  🔍 代码审查模板:")
	for _, l := range strings.Split(p, "\n") { fmt.Printf("    %s\n", l) }

	// ---- 4. 多模型路由 ----
	fmt.Println("\n============================================================")
	fmt.Println("📋 4. 多模型路由")
	fmt.Println("============================================================")
	fmt.Println("💡 简单任务用便宜模型，复杂任务用强大模型\n")
	router := NewModelRouter()
	for _, tc := range []struct{ d string; m []Message }{
		{"简单问答", []Message{{Role: "user", Content: "你好"}}},
		{"代码分析", []Message{{Role: "user", Content: "请分析这段代码的并发安全性。"}}},
		{"复杂推理", []Message{
			{Role: "system", Content: "数学专家"}, {Role: "user", Content: "推理：A⊆B, B⊆C"},
			{Role: "assistant", Content: "传递性..."}, {Role: "user", Content: "数学归纳法证明"},
			{Role: "assistant", Content: "证明..."}, {Role: "user", Content: "分析边界"},
			{Role: "assistant", Content: "边界..."}, {Role: "user", Content: "翻译为英文并写论文摘要"},
		}},
	} {
		sel := router.Route(tc.m)
		fmt.Printf("  %-8s | 复杂度:%2d/10 | 模型:%-16s | $%.2f/M\n",
			tc.d, router.analyzeComplexity(tc.m), sel.Name, sel.CostPerM)
	}

	// ---- 5. 速率限制 ----
	fmt.Println("\n============================================================")
	fmt.Println("📋 5. 速率限制（Token Bucket）")
	fmt.Println("============================================================")
	fmt.Println("💡 令牌桶算法：满桶允许突发，空桶需等待\n")
	lim := NewRateLimiter(3, 2)
	for i := 1; i <= 5; i++ {
		av, mx := lim.Status()
		if lim.Allow() {
			fmt.Printf("  请求#%d: ✅ 允许 (令牌:%.0f/%.0f)\n", i, av-1, mx)
		} else {
			fmt.Printf("  请求#%d: ⏳ 限流 (令牌:%.0f/%.0f, 等待:%v)\n", i, av, mx, lim.WaitTime().Round(time.Millisecond))
		}
	}
	fmt.Println("  ... 等待 1 秒 ...")
	time.Sleep(1 * time.Second)
	av, mx := lim.Status()
	fmt.Printf("  补充后: %.0f/%.0f → 请求#6: %s\n", av, mx,
		map[bool]string{true: "✅ 允许", false: "⏳ 限流"}[lim.Allow()])

	// ---- 6. 成本追踪 ----
	fmt.Println("\n============================================================")
	fmt.Println("📋 6. 成本追踪")
	fmt.Println("============================================================")
	fmt.Println("💡 按模型和 token 用量精确计费\n")
	ct := NewCostTracker()
	for i, c := range [][3]interface{}{{"gpt-3.5-turbo", 500, 200}, {"gpt-3.5-turbo", 800, 350},
		{"gpt-4o", 1200, 500}, {"gpt-4o-mini", 600, 250}, {"gpt-4o", 2000, 800}} {
		rc := ct.Record(c[0].(string), c[1].(int), c[2].(int))
		fmt.Printf("  #%d %-14s 输入:%5d 输出:%4d 费用:$%.6f\n",
			i+1, rc.Model, rc.PromptTokens, rc.CompletionTokens, rc.Cost)
	}
	totalC, totalT, bm := ct.Summary()
	fmt.Printf("\n  📊 汇总: %d tokens, $%.6f\n", totalT, totalC)
	for m, c := range bm {
		fmt.Printf("    %-14s $%.6f\n", m, c)
	}

	// ---- 学习要点 ----
	fmt.Println("\n============================================================")
	fmt.Println("🎓 学习要点")
	fmt.Println("============================================================")
	fmt.Println("  1. Function Calling 让 LLM 安全调用外部工具")
	fmt.Println("  2. 滑动窗口 + token 估算控制对话上下文长度")
	fmt.Println("  3. text/template 实现提示词参数化和复用")
	fmt.Println("  4. 多模型路由按复杂度选择性价比最优模型")
	fmt.Println("  5. Token Bucket 算法防止 API 限流错误")
	fmt.Println("  6. 成本追踪优化 API 支出，避免预算超支")
}
