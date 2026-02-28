// Day 15 - LLM API 客户端实现
//
// 本示例演示如何用 Go 构建一个大语言模型 (LLM) API 客户端：
// - 定义与 OpenAI 兼容的请求/响应结构体
// - 实现支持多模型提供商的通用客户端
// - 自定义错误类型与令牌用量追踪
// - 使用内置 Mock 服务器进行本地演示
//
// 🔑 核心概念：ChatCompletion 是 LLM 最常用的 API 模式，
// 客户端发送一组消息（对话历史），服务器返回模型生成的回复。

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// ============================================================
// 数据结构定义 —— 兼容 OpenAI ChatCompletion API 格式
// ============================================================

// Message 表示对话中的一条消息
// role 字段常见取值：system（系统提示）、user（用户输入）、assistant（模型回复）
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 是发送给 LLM 的聊天请求体
// Temperature 控制随机性：0 = 确定性输出，1 = 更随机
// MaxTokens 限制回复最大 token 数，防止过长回复
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// Usage 记录本次请求的 token 用量
// LLM API 按 token 计费，追踪用量对成本控制至关重要
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice 表示模型返回的一个候选回复
// FinishReason 常见值：stop（正常结束）、length（达到最大长度）
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ChatCompletion 是 LLM 返回的完整响应
// 包含唯一 ID、创建时间戳、模型名称、候选回复列表和 token 用量
type ChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// ============================================================
// 自定义错误类型 —— 精确描述 API 调用失败的原因
// ============================================================

// APIError 表示 LLM API 返回的错误
// StatusCode 用于区分不同类型的错误（401 = 鉴权失败，429 = 限流等）
type APIError struct {
	StatusCode int
	Message    string
	Provider   string
}

// Error 实现 error 接口，格式化输出错误信息
func (e *APIError) Error() string {
	return fmt.Sprintf("[%s] API 错误 (HTTP %d): %s", e.Provider, e.StatusCode, e.Message)
}

// ============================================================
// LLM 客户端实现
// ============================================================

// LLMClient 是通用 LLM API 客户端
// 通过配置 baseURL 和 apiKey 可以对接不同的 LLM 提供商
type LLMClient struct {
	baseURL    string       // API 基础地址，例如 https://api.openai.com/v1
	apiKey     string       // API 密钥，用于身份验证
	model      string       // 默认使用的模型名称
	httpClient *http.Client // 底层 HTTP 客户端，可配置超时
	provider   string       // 提供商名称，用于错误信息展示

	// token 用量统计（累计值）
	totalPromptTokens     int
	totalCompletionTokens int
}

// NewLLMClient 创建一个新的 LLM 客户端
// 设置合理的默认值：30 秒超时防止请求挂起
func NewLLMClient(baseURL, apiKey, model, provider string) *LLMClient {
	return &LLMClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		model:    model,
		provider: provider,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Chat 发送聊天请求并返回模型回复
// 这是客户端的核心方法，完整流程：构建请求 → 发送 → 解析响应 → 错误处理
func (c *LLMClient) Chat(messages []Message) (*ChatCompletion, error) {
	// 1. 构建请求体
	reqBody := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   1024,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 2. 创建 HTTP 请求
	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置必要的请求头
	// Authorization 头使用 Bearer Token 方式传递 API 密钥
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// 3. 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求发送失败: %w", err)
	}
	defer resp.Body.Close()

	// 4. 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 5. 检查 HTTP 状态码，非 200 表示出错
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			Provider:   c.provider,
		}
	}

	// 6. 解析 JSON 响应
	var completion ChatCompletion
	if err := json.Unmarshal(body, &completion); err != nil {
		return nil, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	// 7. 累计 token 用量
	c.totalPromptTokens += completion.Usage.PromptTokens
	c.totalCompletionTokens += completion.Usage.CompletionTokens

	return &completion, nil
}

// GetUsageStats 返回累计的 token 用量统计
func (c *LLMClient) GetUsageStats() (promptTokens, completionTokens, totalTokens int) {
	return c.totalPromptTokens, c.totalCompletionTokens,
		c.totalPromptTokens + c.totalCompletionTokens
}

// ============================================================
// Mock 服务器 —— 模拟 LLM API 的行为，用于本地测试
// ============================================================

// newMockLLMServer 创建一个模拟的 LLM API 服务器
// 在没有真实 API 密钥的情况下也能完整演示客户端功能
func newMockLLMServer() *httptest.Server {
	handler := http.NewServeMux()

	handler.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
			return
		}

		// 验证 Authorization 头
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error": "缺少或无效的 API 密钥"}`)
			return
		}

		// 模拟鉴权失败场景
		apiKey := strings.TrimPrefix(auth, "Bearer ")
		if apiKey == "invalid-key" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error": "无效的 API 密钥"}`)
			return
		}

		// 解析请求体
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "无效的请求体", http.StatusBadRequest)
			return
		}

		// 根据用户最后一条消息生成模拟回复
		lastMsg := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				lastMsg = m.Content
			}
		}

		replyContent := generateMockReply(lastMsg, req.Model)

		// 模拟 token 计算（简单按字符数估算）
		promptTokens := 0
		for _, m := range req.Messages {
			promptTokens += len(m.Content) / 2
		}
		completionTokens := len(replyContent) / 2

		// 构建响应
		completion := ChatCompletion{
			ID:      "chatcmpl-mock-001",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []Choice{
				{
					Index: 0,
					Message: Message{
						Role:    "assistant",
						Content: replyContent,
					},
					FinishReason: "stop",
				},
			},
			Usage: Usage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(completion)
	})

	return httptest.NewServer(handler)
}

// generateMockReply 根据用户输入生成模拟回复
func generateMockReply(userMsg, model string) string {
	// 模拟不同模型的回复风格
	prefix := fmt.Sprintf("[模型 %s 回复] ", model)

	if strings.Contains(userMsg, "你好") || strings.Contains(userMsg, "hello") {
		return prefix + "你好！我是 AI 助手，很高兴为你服务。有什么我可以帮助你的吗？"
	}
	if strings.Contains(userMsg, "Go") || strings.Contains(userMsg, "golang") {
		return prefix + "Go 语言是 Google 开发的静态类型编程语言，以并发编程、简洁语法和高性能著称。它非常适合构建微服务、CLI 工具和云原生应用。"
	}
	if strings.Contains(userMsg, "天气") {
		return prefix + "抱歉，我无法查询实时天气。不过你可以通过工具调用（Function Calling）让我接入天气 API 来获取实时数据。"
	}
	return prefix + "这是一个很好的问题！作为 AI 助手，我会尽力为你提供有用的信息。"
}

// ============================================================
// 提供商配置 —— 展示如何切换不同的 LLM 提供商
// ============================================================

// ProviderConfig 定义 LLM 提供商的配置信息
type ProviderConfig struct {
	Name    string // 提供商名称
	BaseURL string // API 地址
	Model   string // 推荐模型
}

// 常见的 LLM 提供商配置（仅作展示，实际使用需要有效的 API 密钥）
var providers = []ProviderConfig{
	{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
	{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
	{Name: "Moonshot", BaseURL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-8k"},
}

func main() {
	fmt.Println("=== Day 15: LLM API 客户端实现 ===")
	fmt.Println()

	// ---- 1. 展示支持的 LLM 提供商 ----
	fmt.Println("📋 支持的 LLM 提供商：")
	for _, p := range providers {
		fmt.Printf("  • %-10s  模型: %-20s  地址: %s\n", p.Name, p.Model, p.BaseURL)
	}
	fmt.Println()

	// ---- 2. 启动 Mock 服务器 ----
	server := newMockLLMServer()
	defer server.Close()
	fmt.Printf("🚀 Mock LLM 服务器已启动: %s\n\n", server.URL)

	// ---- 3. 创建客户端并发送请求 ----
	client := NewLLMClient(server.URL, "test-api-key-123", "gpt-4o-mock", "MockLLM")

	// 第一轮对话：简单问候
	fmt.Println("--- 第一轮对话 ---")
	messages := []Message{
		{Role: "system", Content: "你是一个有帮助的 AI 助手。"},
		{Role: "user", Content: "你好，请介绍一下你自己"},
	}

	resp, err := client.Chat(messages)
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		return
	}
	printCompletion(resp)

	// 第二轮对话：技术问题
	fmt.Println("--- 第二轮对话 ---")
	messages = append(messages, resp.Choices[0].Message)
	messages = append(messages, Message{Role: "user", Content: "请介绍一下 Go 语言的特点"})

	resp, err = client.Chat(messages)
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		return
	}
	printCompletion(resp)

	// 第三轮对话：测试工具调用场景
	fmt.Println("--- 第三轮对话 ---")
	resp, err = client.Chat([]Message{
		{Role: "user", Content: "今天天气怎么样？"},
	})
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		return
	}
	printCompletion(resp)

	// ---- 4. 展示 token 用量统计 ----
	prompt, completion, total := client.GetUsageStats()
	fmt.Println("=== Token 用量统计 ===")
	fmt.Printf("  输入 tokens:  %d\n", prompt)
	fmt.Printf("  输出 tokens:  %d\n", completion)
	fmt.Printf("  总计 tokens:  %d\n\n", total)

	// ---- 5. 演示错误处理 ----
	fmt.Println("=== 错误处理演示 ===")

	// 5a. 使用无效 API 密钥
	badClient := NewLLMClient(server.URL, "invalid-key", "gpt-4o", "MockLLM")
	_, err = badClient.Chat([]Message{{Role: "user", Content: "test"}})
	if err != nil {
		// 类型断言检查是否为 APIError
		if apiErr, ok := err.(*APIError); ok {
			fmt.Printf("  ✅ 捕获到 API 错误 — 状态码: %d, 提供商: %s\n", apiErr.StatusCode, apiErr.Provider)
		}
		fmt.Printf("  错误详情: %v\n", err)
	}

	// 5b. 连接不可达的服务器
	fmt.Println()
	unreachable := NewLLMClient("http://localhost:1", "key", "model", "Unreachable")
	unreachable.httpClient.Timeout = 2 * time.Second
	_, err = unreachable.Chat([]Message{{Role: "user", Content: "test"}})
	if err != nil {
		fmt.Printf("  ✅ 捕获到连接错误: %v\n\n", err)
	}

	// ---- 6. 演示切换提供商 ----
	fmt.Println("=== 切换提供商演示 ===")
	fmt.Println("💡 通过更换 baseURL 和 model 即可切换不同 LLM 提供商：")
	for _, p := range providers {
		c := NewLLMClient(p.BaseURL, "your-api-key", p.Model, p.Name)
		fmt.Printf("  客户端 [%s] → 地址: %s, 模型: %s\n", c.provider, c.baseURL, c.model)
	}

	fmt.Println("\n🎓 学习要点：")
	fmt.Println("  1. ChatCompletion 是 LLM API 的标准交互模式")
	fmt.Println("  2. 通过 baseURL 抽象可以轻松切换不同提供商")
	fmt.Println("  3. token 用量追踪对成本管控非常重要")
	fmt.Println("  4. 完善的错误处理让客户端更加健壮")
}

// printCompletion 格式化输出 ChatCompletion 响应
func printCompletion(c *ChatCompletion) {
	if len(c.Choices) == 0 {
		fmt.Println("  (无回复)")
		return
	}
	fmt.Printf("  模型: %s\n", c.Model)
	fmt.Printf("  回复: %s\n", c.Choices[0].Message.Content)
	fmt.Printf("  结束原因: %s\n", c.Choices[0].FinishReason)
	fmt.Printf("  Token 用量: 输入=%d, 输出=%d, 总计=%d\n\n",
		c.Usage.PromptTokens, c.Usage.CompletionTokens, c.Usage.TotalTokens)
}
