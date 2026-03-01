// Day 10 - 模拟 gRPC 服务器
// 本示例使用标准库 net/http 模拟 gRPC 的核心模式：
// - 一元 RPC（Unary RPC）：请求-响应模式
// - 服务端流式 RPC（Server Streaming RPC）：服务端分块推送
//
// ============================================================
// gRPC 原理说明：
// - gRPC 基于 HTTP/2 协议，支持多路复用和流式传输
// - 使用 Protobuf 作为默认序列化格式（本示例用 JSON 替代）
// - 一元 RPC：客户端发送一个请求，服务端返回一个响应
// - 服务端流式 RPC：客户端发一个请求，服务端返回一个响应流
// - 客户端流式 RPC：客户端发送请求流，服务端返回一个响应
// - 双向流式 RPC：客户端和服务端同时发送和接收消息流
//
// 真正的 gRPC 服务注册方式：
//
//   pb.RegisterChatServiceServer(grpcServer, &chatServer{})
//   grpcServer.Serve(listener)
//
// 真正的 gRPC 客户端调用方式：
//
//   conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
//   client := pb.NewChatServiceClient(conn)
//   resp, _ := client.Chat(ctx, &pb.ChatRequest{...})
// ============================================================

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ============================================================
// 消息定义（模拟 Protobuf 生成的结构体）
// ============================================================

// Message 单条聊天消息
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature"`
	MaxTokens   int32     `json:"max_tokens"`
	Stream      bool      `json:"stream"` // 是否使用流式响应
}

// ChatResponse 聊天响应
type ChatResponse struct {
	ID      string  `json:"id"`
	Message Message `json:"message"`
	Done    bool    `json:"done"` // 流式模式下标记是否结束
}

// ============================================================
// ChatService 服务实现
// 在真正的 gRPC 中，这会实现由 protoc 生成的接口：
//
//	type ChatServiceServer interface {
//	    Chat(context.Context, *ChatRequest) (*ChatResponse, error)
//	    ChatStream(*ChatRequest, ChatService_ChatStreamServer) error
//	}
//
// ============================================================

// ChatService 聊天服务，模拟 gRPC 服务实现
type ChatService struct {
	mu           sync.Mutex
	requestCount int // 请求计数器
}

// NewChatService 创建新的聊天服务实例
func NewChatService() *ChatService {
	return &ChatService{}
}

// Chat 处理一元 RPC 请求
// 在 gRPC 中签名为：Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
func (s *ChatService) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()

	// 检查上下文是否已取消（gRPC 中框架自动处理）
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// 模拟处理延迟（真实场景中是调用 LLM API）
	time.Sleep(100 * time.Millisecond)

	// 获取用户最后一条消息
	userMsg := "（空消息）"
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMsg = req.Messages[i].Content
			break
		}
	}

	// 构造响应
	resp := &ChatResponse{
		ID: fmt.Sprintf("chatcmpl-%d", count),
		Message: Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("收到您的消息：「%s」。这是来自 %s 的回复。", userMsg, req.Model),
			Timestamp: time.Now().UnixMilli(),
		},
		Done: true,
	}

	log.Printf("[一元RPC] 请求 #%d 处理完成，模型: %s", count, req.Model)
	return resp, nil
}

// ChatStream 处理服务端流式 RPC
// 在 gRPC 中签名为：ChatStream(req *ChatRequest, stream ChatService_ChatStreamServer) error
// stream.Send() 用于发送每个响应块
func (s *ChatService) ChatStream(ctx context.Context, req *ChatRequest, sendFunc func(*ChatResponse) error) error {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()

	log.Printf("[流式RPC] 请求 #%d 开始流式响应，模型: %s", count, req.Model)

	// 模拟流式生成：逐词发送响应
	// 在真正的 LLM 流式响应中，每个 token 生成后立即发送
	words := []string{
		"Go", "语言", "的", "goroutine", "是",
		"一种", "轻量级", "的", "并发", "原语，",
		"它", "由", "Go", "运行时", "调度，",
		"而非", "操作系统", "线程。",
	}

	for i, word := range words {
		// 检查客户端是否取消
		select {
		case <-ctx.Done():
			log.Printf("[流式RPC] 请求 #%d 被客户端取消", count)
			return ctx.Err()
		default:
		}

		// 构造流式响应块
		chunk := &ChatResponse{
			ID: fmt.Sprintf("chatcmpl-%d", count),
			Message: Message{
				Role:      "assistant",
				Content:   word,
				Timestamp: time.Now().UnixMilli(),
			},
			Done: i == len(words)-1, // 最后一个块标记完成
		}

		// 发送响应块（gRPC 中调用 stream.Send(chunk)）
		if err := sendFunc(chunk); err != nil {
			return fmt.Errorf("发送流式块失败: %w", err)
		}

		// 模拟 token 生成间隔
		time.Sleep(50 * time.Millisecond)
	}

	log.Printf("[流式RPC] 请求 #%d 流式响应完成，共 %d 个块", count, len(words))
	return nil
}

// ============================================================
// HTTP 处理器（模拟 gRPC 传输层）
// ============================================================

// handleChat 处理一元 RPC 的 HTTP 端点
// gRPC 实际使用 HTTP/2 POST 到 /包名.服务名/方法名
// 例如：POST /chatservice.ChatService/Chat
func handleChat(svc *ChatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// gRPC 只接受 POST 请求
		if r.Method != http.MethodPost {
			http.Error(w, "gRPC 仅支持 POST 方法", http.StatusMethodNotAllowed)
			return
		}

		// 解析请求体（gRPC 使用 Protobuf 反序列化）
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// gRPC 中返回 codes.InvalidArgument 状态码
			http.Error(w, fmt.Sprintf("请求解析失败: %v", err), http.StatusBadRequest)
			return
		}

		// 使用请求的上下文（支持超时和取消）
		resp, err := svc.Chat(r.Context(), &req)
		if err != nil {
			// gRPC 中返回 codes.Internal 状态码
			http.Error(w, fmt.Sprintf("处理失败: %v", err), http.StatusInternalServerError)
			return
		}

		// 返回响应（gRPC 使用 Protobuf 序列化 + HTTP/2 帧）
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("响应编码失败: %v", err)
		}
	}
}

// handleChatStream 处理服务端流式 RPC 的 HTTP 端点
// gRPC 流式响应通过 HTTP/2 的多个 DATA 帧发送
// 这里使用 HTTP/1.1 的 chunked transfer encoding 模拟
func handleChatStream(svc *ChatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "gRPC 仅支持 POST 方法", http.StatusMethodNotAllowed)
			return
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("请求解析失败: %v", err), http.StatusBadRequest)
			return
		}

		// 设置流式响应头
		// gRPC 使用 HTTP/2 的帧机制，不需要这些头
		w.Header().Set("Content-Type", "application/x-ndjson") // 换行分隔的 JSON
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// 获取 Flusher 接口，用于立即发送数据
		// gRPC 的 stream.Send() 内部处理缓冲和刷新
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式响应", http.StatusInternalServerError)
			return
		}

		// 定义发送函数（模拟 gRPC 的 stream.Send）
		sendFunc := func(resp *ChatResponse) error {
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			// 每个响应块以换行分隔（NDJSON 格式）
			_, writeErr := fmt.Fprintf(w, "%s\n", data)
			if writeErr != nil {
				return writeErr
			}
			flusher.Flush() // 立即推送到客户端
			return nil
		}

		// 调用流式处理
		if err := svc.ChatStream(r.Context(), &req, sendFunc); err != nil {
			log.Printf("流式处理错误: %v", err)
		}
	}
}

// ============================================================
// 客户端函数（模拟 gRPC 客户端调用）
// ============================================================

// callUnaryRPC 调用一元 RPC
// 真正的 gRPC 客户端：resp, err := client.Chat(ctx, req)
func callUnaryRPC(baseURL string, req *ChatRequest) (*ChatResponse, error) {
	// 序列化请求
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建带超时的上下文（gRPC 中通过 context 或 DialOption 设置）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发送请求
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/chat", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("服务端错误 (状态码 %d): %s", httpResp.StatusCode, body)
	}

	// 反序列化响应
	var resp ChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("反序列化响应失败: %w", err)
	}

	return &resp, nil
}

// callStreamingRPC 调用服务端流式 RPC
// 真正的 gRPC 客户端：
//
//	stream, err := client.ChatStream(ctx, req)
//	for {
//	    resp, err := stream.Recv()
//	    if err == io.EOF { break }
//	    // 处理 resp
//	}
func callStreamingRPC(baseURL string, req *ChatRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/chat/stream", strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	// 逐行读取流式响应（模拟 gRPC 的 stream.Recv()）
	scanner := bufio.NewScanner(httpResp.Body)
	fmt.Print("  流式响应: ")
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return fmt.Errorf("解析流式块失败: %w", err)
		}

		// 打印每个 token（模拟实时输出）
		fmt.Print(chunk.Message.Content)

		if chunk.Done {
			fmt.Println(" [完成]")
		}
	}

	return scanner.Err()
}

// ============================================================
// 主函数
// ============================================================

func main() {
	fmt.Println("=== Day 10: 模拟 gRPC 服务器 ===")
	fmt.Println()

	// 创建服务实例
	svc := NewChatService()

	// 注册路由（模拟 gRPC 服务注册）
	// 真正的 gRPC：pb.RegisterChatServiceServer(s, svc)
	mux := http.NewServeMux()
	mux.HandleFunc("/chat", handleChat(svc))
	mux.HandleFunc("/chat/stream", handleChatStream(svc))

	// 动态分配端口，避免端口冲突
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// 创建 HTTP 服务器（模拟 gRPC 服务器）
	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 启动服务器（后台运行）
	go func() {
		log.Printf("服务器启动在 %s", baseURL)
		log.Printf("  一元 RPC:   POST %s/chat", baseURL)
		log.Printf("  流式 RPC:   POST %s/chat/stream", baseURL)
		if err := server.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()

	// 等待服务器就绪
	time.Sleep(100 * time.Millisecond)

	// ----------------------------------------------------------
	// 演示 1：一元 RPC 调用
	// ----------------------------------------------------------
	fmt.Println("--- 一元 RPC 调用 ---")

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "你是一个Go语言专家。", Timestamp: time.Now().UnixMilli()},
			{Role: "user", Content: "什么是 gRPC？", Timestamp: time.Now().UnixMilli()},
		},
		Temperature: 0.7,
		MaxTokens:   1024,
	}

	resp, err := callUnaryRPC(baseURL, req)
	if err != nil {
		log.Printf("一元 RPC 失败: %v", err)
	} else {
		fmt.Printf("  响应 ID: %s\n", resp.ID)
		fmt.Printf("  内容: %s\n", resp.Message.Content)
	}
	fmt.Println()

	// ----------------------------------------------------------
	// 演示 2：服务端流式 RPC 调用
	// ----------------------------------------------------------
	fmt.Println("--- 服务端流式 RPC 调用 ---")

	streamReq := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "解释 goroutine", Timestamp: time.Now().UnixMilli()},
		},
		Temperature: 0.7,
		MaxTokens:   2048,
		Stream:      true,
	}

	if err := callStreamingRPC(baseURL, streamReq); err != nil {
		log.Printf("流式 RPC 失败: %v", err)
	}
	fmt.Println()

	// ----------------------------------------------------------
	// 演示 3：并发请求（模拟多个 gRPC 客户端）
	// ----------------------------------------------------------
	fmt.Println("--- 并发一元 RPC 调用 ---")

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r := &ChatRequest{
				Model: "gpt-4",
				Messages: []Message{
					{Role: "user", Content: fmt.Sprintf("问题 %d", id), Timestamp: time.Now().UnixMilli()},
				},
			}
			resp, err := callUnaryRPC(baseURL, r)
			if err != nil {
				fmt.Printf("  客户端 %d 失败: %v\n", id, err)
			} else {
				fmt.Printf("  客户端 %d 收到: %s\n", id, resp.Message.Content)
			}
		}(i)
	}
	wg.Wait()
	fmt.Println()

	// ----------------------------------------------------------
	// 优雅关闭（gRPC 也支持 GracefulStop）
	// ----------------------------------------------------------
	fmt.Println("--- 优雅关闭服务器 ---")

	// 设置信号处理（在实际服务中使用）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 这里直接关闭（演示用途）
	// 在生产环境中，会等待 sigCh 信号
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭服务器失败: %v", err)
	} else {
		fmt.Println("  服务器已优雅关闭")
	}

	// 停止信号监听
	signal.Stop(sigCh)
	fmt.Println()

	// ----------------------------------------------------------
	// 总结
	// ----------------------------------------------------------
	fmt.Println("=== 总结 ===")
	fmt.Println("1. 一元 RPC: 最简单的模式，一个请求对应一个响应")
	fmt.Println("2. 服务端流式 RPC: 适合流式生成（如 LLM token 流）")
	fmt.Println("3. gRPC 基于 HTTP/2，自动支持多路复用和流控")
	fmt.Println("4. 使用 context 实现超时控制和请求取消")
	fmt.Println("5. GracefulStop/Shutdown 确保处理完进行中的请求")
}
