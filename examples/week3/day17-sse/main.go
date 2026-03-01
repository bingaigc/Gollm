// Day 17 - SSE (Server-Sent Events) 流式响应处理
//
// 本示例演示 LLM 流式输出的核心技术 —— Server-Sent Events：
// - SSE 服务器：模拟 LLM 逐字输出的流式响应
// - SSE 客户端：使用 bufio.Scanner 逐行解析事件流
// - 处理边界情况：空行、注释行、[DONE] 终止信号
// - StreamReader 封装：按 delta 内容块逐步产出
// - 终端逐字打印效果，展示流式体验
// - 上下文取消与连接超时机制
//
// 🔑 核心概念：LLM 生成文本是逐 token 进行的，SSE 允许服务器
// 在生成过程中实时推送部分结果，用户无需等待全部生成完毕。
// SSE 协议格式：每条事件以 "data: " 前缀开头，空行分隔事件。

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// ============================================================
// SSE 数据结构 —— 兼容 OpenAI 流式响应格式
// ============================================================

// StreamDelta 表示流式输出中的一个增量内容片段
// 与非流式不同，流式响应中每个 chunk 只包含新增的部分（delta）
type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// StreamChoice 表示流式响应中的一个选择
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// StreamChunk 表示 SSE 流中的一个数据块
// 每个 chunk 对应一条 "data: {...}" 消息
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// ============================================================
// SSE 服务器 —— 模拟 LLM 的流式输出行为
// ============================================================

// newSSEServer 创建模拟 LLM 流式输出的 SSE 服务器
func newSSEServer() *httptest.Server {
	handler := http.NewServeMux()

	handler.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// 设置 SSE 必需的响应头
		// Content-Type 必须为 text/event-stream
		// Cache-Control 禁止缓存，确保实时性
		// Connection keep-alive 保持长连接
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Flusher 接口用于立即将缓冲区数据发送给客户端
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式响应", http.StatusInternalServerError)
			return
		}

		// 模拟 LLM 逐步生成的文本片段
		tokens := []string{
			"Go", " 语言", "是", "一门", "简洁", "高效", "的",
			"编程", "语言，", "特别", "适合", "构建", "高并发",
			"的", "网络", "服务", "和", "云原生", "应用。",
			"它", "由", " Google", " 开发，", "拥有", "出色",
			"的", "工具链", "和", "标准库。",
		}

		// 发送 SSE 注释行（以冒号开头）—— 客户端应忽略此类行
		// 注释常用于保持连接活跃（心跳）
		fmt.Fprintf(w, ": 这是一条 SSE 注释，客户端应忽略\n\n")
		flusher.Flush()

		// 第一个 chunk：发送角色信息（role）
		firstChunk := StreamChunk{
			ID:      "chatcmpl-stream-001",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "mock-gpt-4o",
			Choices: []StreamChoice{
				{Index: 0, Delta: StreamDelta{Role: "assistant"}, FinishReason: nil},
			},
		}
		sendSSEChunk(w, flusher, firstChunk)

		// 逐个 token 发送内容 —— 模拟 LLM 逐步生成
		for _, token := range tokens {
			// 检查客户端是否已断开连接
			select {
			case <-r.Context().Done():
				return
			default:
			}

			chunk := StreamChunk{
				ID:      "chatcmpl-stream-001",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   "mock-gpt-4o",
				Choices: []StreamChoice{
					{Index: 0, Delta: StreamDelta{Content: token}, FinishReason: nil},
				},
			}
			sendSSEChunk(w, flusher, chunk)

			// 模拟 token 生成的延迟（真实 LLM 每个 token 约 20-50ms）
			time.Sleep(30 * time.Millisecond)
		}

		// 发送结束 chunk，FinishReason 设为 "stop"
		stopReason := "stop"
		finalChunk := StreamChunk{
			ID:      "chatcmpl-stream-001",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "mock-gpt-4o",
			Choices: []StreamChoice{
				{Index: 0, Delta: StreamDelta{}, FinishReason: &stopReason},
			},
		}
		sendSSEChunk(w, flusher, finalChunk)

		// 发送 [DONE] 终止信号 —— 这是 OpenAI SSE 协议的约定
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	return httptest.NewServer(handler)
}

// sendSSEChunk 将一个数据块以 SSE 格式写入响应
// SSE 格式要求：以 "data: " 开头，以两个换行符结尾
func sendSSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk StreamChunk) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// ============================================================
// StreamReader —— SSE 客户端的核心封装
// ============================================================

// StreamReader 从 SSE 事件流中逐块读取内容
// 封装了 SSE 协议解析的全部细节，对外提供简洁的 ReadChunk 接口
type StreamReader struct {
	scanner *bufio.Scanner
	body    interface{ Close() error }
	done    bool // 是否已收到 [DONE] 信号
}

// NewStreamReader 从 HTTP 响应创建 StreamReader
func NewStreamReader(resp *http.Response) *StreamReader {
	return &StreamReader{
		scanner: bufio.NewScanner(resp.Body),
		body:    resp.Body,
	}
}

// ReadChunk 读取下一个内容块
// 返回值：content 为增量文本，done 为 true 表示流已结束
// 自动跳过空行、注释行，并解析 JSON 数据
func (sr *StreamReader) ReadChunk() (content string, finishReason string, done bool, err error) {
	for sr.scanner.Scan() {
		line := sr.scanner.Text()

		// 空行：SSE 事件之间的分隔符，跳过
		if line == "" {
			continue
		}

		// 注释行：以冒号开头，用于心跳等目的，跳过
		if strings.HasPrefix(line, ":") {
			continue
		}

		// 非 data 行：不符合 SSE 协议，跳过
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// 提取 data 字段内容
		data := strings.TrimPrefix(line, "data: ")

		// [DONE] 信号：流式传输结束
		if data == "[DONE]" {
			sr.done = true
			return "", "", true, nil
		}

		// 解析 JSON 数据块
		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", "", false, fmt.Errorf("解析 SSE 数据失败: %w", err)
		}

		// 提取增量内容
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			reason := ""
			if choice.FinishReason != nil {
				reason = *choice.FinishReason
			}
			return choice.Delta.Content, reason, false, nil
		}
	}

	// scanner 结束（连接关闭）
	if err := sr.scanner.Err(); err != nil {
		return "", "", true, fmt.Errorf("读取 SSE 流出错: %w", err)
	}
	return "", "", true, nil
}

// Close 关闭底层连接
func (sr *StreamReader) Close() error {
	return sr.body.Close()
}

// ============================================================
// 演示函数
// ============================================================

// demonstrateSSEStream 演示完整的 SSE 流式读取过程
func demonstrateSSEStream(serverURL string) error {
	fmt.Println("--- 流式响应演示 ---")
	fmt.Println("🔄 正在连接 SSE 流...")

	// 创建带超时的上下文（10 秒超时防止无限等待）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 构建请求
	reqBody := `{"model":"mock-gpt-4o","messages":[{"role":"user","content":"介绍Go"}],"stream":true}`
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/chat/completions",
		strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}

	// 使用 StreamReader 逐块读取
	reader := NewStreamReader(resp)
	defer reader.Close()

	fmt.Print("  AI: ")
	var fullContent strings.Builder
	chunkCount := 0

	startTime := time.Now()

	for {
		content, finishReason, done, err := reader.ReadChunk()
		if err != nil {
			return fmt.Errorf("读取流失败: %w", err)
		}
		if done {
			break
		}

		// 逐字打印到终端 —— 模拟打字机效果
		if content != "" {
			fmt.Print(content)
			fullContent.WriteString(content)
			chunkCount++
		}

		// 展示 finishReason
		if finishReason == "stop" {
			fmt.Print(" [结束]")
		}
	}

	elapsed := time.Since(startTime)
	fmt.Println()
	fmt.Printf("\n  📊 流式统计：共 %d 个数据块，耗时 %v\n", chunkCount, elapsed.Round(time.Millisecond))
	fmt.Printf("  📝 完整内容长度：%d 字符\n\n", fullContent.Len())

	return nil
}

// demonstrateContextCancel 演示上下文取消（中断流式传输）
func demonstrateContextCancel(serverURL string) error {
	fmt.Println("--- 上下文取消演示 ---")
	fmt.Println("🔄 连接 SSE 流，300ms 后取消...")

	// 创建一个 300ms 后自动取消的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	reqBody := `{"model":"mock-gpt-4o","messages":[{"role":"user","content":"test"}],"stream":true}`
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/chat/completions",
		strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}

	reader := NewStreamReader(resp)
	defer reader.Close()

	fmt.Print("  AI: ")
	chunkCount := 0

	for {
		content, _, done, err := reader.ReadChunk()
		if err != nil {
			// 上下文取消会导致读取错误，这是预期行为
			fmt.Printf("\n  ⚠️ 流被中断: %v\n", err)
			break
		}
		if done {
			break
		}
		if content != "" {
			fmt.Print(content)
			chunkCount++
		}
	}

	fmt.Printf("\n  📊 取消前收到 %d 个数据块\n\n", chunkCount)
	return nil
}

// demonstrateBackpressure 演示背压处理概念
func demonstrateBackpressure(serverURL string) {
	fmt.Println("--- 背压处理概念演示 ---")
	fmt.Println("💡 背压（Backpressure）是指当消费者处理速度跟不上生产者时的应对策略")
	fmt.Println()

	// 使用带缓冲的通道模拟背压
	// 缓冲区大小限制了未处理数据块的数量
	bufferSize := 5
	chunks := make(chan string, bufferSize)
	done := make(chan struct{})

	// 生产者：模拟快速生成 token
	go func() {
		tokens := []string{"Go", " 是", "一门", "优秀", "的", "语言", "！"}
		for _, t := range tokens {
			chunks <- t // 缓冲区满时会阻塞，实现背压
			fmt.Printf("  [生产者] 发送: %q (缓冲: %d/%d)\n", t, len(chunks), bufferSize)
		}
		close(chunks)
	}()

	// 消费者：模拟较慢的处理速度
	go func() {
		fmt.Print("  [消费者] 输出: ")
		for chunk := range chunks {
			fmt.Print(chunk)
			time.Sleep(50 * time.Millisecond) // 消费者处理较慢
		}
		fmt.Println()
		close(done)
	}()

	<-done
	fmt.Println("  ✅ 背压演示完成：通道缓冲自动协调了生产和消费速度")
	fmt.Println()
}

func main() {
	fmt.Println("=== Day 17: SSE 流式响应处理 ===")
	fmt.Println()

	// SSE 协议简介
	fmt.Println("📖 SSE 协议格式说明：")
	fmt.Println("  • 每条事件以 \"data: \" 前缀开头")
	fmt.Println("  • 事件之间用空行分隔")
	fmt.Println("  • 以冒号开头的行是注释（用于心跳）")
	fmt.Println("  • \"data: [DONE]\" 表示流结束")
	fmt.Println()

	// 启动 SSE 服务器
	server := newSSEServer()
	defer server.Close()
	fmt.Printf("🚀 SSE 服务器已启动: %s\n\n", server.URL)

	// 1. 完整的流式读取演示
	if err := demonstrateSSEStream(server.URL); err != nil {
		fmt.Printf("❌ 流式演示失败: %v\n\n", err)
	}

	// 2. 上下文取消演示
	if err := demonstrateContextCancel(server.URL); err != nil {
		fmt.Printf("❌ 取消演示失败: %v\n\n", err)
	}

	// 3. 背压处理演示
	demonstrateBackpressure(server.URL)

	fmt.Println("🎓 学习要点：")
	fmt.Println("  1. SSE 是 LLM 流式输出的标准协议，基于 HTTP 长连接")
	fmt.Println("  2. bufio.Scanner 是解析 SSE 流的理想工具")
	fmt.Println("  3. context 用于控制超时和取消，防止资源泄漏")
	fmt.Println("  4. 背压通过有缓冲通道自然实现生产-消费速率协调")
}
