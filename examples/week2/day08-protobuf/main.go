// Day 08 - Protobuf 数据序列化概念
// 本示例演示 Protocol Buffers 所解决的数据序列化问题
// 由于环境限制，我们使用标准库模拟 Protobuf 的核心概念
//
// ============================================================
// 如果使用真正的 Protobuf，.proto 文件定义如下：
// ============================================================
//
//   syntax = "proto3";
//   package chatservice;
//   option go_package = "github.com/bingaigc/Gollm/pb/chat";
//
//   // 聊天请求消息
//   message ChatRequest {
//     string model = 1;           // 模型名称，如 "gpt-4"
//     repeated Message messages = 2; // 历史消息列表
//     float temperature = 3;      // 温度参数，控制随机性
//     int32 max_tokens = 4;       // 最大生成令牌数
//   }
//
//   // 单条消息
//   message Message {
//     string role = 1;            // 角色：system/user/assistant
//     string content = 2;         // 消息内容
//     int64 timestamp = 3;        // 时间戳（Unix 毫秒）
//   }
//
//   // 聊天响应消息
//   message ChatResponse {
//     string id = 1;              // 响应唯一 ID
//     Message message = 2;        // 生成的消息
//     Usage usage = 3;            // 令牌使用统计
//   }
//
//   // 令牌使用统计
//   message Usage {
//     int32 prompt_tokens = 1;    // 输入令牌数
//     int32 completion_tokens = 2;// 输出令牌数
//     int32 total_tokens = 3;     // 总令牌数
//   }
//
//   // 聊天服务定义
//   service ChatService {
//     rpc Chat(ChatRequest) returns (ChatResponse);
//     rpc ChatStream(ChatRequest) returns (stream ChatResponse);
//   }
//
// ============================================================
// Protobuf 相比 JSON 的优势：
// 1. 更小的序列化体积（字段用数字标签而非字符串键）
// 2. 更快的编解码速度（二进制格式，无需解析文本）
// 3. 强类型 + 代码生成，编译期捕获错误
// 4. 向前/向后兼容（新增字段不影响旧代码）
// 5. 跨语言支持（自动生成 Go/Python/Java 等代码）
// ============================================================

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// Go 结构体定义（模拟 Protobuf 生成的代码）
// ============================================================

// Message 表示一条聊天消息
// 在真正的 Protobuf 中，这由 protoc 编译器自动生成
// json tag 用于 JSON 序列化，protobuf tag 用于二进制序列化
type Message struct {
	Role      string `json:"role"`      // 角色：system/user/assistant
	Content   string `json:"content"`   // 消息内容
	Timestamp int64  `json:"timestamp"` // Unix 毫秒时间戳
}

// ChatRequest 聊天请求
// 对应 proto 文件中的 ChatRequest message
type ChatRequest struct {
	Model       string    `json:"model"`       // 模型名称
	Messages    []Message `json:"messages"`    // 消息历史
	Temperature float32   `json:"temperature"` // 温度参数
	MaxTokens   int32     `json:"max_tokens"`  // 最大令牌数
}

// Usage 令牌使用统计
type Usage struct {
	PromptTokens     int32 `json:"prompt_tokens"`     // 输入令牌数
	CompletionTokens int32 `json:"completion_tokens"` // 输出令牌数
	TotalTokens      int32 `json:"total_tokens"`      // 总令牌数
}

// ChatResponse 聊天响应
type ChatResponse struct {
	ID      string  `json:"id"`      // 响应 ID
	Message Message `json:"message"` // 生成的消息
	Usage   Usage   `json:"usage"`   // 令牌使用情况
}

// ============================================================
// JSON 编解码演示
// ============================================================

// encodeJSON 使用 JSON 序列化请求
// JSON 是文本格式，人类可读但体积较大
func encodeJSON(req *ChatRequest) ([]byte, error) {
	return json.Marshal(req)
}

// decodeJSON 从 JSON 反序列化请求
func decodeJSON(data []byte) (*ChatRequest, error) {
	var req ChatRequest
	err := json.Unmarshal(data, &req)
	return &req, err
}

// ============================================================
// 简化的二进制编码演示（模拟 Protobuf 的 wire format）
// ============================================================
//
// Protobuf 使用 Tag-Length-Value (TLV) 编码：
// - Tag = (字段编号 << 3) | 线缆类型
// - 线缆类型：0=Varint, 1=64位, 2=带长度分隔, 5=32位
// - Varint 编码用变长字节表示整数，小数字用更少字节
//
// 例如，字段编号1的字符串 "hello":
//   Tag: 0x0A (字段1, 线缆类型2)
//   Length: 0x05 (5字节)
//   Value: 68 65 6C 6C 6F ("hello" 的 UTF-8)

// encodeBinarySimple 演示简化的二进制编码
// 这不是真正的 Protobuf 编码，仅用于展示二进制编码比 JSON 更紧凑
func encodeBinarySimple(req *ChatRequest) ([]byte, error) {
	buf := new(bytes.Buffer)

	// 写入模型名称：先写长度，再写内容
	// Protobuf 中使用 Varint 编码长度，这里简化为 uint16
	modelBytes := []byte(req.Model)
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(modelBytes))); err != nil {
		return nil, fmt.Errorf("写入模型名称长度失败: %w", err)
	}
	buf.Write(modelBytes)

	// 写入消息数量
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(req.Messages))); err != nil {
		return nil, fmt.Errorf("写入消息数量失败: %w", err)
	}

	// 逐条写入消息
	for _, msg := range req.Messages {
		// 写入角色
		roleBytes := []byte(msg.Role)
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(roleBytes))); err != nil {
			return nil, fmt.Errorf("写入角色长度失败: %w", err)
		}
		buf.Write(roleBytes)

		// 写入内容
		contentBytes := []byte(msg.Content)
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(contentBytes))); err != nil {
			return nil, fmt.Errorf("写入内容长度失败: %w", err)
		}
		buf.Write(contentBytes)

		// 写入时间戳（固定 8 字节）
		if err := binary.Write(buf, binary.LittleEndian, msg.Timestamp); err != nil {
			return nil, fmt.Errorf("写入时间戳失败: %w", err)
		}
	}

	// 写入温度（固定 4 字节）
	if err := binary.Write(buf, binary.LittleEndian, req.Temperature); err != nil {
		return nil, fmt.Errorf("写入温度失败: %w", err)
	}

	// 写入最大令牌数（固定 4 字节）
	if err := binary.Write(buf, binary.LittleEndian, req.MaxTokens); err != nil {
		return nil, fmt.Errorf("写入最大令牌数失败: %w", err)
	}

	return buf.Bytes(), nil
}

// decodeBinarySimple 从简化的二进制格式反序列化
func decodeBinarySimple(data []byte) (*ChatRequest, error) {
	buf := bytes.NewReader(data)
	req := &ChatRequest{}

	// 读取模型名称
	var modelLen uint16
	if err := binary.Read(buf, binary.LittleEndian, &modelLen); err != nil {
		return nil, fmt.Errorf("读取模型名称长度失败: %w", err)
	}
	modelBytes := make([]byte, modelLen)
	if _, err := buf.Read(modelBytes); err != nil {
		return nil, fmt.Errorf("读取模型名称失败: %w", err)
	}
	req.Model = string(modelBytes)

	// 读取消息数量
	var msgCount uint16
	if err := binary.Read(buf, binary.LittleEndian, &msgCount); err != nil {
		return nil, fmt.Errorf("读取消息数量失败: %w", err)
	}

	req.Messages = make([]Message, msgCount)
	for i := range req.Messages {
		// 读取角色
		var roleLen uint16
		if err := binary.Read(buf, binary.LittleEndian, &roleLen); err != nil {
			return nil, fmt.Errorf("读取角色长度失败: %w", err)
		}
		roleBytes := make([]byte, roleLen)
		if _, err := buf.Read(roleBytes); err != nil {
			return nil, fmt.Errorf("读取角色失败: %w", err)
		}
		req.Messages[i].Role = string(roleBytes)

		// 读取内容
		var contentLen uint16
		if err := binary.Read(buf, binary.LittleEndian, &contentLen); err != nil {
			return nil, fmt.Errorf("读取内容长度失败: %w", err)
		}
		contentBytes := make([]byte, contentLen)
		if _, err := buf.Read(contentBytes); err != nil {
			return nil, fmt.Errorf("读取内容失败: %w", err)
		}
		req.Messages[i].Content = string(contentBytes)

		// 读取时间戳
		if err := binary.Read(buf, binary.LittleEndian, &req.Messages[i].Timestamp); err != nil {
			return nil, fmt.Errorf("读取时间戳失败: %w", err)
		}
	}

	// 读取温度
	if err := binary.Read(buf, binary.LittleEndian, &req.Temperature); err != nil {
		return nil, fmt.Errorf("读取温度失败: %w", err)
	}

	// 读取最大令牌数
	if err := binary.Read(buf, binary.LittleEndian, &req.MaxTokens); err != nil {
		return nil, fmt.Errorf("读取最大令牌数失败: %w", err)
	}

	return req, nil
}

// ============================================================
// 主函数：对比 JSON 和二进制编码
// ============================================================

func main() {
	fmt.Println("=== Day 08: Protobuf 数据序列化概念 ===")
	fmt.Println()

	// 构造测试数据
	now := time.Now().UnixMilli()
	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{
				Role:      "system",
				Content:   "你是一个有用的AI助手。",
				Timestamp: now - 60000, // 一分钟前
			},
			{
				Role:      "user",
				Content:   "请解释 Go 语言中的 goroutine 是什么？",
				Timestamp: now,
			},
		},
		Temperature: 0.7,
		MaxTokens:   2048,
	}

	// ----------------------------------------------------------
	// 1. JSON 编码
	// ----------------------------------------------------------
	fmt.Println("--- JSON 编码 ---")

	jsonData, err := encodeJSON(req)
	if err != nil {
		fmt.Printf("JSON 编码失败: %v\n", err)
		return
	}
	fmt.Printf("JSON 大小: %d 字节\n", len(jsonData))

	// 格式化输出 JSON（便于阅读）
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, jsonData, "", "  "); err == nil {
		fmt.Printf("JSON 内容:\n%s\n", prettyJSON.String())
	}

	// 验证 JSON 反序列化
	decoded, err := decodeJSON(jsonData)
	if err != nil {
		fmt.Printf("JSON 解码失败: %v\n", err)
		return
	}
	fmt.Printf("JSON 解码验证 - 模型: %s, 消息数: %d\n", decoded.Model, len(decoded.Messages))
	fmt.Println()

	// ----------------------------------------------------------
	// 2. 简化的二进制编码
	// ----------------------------------------------------------
	fmt.Println("--- 二进制编码（模拟 Protobuf）---")

	binaryData, err := encodeBinarySimple(req)
	if err != nil {
		fmt.Printf("二进制编码失败: %v\n", err)
		return
	}
	fmt.Printf("二进制大小: %d 字节\n", len(binaryData))

	// 打印二进制数据的十六进制表示（前 64 字节）
	fmt.Printf("二进制内容（十六进制）: ")
	displayLen := len(binaryData)
	if displayLen > 64 {
		displayLen = 64
	}
	for i := 0; i < displayLen; i++ {
		fmt.Printf("%02x ", binaryData[i])
	}
	if len(binaryData) > 64 {
		fmt.Printf("...")
	}
	fmt.Println()

	// 验证二进制反序列化
	binaryDecoded, err := decodeBinarySimple(binaryData)
	if err != nil {
		fmt.Printf("二进制解码失败: %v\n", err)
		return
	}
	fmt.Printf("二进制解码验证 - 模型: %s, 消息数: %d\n",
		binaryDecoded.Model, len(binaryDecoded.Messages))
	fmt.Println()

	// ----------------------------------------------------------
	// 3. 大小对比
	// ----------------------------------------------------------
	fmt.Println("--- 序列化大小对比 ---")
	fmt.Printf("JSON 大小:   %d 字节\n", len(jsonData))
	fmt.Printf("二进制大小:  %d 字节\n", len(binaryData))

	savings := float64(len(jsonData)-len(binaryData)) / float64(len(jsonData)) * 100
	fmt.Printf("节省空间:    %.1f%%\n", savings)
	fmt.Println()

	// ----------------------------------------------------------
	// 4. 性能对比（简单计时）
	// ----------------------------------------------------------
	fmt.Println("--- 编解码性能对比 ---")

	const iterations = 10000

	// JSON 编码性能
	startJSON := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = encodeJSON(req)
	}
	jsonEncDuration := time.Since(startJSON)

	// JSON 解码性能
	startJSON = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = decodeJSON(jsonData)
	}
	jsonDecDuration := time.Since(startJSON)

	// 二进制编码性能
	startBin := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = encodeBinarySimple(req)
	}
	binEncDuration := time.Since(startBin)

	// 二进制解码性能
	startBin = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = decodeBinarySimple(binaryData)
	}
	binDecDuration := time.Since(startBin)

	fmt.Printf("JSON   编码: %v (%d 次)\n", jsonEncDuration, iterations)
	fmt.Printf("二进制 编码: %v (%d 次)\n", binEncDuration, iterations)
	fmt.Printf("JSON   解码: %v (%d 次)\n", jsonDecDuration, iterations)
	fmt.Printf("二进制 解码: %v (%d 次)\n", binDecDuration, iterations)
	fmt.Println()

	// ----------------------------------------------------------
	// 5. 响应体演示
	// ----------------------------------------------------------
	fmt.Println("--- ChatResponse 演示 ---")

	resp := &ChatResponse{
		ID: "chatcmpl-abc123",
		Message: Message{
			Role:      "assistant",
			Content:   "Goroutine 是 Go 语言中的轻量级线程，由 Go 运行时管理。",
			Timestamp: time.Now().UnixMilli(),
		},
		Usage: Usage{
			PromptTokens:     45,
			CompletionTokens: 28,
			TotalTokens:      73,
		},
	}

	respJSON, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("响应 JSON:\n%s\n", string(respJSON))
	fmt.Println()

	// ----------------------------------------------------------
	// 总结
	// ----------------------------------------------------------
	fmt.Println("=== 总结 ===")
	fmt.Println("1. Protobuf 使用二进制编码，体积比 JSON 小 30%-80%")
	fmt.Println("2. Protobuf 编解码速度通常比 JSON 快 2-10 倍")
	fmt.Println("3. Protobuf 提供强类型和代码生成，减少手写序列化代码")
	fmt.Println("4. Protobuf 支持字段编号，实现向前/向后兼容")
	fmt.Println("5. 在 gRPC 中，Protobuf 是默认的序列化格式")
	fmt.Println()
	fmt.Println("实际项目中使用 Protobuf 的步骤：")
	fmt.Println("  1. 编写 .proto 文件定义消息和服务")
	fmt.Println("  2. 使用 protoc 编译器生成 Go 代码")
	fmt.Println("  3. 导入生成的包，直接使用类型安全的结构体")
	fmt.Println("  4. 配合 gRPC 框架实现高性能 RPC 通信")
}
