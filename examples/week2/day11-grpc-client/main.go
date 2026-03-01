// Day 11 - gRPC 客户端开发
// 本示例使用标准库 net/http 模拟 gRPC 客户端核心模式：
// - 连接管理（Dial、连接池、重连）
// - 一元 RPC（Unary RPC）调用
// - 服务端流式 RPC（Server Streaming）
// - 超时与截止时间（context 控制）
// - 重试策略（指数退避 Exponential Backoff）
// - 负载均衡（Round-Robin）
//
// ============================================================
// 真正的 gRPC 客户端代码：
//
//   conn, _ := grpc.Dial("localhost:50051",
//       grpc.WithTransportCredentials(insecure.NewCredentials()),
//       grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
//   )
//   client := pb.NewChatServiceClient(conn)
//   resp, _ := client.Chat(ctx, &pb.ChatRequest{...})
//
//   stream, _ := client.ChatStream(ctx, &pb.ChatRequest{...})
//   for { resp, err := stream.Recv(); if err == io.EOF { break } }
// ============================================================

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// 消息定义（模拟 Protobuf 生成的结构体）
// ============================================================

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string  `json:"id"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// ============================================================
// 1. 连接管理（模拟 gRPC Dial 和连接池）
// gRPC 底层维护 HTTP/2 长连接，支持连接复用和自动重连
// ============================================================

// ConnState 连接状态（模拟 gRPC connectivity.State）
type ConnState int

const (
	StateIdle       ConnState = iota // 空闲（gRPC: IDLE）
	StateConnecting                  // 连接中（gRPC: CONNECTING）
	StateReady                       // 就绪（gRPC: READY）
	StateDown                        // 短暂故障（gRPC: TRANSIENT_FAILURE）
	StateShutdown                    // 已关闭（gRPC: SHUTDOWN）
)

func (s ConnState) String() string {
	names := []string{"IDLE", "CONNECTING", "READY", "TRANSIENT_FAILURE", "SHUTDOWN"}
	if int(s) < len(names) {
		return names[s]
	}
	return "UNKNOWN"
}

// ClientConn 模拟 grpc.ClientConn
type ClientConn struct {
	target     string
	state      ConnState
	mu         sync.RWMutex
	httpClient *http.Client
}

// Dial 模拟 grpc.Dial()，创建客户端连接
func Dial(target string) (*ClientConn, error) {
	conn := &ClientConn{
		target: target,
		state:  StateReady,
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        10,               // 最大空闲连接数
				MaxIdleConnsPerHost: 5,                // 每主机空闲连接数
				IdleConnTimeout:     90 * time.Second, // 空闲超时
			},
			Timeout: 30 * time.Second,
		},
	}
	fmt.Printf("  📡 Dial(%s) → %s\n", target, conn.state)
	return conn, nil
}

func (c *ClientConn) GetState() ConnState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *ClientConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = StateShutdown
	c.httpClient.CloseIdleConnections()
	fmt.Printf("  🔌 连接已关闭: %s\n", c.target)
}

// reconnect 模拟自动重连（gRPC 内置指数退避重连）
func (c *ClientConn) reconnect() {
	c.mu.Lock()
	c.state = StateConnecting
	c.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	c.state = StateReady
	c.mu.Unlock()
	fmt.Printf("  🔄 重连成功 → %s\n", c.GetState())
}

// ============================================================
// 2. 一元调用 & 3. 流式调用（客户端存根）
// ============================================================

// ChatServiceClient 模拟 protoc 生成的客户端存根
type ChatServiceClient struct {
	conn *ClientConn
}

func NewChatServiceClient(conn *ClientConn) *ChatServiceClient {
	return &ChatServiceClient{conn: conn}
}

// Chat 一元 RPC — 真正的 gRPC: resp, err := client.Chat(ctx, req)
func (c *ChatServiceClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if c.conn.GetState() != StateReady {
		return nil, fmt.Errorf("连接不可用，状态: %s", c.conn.GetState())
	}
	data, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.conn.target+"/chat", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.conn.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("RPC 调用失败: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("RPC 错误(%d): %s", httpResp.StatusCode, body)
	}
	var resp ChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("反序列化失败: %w", err)
	}
	return &resp, nil
}

// StreamReader 模拟 gRPC 流读取器，通过 Recv() 逐个读取响应
type StreamReader struct {
	scanner *bufio.Scanner
	body    io.ReadCloser
}

// Recv 读取下一个响应块，io.EOF 表示流结束
func (s *StreamReader) Recv() (*ChatResponse, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			continue
		}
		var resp ChatResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("解析流式块失败: %w", err)
		}
		return &resp, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (s *StreamReader) Close() { s.body.Close() }

// ChatStream 服务端流式 RPC — 真正的 gRPC: stream, err := client.ChatStream(ctx, req)
func (c *ChatServiceClient) ChatStream(ctx context.Context, req *ChatRequest) (*StreamReader, error) {
	if c.conn.GetState() != StateReady {
		return nil, fmt.Errorf("连接不可用，状态: %s", c.conn.GetState())
	}
	data, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.conn.target+"/chat/stream", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.conn.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("流式 RPC 失败: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("流式错误(%d): %s", httpResp.StatusCode, body)
	}
	return &StreamReader{scanner: bufio.NewScanner(httpResp.Body), body: httpResp.Body}, nil
}

// ============================================================
// 5. 重试策略（Exponential Backoff）
// 退避公式：wait = min(initial × multiplier^attempt, max) ± jitter
// ============================================================

type RetryConfig struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	Jitter            bool // 随机抖动避免雷群效应（Thundering Herd）
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 4, InitialBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second, BackoffMultiplier: 2.0, Jitter: true,
	}
}

func callWithRetry(client *ChatServiceClient, req *ChatRequest, cfg *RetryConfig) (*ChatResponse, error) {
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := float64(cfg.InitialBackoff) * math.Pow(cfg.BackoffMultiplier, float64(attempt-1))
			if backoff > float64(cfg.MaxBackoff) {
				backoff = float64(cfg.MaxBackoff)
			}
			if cfg.Jitter {
				backoff += backoff * 0.25 * (rand.Float64()*2 - 1)
			}
			wait := time.Duration(backoff)
			fmt.Printf("    🔄 重试 %d/%d，退避 %v\n", attempt, cfg.MaxAttempts-1, wait.Round(time.Millisecond))
			time.Sleep(wait)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := client.Chat(ctx, req)
		// 必须在 cancel() 前捕获：cancel() 后 ctx.Err() 总返回 Canceled
		ctxErr := ctx.Err()
		cancel()
		if err == nil {
			if attempt > 0 {
				fmt.Printf("    ✅ 重试 %d 成功\n", attempt)
			}
			return resp, nil
		}
		lastErr = err
		fmt.Printf("    ❌ 尝试 %d 失败: %v\n", attempt+1, err)
		if ctxErr == context.DeadlineExceeded {
			continue
		}
	}
	return nil, fmt.Errorf("重试 %d 次失败: %w", cfg.MaxAttempts, lastErr)
}

// ============================================================
// 6. 负载均衡（Round-Robin）
// gRPC: grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`)
// ============================================================

type RoundRobinBalancer struct {
	backends []string
	counter  uint64
}

func (b *RoundRobinBalancer) Pick() string {
	idx := atomic.AddUint64(&b.counter, 1) - 1
	return b.backends[idx%uint64(len(b.backends))]
}

type BalancedClient struct {
	balancer *RoundRobinBalancer
	clients  map[string]*ChatServiceClient
}

func NewBalancedClient(backends []string) (*BalancedClient, error) {
	clients := make(map[string]*ChatServiceClient)
	for _, addr := range backends {
		conn, err := Dial(addr)
		if err != nil {
			return nil, err
		}
		clients[addr] = NewChatServiceClient(conn)
	}
	return &BalancedClient{
		balancer: &RoundRobinBalancer{backends: backends},
		clients:  clients,
	}, nil
}

func (bc *BalancedClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, string, error) {
	backend := bc.balancer.Pick()
	resp, err := bc.clients[backend].Chat(ctx, req)
	return resp, backend, err
}

// ============================================================
// Mock 服务器
// ============================================================

func startMockServer() (string, func()) {
	var reqCount int64
	mux := http.NewServeMux()

	// 一元 RPC
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		time.Sleep(50 * time.Millisecond)
		n := atomic.AddInt64(&reqCount, 1)
		userMsg := "（空）"
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				userMsg = req.Messages[i].Content
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			ID:      fmt.Sprintf("resp-%d", n),
			Message: Message{Role: "assistant", Content: fmt.Sprintf("[%s] 回复「%s」(#%d)", req.Model, userMsg, n)},
			Done:    true,
		})
	})

	// 流式 RPC
	mux.HandleFunc("/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		n := atomic.AddInt64(&reqCount, 1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式", http.StatusInternalServerError)
			return
		}
		words := []string{"gRPC", "客户端", "通过", "HTTP/2", "实现", "高效", "远程调用。"}
		for i, word := range words {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			data, _ := json.Marshal(ChatResponse{
				ID: fmt.Sprintf("stream-%d", n), Done: i == len(words)-1,
				Message: Message{Role: "assistant", Content: word},
			})
			fmt.Fprintf(w, "%s\n", data)
			flusher.Flush()
			time.Sleep(30 * time.Millisecond)
		}
	})

	// 慢速端点（超时演示）
	mux.HandleFunc("/chat/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{ID: "slow", Message: Message{Content: "慢速响应"}, Done: true})
	})

	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	server := &http.Server{Handler: mux, WriteTimeout: 30 * time.Second}
	go server.Serve(listener)
	return addr, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}
}

// ============================================================
// 主函数
// ============================================================

func main() {
	fmt.Println("=== Day 11: gRPC 客户端开发 ===")
	fmt.Println()

	// 启动 Mock 服务器
	addr1, shutdown1 := startMockServer()
	addr2, shutdown2 := startMockServer()
	addr3, shutdown3 := startMockServer()
	defer shutdown1()
	defer shutdown2()
	defer shutdown3()
	time.Sleep(50 * time.Millisecond)

	// --- 1. 连接管理 ---
	fmt.Println("--- 1. 连接管理（Connection Management）---")
	fmt.Println("  gRPC 通过 Dial() 建立连接，底层维护 HTTP/2 连接池和自动重连")
	conn, _ := Dial(addr1)
	fmt.Printf("  当前状态: %s\n", conn.GetState())
	// 模拟断线重连
	conn.mu.Lock()
	conn.state = StateDown
	conn.mu.Unlock()
	fmt.Printf("  模拟断开 → %s\n", conn.GetState())
	conn.reconnect()
	fmt.Println()

	// --- 2. 一元 RPC ---
	fmt.Println("--- 2. 一元调用（Unary RPC）---")
	fmt.Println("  client.Chat(ctx, req) → 一个请求，一个响应")
	client := NewChatServiceClient(conn)
	req := &ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "什么是 gRPC？"}},
	}
	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		fmt.Printf("  ❌ 失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ [%s] %s\n", resp.ID, resp.Message.Content)
	}
	fmt.Println()

	// --- 3. 服务端流式 ---
	fmt.Println("--- 3. 服务端流式（Server Streaming RPC）---")
	fmt.Println("  stream.Recv() 循环读取，io.EOF 表示流结束")
	stream, err := client.ChatStream(context.Background(),
		&ChatRequest{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "介绍 gRPC"}}, Stream: true})
	if err != nil {
		fmt.Printf("  ❌ 失败: %v\n", err)
	} else {
		fmt.Print("  流式响应: ")
		n := 0
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Printf("\n  错误: %v\n", err)
				break
			}
			fmt.Print(chunk.Message.Content)
			n++
			if chunk.Done {
				fmt.Printf(" [完成, %d块]\n", n)
			}
		}
		stream.Close()
	}
	fmt.Println()

	// --- 4. 超时控制 ---
	fmt.Println("--- 4. 超时与截止时间（Timeout & Deadline）---")
	fmt.Println("  context.WithTimeout 控制 RPC 生命周期")
	// 充足超时
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	resp, err = client.Chat(ctx, req)
	cancel()
	if err != nil {
		fmt.Printf("  [3s超时] ❌ %v\n", err)
	} else {
		fmt.Printf("  [3s超时] ✅ %s\n", resp.Message.Content)
	}
	// 超时触发
	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	slowReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		addr1+"/chat/slow", strings.NewReader("{}"))
	slowReq.Header.Set("Content-Type", "application/json")
	_, err = conn.httpClient.Do(slowReq)
	cancel()
	if err != nil {
		fmt.Printf("  [200ms超时] ⏱️ 超时触发: %v\n", err)
	}
	fmt.Println()

	// --- 5. 重试策略 ---
	fmt.Println("--- 5. 重试策略（Exponential Backoff）---")
	fmt.Println("  指数退避 + 随机抖动，避免雷群效应")
	cfg := DefaultRetryConfig()

	// 正常（无需重试）
	fmt.Println("  [5a] 正常请求:")
	resp, err = callWithRetry(client, req, cfg)
	if err != nil {
		fmt.Printf("    ❌ %v\n", err)
	} else {
		fmt.Printf("    ✅ %s\n", resp.Message.Content)
	}

	// 模拟故障后恢复
	fmt.Println("  [5b] 故障 → 200ms后恢复:")
	conn.mu.Lock()
	conn.state = StateDown
	conn.mu.Unlock()
	go func() {
		time.Sleep(200 * time.Millisecond)
		conn.mu.Lock()
		conn.state = StateReady
		conn.mu.Unlock()
		fmt.Println("    🔧 连接已恢复")
	}()
	resp, err = callWithRetry(client, req, cfg)
	if err != nil {
		fmt.Printf("    ❌ %v\n", err)
	} else {
		fmt.Printf("    ✅ 恢复后: %s\n", resp.Message.Content)
	}

	// 退避序列
	fmt.Println("  退避公式: wait = min(initial × multiplier^n, max) ± jitter")
	for i := 0; i < cfg.MaxAttempts-1; i++ {
		b := float64(cfg.InitialBackoff) * math.Pow(cfg.BackoffMultiplier, float64(i))
		if b > float64(cfg.MaxBackoff) {
			b = float64(cfg.MaxBackoff)
		}
		fmt.Printf("    重试 %d: %v\n", i+1, time.Duration(b))
	}
	fmt.Println()

	// --- 6. 负载均衡 ---
	fmt.Println("--- 6. 负载均衡（Round-Robin）---")
	fmt.Println("  请求在多个后端间轮询分发")
	bc, _ := NewBalancedClient([]string{addr1, addr2, addr3})
	for i := 0; i < 6; i++ {
		lbReq := &ChatRequest{
			Model:    "gpt-4",
			Messages: []Message{{Role: "user", Content: fmt.Sprintf("请求#%d", i+1)}},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resp, backend, err := bc.Chat(ctx, lbReq)
		cancel()
		if err != nil {
			fmt.Printf("  ❌ #%d → %s: %v\n", i+1, backend, err)
		} else {
			fmt.Printf("  ✅ #%d → %s → %s\n", i+1, backend, resp.Message.Content)
		}
	}
	fmt.Println()

	// --- 7. 并发调用 ---
	fmt.Println("--- 7. 并发调用 ---")
	fmt.Println("  HTTP/2 多路复用支持单连接并发 RPC")
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cReq := &ChatRequest{Model: "gpt-4", Messages: []Message{{Role: "user", Content: fmt.Sprintf("并发#%d", idx+1)}}}
			if resp, err := client.Chat(ctx, cReq); err == nil {
				fmt.Printf("  请求 #%d → %s\n", idx+1, resp.ID)
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("  ⏱️ 5个并发请求耗时: %v（串行 ~250ms+）\n", time.Since(start).Round(time.Millisecond))
	fmt.Println()

	// 清理
	conn.Close()
	fmt.Println()
	fmt.Println("=== 总结 ===")
	fmt.Println("1. 连接管理: Dial() 建立连接，HTTP/2 连接池 + 自动重连")
	fmt.Println("2. 一元 RPC: client.Method(ctx, req) → (resp, err)")
	fmt.Println("3. 服务端流式: stream.Recv() 循环，io.EOF 结束")
	fmt.Println("4. 超时控制: context.WithTimeout 控制 RPC 生命周期")
	fmt.Println("5. 重试策略: 指数退避 + 抖动，避免雷群效应")
	fmt.Println("6. 负载均衡: round_robin 轮询多后端")
	fmt.Println("7. 并发调用: 多路复用支持单连接并发多个 RPC")
}
