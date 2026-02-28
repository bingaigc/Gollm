// Day 19 - MCP (Model Context Protocol) 工具服务器
//
// 本示例演示如何实现一个 MCP 工具服务器：
// - 定义 MCP 协议核心类型（基于 JSON-RPC 2.0）
// - 实现工具注册中心（ToolRegistry）
// - 提供 "天气查询" 和 "计算器" 两个示例工具
// - 处理 tools/list 和 tools/call 两个核心端点
// - 完整的错误处理和协议合规性
//
// 🔑 MCP 协议简介：
// Model Context Protocol 是 Anthropic 提出的开放协议，旨在标准化
// LLM 与外部工具/数据源的交互方式。MCP 使用 JSON-RPC 2.0 作为
// 传输格式，定义了工具发现（tools/list）和工具调用（tools/call）
// 两个核心操作，让 AI 模型能够安全地调用外部能力。
//
// MCP 架构：
//   LLM (客户端) ←→ MCP Server (工具提供方)
//   1. 客户端通过 tools/list 发现可用工具
//   2. LLM 决定调用哪个工具并生成参数
//   3. 客户端通过 tools/call 执行工具
//   4. 工具结果返回给 LLM 作为上下文

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
)

// ============================================================
// MCP 协议类型定义 —— 基于 JSON-RPC 2.0
// ============================================================

// JSONRPCRequest 是 JSON-RPC 2.0 请求格式
// MCP 使用 JSON-RPC 作为通信协议，每个请求包含方法名和参数
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`          // 协议版本，固定为 "2.0"
	ID      interface{}     `json:"id"`               // 请求 ID，用于匹配响应
	Method  string          `json:"method"`           // 方法名，如 "tools/list"
	Params  json.RawMessage `json:"params,omitempty"` // 方法参数（延迟解析）
}

// JSONRPCResponse 是 JSON-RPC 2.0 响应格式
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"` // 错误信息（成功时为 nil）
}

// JSONRPCError 是 JSON-RPC 2.0 错误对象
// 标准错误码：-32700 解析错误, -32600 无效请求, -32601 方法未找到, -32602 无效参数
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ============================================================
// MCP 工具定义
// ============================================================

// ToolProperty 描述工具参数的一个属性
type ToolProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolInputSchema 描述工具的输入参数 schema（基于 JSON Schema）
// MCP 使用 JSON Schema 描述工具参数，让 LLM 能理解如何调用
type ToolInputSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string                `json:"required,omitempty"`
}

// Tool 是 MCP 工具的元数据定义
// 包含名称、描述和输入参数格式，LLM 通过此信息决定如何调用
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolInputSchema `json:"inputSchema"`
}

// ToolCallParams 是 tools/call 请求的参数
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult 是工具执行的返回结果
// IsError 标识工具执行是否出错
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// ToolResultContent 是工具返回内容的单元
type ToolResultContent struct {
	Type string `json:"type"` // "text" 或 "image" 等
	Text string `json:"text"`
}

// ListToolsResult 是 tools/list 的响应
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// ============================================================
// 工具处理函数类型和注册中心
// ============================================================

// ToolHandler 是工具执行函数的类型签名
// 接收参数映射，返回结果或错误
type ToolHandler func(args map[string]interface{}) (*ToolResult, error)

// ToolRegistry 管理所有注册的工具
// 提供工具注册、列表查询和调用分发功能
type ToolRegistry struct {
	tools    map[string]Tool        // 工具元数据（名称 → 定义）
	handlers map[string]ToolHandler // 工具处理函数（名称 → 函数）
	order    []string               // 注册顺序（保证列表顺序稳定）
}

// NewToolRegistry 创建一个空的工具注册中心
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:    make(map[string]Tool),
		handlers: make(map[string]ToolHandler),
	}
}

// Register 注册一个新工具及其处理函数
func (r *ToolRegistry) Register(tool Tool, handler ToolHandler) {
	r.tools[tool.Name] = tool
	r.handlers[tool.Name] = handler
	r.order = append(r.order, tool.Name)
}

// ListTools 返回所有已注册工具的列表
func (r *ToolRegistry) ListTools() []Tool {
	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		tools = append(tools, r.tools[name])
	}
	return tools
}

// CallTool 根据名称调用工具
func (r *ToolRegistry) CallTool(name string, args map[string]interface{}) (*ToolResult, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return &ToolResult{
			Content: []ToolResultContent{{Type: "text", Text: fmt.Sprintf("未知工具: %s", name)}},
			IsError: true,
		}, fmt.Errorf("工具 %q 未注册", name)
	}
	return handler(args)
}

// ============================================================
// 示例工具实现
// ============================================================

// registerWeatherTool 注册天气查询工具
// 返回模拟天气数据，演示工具如何接收参数并返回结构化结果
func registerWeatherTool(registry *ToolRegistry) {
	tool := Tool{
		Name:        "get_weather",
		Description: "查询指定城市的当前天气信息，返回温度、天气状况和湿度",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]ToolProperty{
				"city": {
					Type:        "string",
					Description: "要查询天气的城市名称，例如：北京、上海",
				},
			},
			Required: []string{"city"},
		},
	}

	handler := func(args map[string]interface{}) (*ToolResult, error) {
		city, ok := args["city"].(string)
		if !ok || city == "" {
			return &ToolResult{
				Content: []ToolResultContent{{Type: "text", Text: "缺少必要参数: city"}},
				IsError: true,
			}, nil
		}

		// 模拟天气数据（真实场景中会调用天气 API）
		weatherData := map[string]map[string]interface{}{
			"北京": {"temp": 22, "condition": "晴", "humidity": 45},
			"上海": {"temp": 26, "condition": "多云", "humidity": 72},
			"深圳": {"temp": 30, "condition": "雷阵雨", "humidity": 85},
			"成都": {"temp": 20, "condition": "阴", "humidity": 68},
		}

		data, exists := weatherData[city]
		if !exists {
			return &ToolResult{
				Content: []ToolResultContent{{
					Type: "text",
					Text: fmt.Sprintf("暂无 %s 的天气数据，支持城市：北京、上海、深圳、成都", city),
				}},
			}, nil
		}

		result := fmt.Sprintf("%s 天气：%s，温度 %v°C，湿度 %v%%",
			city, data["condition"], data["temp"], data["humidity"])

		return &ToolResult{
			Content: []ToolResultContent{{Type: "text", Text: result}},
		}, nil
	}

	registry.Register(tool, handler)
}

// registerCalculatorTool 注册计算器工具
// 支持四则运算和常用数学函数，演示参数验证和错误处理
func registerCalculatorTool(registry *ToolRegistry) {
	tool := Tool{
		Name:        "calculator",
		Description: "执行数学计算，支持加减乘除和常用函数（sqrt, pow）",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]ToolProperty{
				"expression": {
					Type:        "string",
					Description: "数学表达式，格式：操作符 操作数1 操作数2，例如：add 3 5, sqrt 16, pow 2 10",
				},
			},
			Required: []string{"expression"},
		},
	}

	handler := func(args map[string]interface{}) (*ToolResult, error) {
		expr, ok := args["expression"].(string)
		if !ok || expr == "" {
			return &ToolResult{
				Content: []ToolResultContent{{Type: "text", Text: "缺少必要参数: expression"}},
				IsError: true,
			}, nil
		}

		result, err := evaluateExpression(expr)
		if err != nil {
			return &ToolResult{
				Content: []ToolResultContent{{Type: "text", Text: fmt.Sprintf("计算错误: %v", err)}},
				IsError: true,
			}, nil
		}

		return &ToolResult{
			Content: []ToolResultContent{{
				Type: "text",
				Text: fmt.Sprintf("计算结果: %s = %s", expr, formatNumber(result)),
			}},
		}, nil
	}

	registry.Register(tool, handler)
}

// evaluateExpression 解析并计算简单数学表达式
func evaluateExpression(expr string) (float64, error) {
	parts := strings.Fields(expr)
	if len(parts) < 2 {
		return 0, fmt.Errorf("表达式格式错误，需要：操作符 操作数1 [操作数2]")
	}

	op := strings.ToLower(parts[0])

	// 单操作数函数
	if op == "sqrt" {
		a, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, fmt.Errorf("无效数字: %s", parts[1])
		}
		if a < 0 {
			return 0, fmt.Errorf("不能对负数开平方根")
		}
		return math.Sqrt(a), nil
	}

	// 双操作数运算
	if len(parts) < 3 {
		return 0, fmt.Errorf("双操作数运算需要：操作符 操作数1 操作数2")
	}

	a, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, fmt.Errorf("无效数字: %s", parts[1])
	}
	b, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, fmt.Errorf("无效数字: %s", parts[2])
	}

	switch op {
	case "add":
		return a + b, nil
	case "sub":
		return a - b, nil
	case "mul":
		return a * b, nil
	case "div":
		if b == 0 {
			return 0, fmt.Errorf("除数不能为零")
		}
		return a / b, nil
	case "pow":
		return math.Pow(a, b), nil
	default:
		return 0, fmt.Errorf("不支持的操作符: %s（支持: add, sub, mul, div, pow, sqrt）", op)
	}
}

// formatNumber 格式化数字输出，整数不显示小数点
func formatNumber(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%.4f", f)
}

// ============================================================
// MCP HTTP 服务器
// ============================================================

// MCPServer 封装 MCP 协议的 HTTP 处理逻辑
type MCPServer struct {
	registry *ToolRegistry
}

// NewMCPServer 创建 MCP 服务器实例
func NewMCPServer(registry *ToolRegistry) *MCPServer {
	return &MCPServer{registry: registry}
}

// ServeHTTP 处理所有 MCP JSON-RPC 请求
// MCP 使用单一端点接收所有请求，通过 method 字段区分操作
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONRPCError(w, nil, -32600, "仅支持 POST 方法")
		return
	}

	// 解析 JSON-RPC 请求
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "JSON 解析错误: "+err.Error())
		return
	}

	// 验证 JSON-RPC 版本
	if req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, -32600, "仅支持 JSON-RPC 2.0")
		return
	}

	// 根据方法名分发请求
	switch req.Method {
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(w, req)
	default:
		writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("未知方法: %s", req.Method))
	}
}

// handleToolsList 处理工具发现请求
// 返回所有已注册工具的元数据，LLM 据此决定可以调用哪些工具
func (s *MCPServer) handleToolsList(w http.ResponseWriter, req JSONRPCRequest) {
	result := ListToolsResult{
		Tools: s.registry.ListTools(),
	}
	writeJSONRPCResult(w, req.ID, result)
}

// handleToolsCall 处理工具调用请求
// 解析参数 → 查找工具 → 执行 → 返回结果
func (s *MCPServer) handleToolsCall(w http.ResponseWriter, req JSONRPCRequest) {
	// 解析调用参数
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "无效的工具调用参数: "+err.Error())
		return
	}

	// 执行工具
	result, err := s.registry.CallTool(params.Name, params.Arguments)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32602, err.Error())
		return
	}

	writeJSONRPCResult(w, req.ID, result)
}

// writeJSONRPCResult 写入成功响应
func writeJSONRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError 写入错误响应
func writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// 客户端辅助函数 —— 模拟 LLM 与 MCP 服务器的交互
// ============================================================

// mcpCall 向 MCP 服务器发送 JSON-RPC 请求
func mcpCall(serverURL string, method string, params interface{}, id interface{}) (*JSONRPCResponse, error) {
	paramsJSON, _ := json.Marshal(params)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(serverURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &rpcResp, nil
}

func main() {
	fmt.Println("=== Day 19: MCP 工具服务器 ===")
	fmt.Println()

	// MCP 协议说明
	fmt.Println("📖 MCP (Model Context Protocol) 协议要点：")
	fmt.Println("  • 基于 JSON-RPC 2.0 传输协议")
	fmt.Println("  • tools/list —— 工具发现：返回可用工具及参数格式")
	fmt.Println("  • tools/call —— 工具调用：执行指定工具并返回结果")
	fmt.Println("  • 工具参数使用 JSON Schema 描述，便于 LLM 理解")
	fmt.Println()

	// ---- 1. 注册工具 ----
	registry := NewToolRegistry()
	registerWeatherTool(registry)
	registerCalculatorTool(registry)

	fmt.Printf("📦 已注册 %d 个工具\n\n", len(registry.tools))

	// ---- 2. 启动 MCP 服务器 ----
	mcpServer := NewMCPServer(registry)
	server := httptest.NewServer(mcpServer)
	defer server.Close()
	fmt.Printf("🚀 MCP 服务器已启动: %s\n\n", server.URL)

	// ---- 3. 工具发现（tools/list）----
	fmt.Println("--- 步骤 1: 工具发现 (tools/list) ---")
	resp, err := mcpCall(server.URL, "tools/list", nil, 1)
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		return
	}

	// 格式化输出工具列表
	resultJSON, _ := json.MarshalIndent(resp.Result, "  ", "  ")
	fmt.Printf("  响应:\n  %s\n\n", resultJSON)

	// ---- 4. 工具调用演示 ----
	fmt.Println("--- 步骤 2: 工具调用 (tools/call) ---")

	// 4a. 调用天气查询工具
	fmt.Println("\n  🌤️  调用 get_weather（查询北京天气）：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "get_weather",
		Arguments: map[string]interface{}{"city": "北京"},
	}, 2)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// 4b. 查询不支持的城市
	fmt.Println("\n  🌤️  调用 get_weather（查询纽约天气 —— 不支持）：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "get_weather",
		Arguments: map[string]interface{}{"city": "纽约"},
	}, 3)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// 4c. 调用计算器工具 —— 基础运算
	fmt.Println("\n  🔢 调用 calculator（计算 add 42 58）：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "calculator",
		Arguments: map[string]interface{}{"expression": "add 42 58"},
	}, 4)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// 4d. 调用计算器工具 —— 平方根
	fmt.Println("\n  🔢 调用 calculator（计算 sqrt 144）：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "calculator",
		Arguments: map[string]interface{}{"expression": "sqrt 144"},
	}, 5)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// 4e. 调用计算器工具 —— 幂运算
	fmt.Println("\n  🔢 调用 calculator（计算 pow 2 10）：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "calculator",
		Arguments: map[string]interface{}{"expression": "pow 2 10"},
	}, 6)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// ---- 5. 错误处理演示 ----
	fmt.Println("\n--- 步骤 3: 错误处理演示 ---")

	// 5a. 调用不存在的工具
	fmt.Println("\n  ⚠️  调用不存在的工具：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "nonexistent_tool",
		Arguments: map[string]interface{}{},
	}, 7)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else if resp.Error != nil {
		fmt.Printf("  JSON-RPC 错误 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
	}

	// 5b. 调用不存在的方法
	fmt.Println("\n  ⚠️  调用未知 RPC 方法：")
	resp, err = mcpCall(server.URL, "unknown/method", nil, 8)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else if resp.Error != nil {
		fmt.Printf("  JSON-RPC 错误 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
	}

	// 5c. 计算器除零错误
	fmt.Println("\n  ⚠️  计算器除零：")
	resp, err = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "calculator",
		Arguments: map[string]interface{}{"expression": "div 10 0"},
	}, 9)
	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
	} else {
		printToolResult(resp)
	}

	// ---- 6. 完整工作流演示 ----
	fmt.Println("\n--- 步骤 4: 完整 MCP 工作流 ---")
	fmt.Println("  模拟 LLM 通过 MCP 调用工具的完整流程：")
	fmt.Println()
	fmt.Println("  1️⃣  LLM 收到用户问题：\"北京天气怎么样？\"")
	fmt.Println("  2️⃣  LLM 调用 tools/list 发现可用工具")
	fmt.Println("  3️⃣  LLM 决定调用 get_weather 工具")
	fmt.Println("  4️⃣  MCP 服务器执行工具并返回结果")
	fmt.Println("  5️⃣  LLM 根据工具结果生成最终回复")
	fmt.Println()

	// 执行工作流
	resp, _ = mcpCall(server.URL, "tools/call", ToolCallParams{
		Name:      "get_weather",
		Arguments: map[string]interface{}{"city": "北京"},
	}, 10)
	if resp != nil && resp.Error == nil {
		resultBytes, _ := json.Marshal(resp.Result)
		fmt.Printf("  📍 工具返回: %s\n", resultBytes)
		fmt.Println("  🤖 LLM 生成回复：根据查询结果，北京今天天气晴朗，气温22°C，适合外出活动。")
	}

	fmt.Println("\n🎓 学习要点：")
	fmt.Println("  1. MCP 通过 JSON-RPC 2.0 标准化了 LLM 与工具的通信")
	fmt.Println("  2. tools/list 让 LLM 动态发现可用工具和参数格式")
	fmt.Println("  3. tools/call 执行工具调用，结果作为上下文返回给 LLM")
	fmt.Println("  4. ToolRegistry 模式便于管理和扩展工具集合")
}

// printToolResult 格式化输出工具调用结果
func printToolResult(resp *JSONRPCResponse) {
	if resp.Error != nil {
		fmt.Printf("  JSON-RPC 错误 [%d]: %s\n", resp.Error.Code, resp.Error.Message)
		return
	}

	// 将 result 转换回 ToolResult 结构
	resultJSON, _ := json.Marshal(resp.Result)
	var result ToolResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		fmt.Printf("  原始结果: %s\n", resultJSON)
		return
	}

	if result.IsError {
		fmt.Print("  ⚠️  ")
	} else {
		fmt.Print("  ✅ ")
	}

	for _, c := range result.Content {
		fmt.Println(c.Text)
	}
}
