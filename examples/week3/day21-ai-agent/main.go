// Day 21 - AI Agent：ReAct 模式实现
//
// 本示例演示如何构建一个基于 ReAct 模式的 AI Agent：
// - ReAct = Reasoning（推理）+ Acting（行动），交替进行
// - Agent 通过 Think → Act → Observe 循环解决复杂问题
// - 集成工具注册中心，支持动态调用多个工具
// - 对话历史管理（记忆机制）
// - 最大迭代次数限制，防止无限循环
// - 完整的推理链追踪和展示
//
// 🔑 ReAct 模式核心思想：
// 传统 LLM 只能一次性给出回答，而 ReAct Agent 能够：
// 1. Thought（思考）：分析当前状态，决定下一步行动
// 2. Action（行动）：调用外部工具获取信息
// 3. Observation（观察）：处理工具返回结果
// 4. 重复以上循环直到获得足够信息，给出最终回答
//
// 这种模式让 AI 能够处理需要多步推理和外部信息的复杂问题。

package main

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================
// Agent 步骤追踪 —— 记录每一步推理过程
// ============================================================

// StepType 标识 Agent 步骤的类型
type StepType string

const (
	StepThought     StepType = "Thought"     // 思考步骤
	StepAction      StepType = "Action"      // 行动步骤
	StepObservation StepType = "Observation" // 观察步骤
	StepFinalAnswer StepType = "Answer"      // 最终回答
)

// AgentStep 记录 Agent 推理过程中的单个步骤
type AgentStep struct {
	Type      StepType  // 步骤类型
	Content   string    // 步骤内容
	ToolName  string    // 调用的工具名（仅 Action 步骤有值）
	ToolInput string    // 工具输入参数（仅 Action 步骤有值）
	Timestamp time.Time // 时间戳
}

// ============================================================
// 工具系统 —— Agent 可调用的外部能力
// ============================================================

// AgentTool 定义 Agent 可用的工具
type AgentTool struct {
	Name        string                       // 工具名称
	Description string                       // 工具描述（供 LLM "理解" 用途）
	Execute     func(input string) string    // 工具执行函数
}

// AgentToolRegistry 管理 Agent 可用的工具集合
type AgentToolRegistry struct {
	tools map[string]AgentTool
	order []string
}

// NewAgentToolRegistry 创建工具注册中心
func NewAgentToolRegistry() *AgentToolRegistry {
	return &AgentToolRegistry{
		tools: make(map[string]AgentTool),
	}
}

// Register 注册一个工具
func (r *AgentToolRegistry) Register(tool AgentTool) {
	r.tools[tool.Name] = tool
	r.order = append(r.order, tool.Name)
}

// Get 获取工具
func (r *AgentToolRegistry) Get(name string) (AgentTool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// ListDescriptions 返回所有工具的描述信息
// 这些描述会包含在系统提示中，让 LLM 知道有哪些工具可用
func (r *AgentToolRegistry) ListDescriptions() string {
	var sb strings.Builder
	for _, name := range r.order {
		tool := r.tools[name]
		sb.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description))
	}
	return sb.String()
}

// ============================================================
// 模拟 LLM —— 用规则引擎代替真实 LLM 调用
// ============================================================

// MockLLM 模拟 LLM 的推理行为
// 真实场景中，这里会调用 OpenAI/DeepSeek 等 API
// 为了演示 ReAct 模式的完整流程，使用预定义的推理规则
type MockLLM struct {
	callCount int // 记录调用次数，用于驱动多步推理
}

// LLMResponse 表示 LLM 的一次输出
type LLMResponse struct {
	Thought     string // 模型的思考过程
	Action      string // 要调用的工具名（空表示不调用工具）
	ActionInput string // 工具的输入参数
	FinalAnswer string // 最终回答（非空表示推理结束）
}

// Generate 模拟 LLM 根据对话历史生成下一步推理
// 在真实实现中，这里会将完整对话历史发送给 LLM API
func (m *MockLLM) Generate(history []string, query string) LLMResponse {
	m.callCount++

	// 根据查询内容和调用次数模拟多步推理
	// 注意：复合问题的匹配优先于单一问题，避免被提前截获
	switch {
	// 复合问题 —— 需要多次工具调用（优先匹配，因为也包含"天气"关键词）
	case containsAny(query, "对比", "比较", "两个城市"):
		return m.handleCompareQuery(history, query)

	// 天气相关问题 —— 需要调用天气工具
	case containsAny(query, "天气", "weather", "温度"):
		return m.handleWeatherQuery(history, query)

	// 计算相关问题 —— 需要调用计算器工具
	case containsAny(query, "计算", "多少", "加", "乘"):
		return m.handleCalcQuery(history, query)

	// 无需工具的简单问题
	default:
		return LLMResponse{
			Thought:     "这个问题我可以直接回答，不需要调用任何工具。",
			FinalAnswer: "你好！我是一个 AI Agent，具备工具调用能力。我可以查询天气、执行计算等。有什么需要帮助的吗？",
		}
	}
}

// handleWeatherQuery 处理天气查询的多步推理
func (m *MockLLM) handleWeatherQuery(history []string, query string) LLMResponse {
	// 检查历史中是否已有天气数据（工具返回的结果包含温度标记）
	hasWeatherData := false
	for _, h := range history {
		if strings.Contains(h, "°C") {
			hasWeatherData = true
			break
		}
	}

	if !hasWeatherData {
		// 第一步：决定调用天气工具
		city := extractCity(query)
		return LLMResponse{
			Thought:     fmt.Sprintf("用户想知道%s的天气情况，我需要调用天气查询工具获取实时数据。", city),
			Action:      "weather",
			ActionInput: city,
		}
	}

	// 第二步：已有数据，生成最终回答
	weatherInfo := ""
	for _, h := range history {
		if strings.Contains(h, "°C") {
			weatherInfo = h
			break
		}
	}
	return LLMResponse{
		Thought:     "我已经获取到天气数据，现在可以整理成友好的回复。",
		FinalAnswer: fmt.Sprintf("根据查询结果，%s 建议您根据天气情况合理安排出行。", weatherInfo),
	}
}

// handleCalcQuery 处理计算问题
func (m *MockLLM) handleCalcQuery(history []string, query string) LLMResponse {
	hasResult := false
	for _, h := range history {
		if strings.Contains(h, "计算结果") {
			hasResult = true
			break
		}
	}

	if !hasResult {
		expr := extractExpression(query)
		return LLMResponse{
			Thought:     "用户需要进行数学计算，我应该使用计算器工具来确保准确性。",
			Action:      "calculator",
			ActionInput: expr,
		}
	}

	calcResult := ""
	for _, h := range history {
		if strings.Contains(h, "计算结果") {
			calcResult = h
			break
		}
	}
	return LLMResponse{
		Thought:     "计算已完成，我可以给出最终答案了。",
		FinalAnswer: fmt.Sprintf("经过计算，%s", calcResult),
	}
}

// handleCompareQuery 处理需要多次工具调用的复合问题
func (m *MockLLM) handleCompareQuery(history []string, query string) LLMResponse {
	// 统计已获取的城市数量
	cityCount := 0
	for _, h := range history {
		if strings.Contains(h, "°C") {
			cityCount++
		}
	}

	switch cityCount {
	case 0:
		return LLMResponse{
			Thought:     "用户想对比两个城市的天气，我需要分别查询。先查第一个城市：北京。",
			Action:      "weather",
			ActionInput: "北京",
		}
	case 1:
		return LLMResponse{
			Thought:     "已获取北京的天气数据，现在查询第二个城市：上海。",
			Action:      "weather",
			ActionInput: "上海",
		}
	default:
		// 收集所有天气信息
		var infos []string
		for _, h := range history {
			if strings.Contains(h, "°C") {
				infos = append(infos, h)
			}
		}
		return LLMResponse{
			Thought:     "两个城市的天气数据都已获取，可以进行对比分析了。",
			FinalAnswer: fmt.Sprintf("对比结果：%s。综合来看，请根据各地天气合理安排行程。", strings.Join(infos, "；")),
		}
	}
}

// ============================================================
// AI Agent 核心实现
// ============================================================

// Agent 是基于 ReAct 模式的 AI Agent
type Agent struct {
	llm       *MockLLM           // LLM 推理引擎
	tools     *AgentToolRegistry // 可用工具集合
	history   []string           // 对话/推理历史（Agent 的记忆）
	steps     []AgentStep        // 完整推理步骤链
	maxIter   int                // 最大迭代次数（防止无限循环）
}

// NewAgent 创建一个新的 AI Agent
func NewAgent(tools *AgentToolRegistry, maxIter int) *Agent {
	return &Agent{
		llm:     &MockLLM{},
		tools:   tools,
		maxIter: maxIter,
	}
}

// Run 执行 Agent 的 ReAct 循环
// 核心流程：Think → Act → Observe → 重复，直到得出最终答案或达到迭代上限
func (a *Agent) Run(query string) string {
	// 重置状态
	a.history = []string{}
	a.steps = []AgentStep{}

	fmt.Printf("👤 用户提问: %s\n\n", query)
	a.history = append(a.history, fmt.Sprintf("用户问题: %s", query))

	// ReAct 循环
	for i := 0; i < a.maxIter; i++ {
		fmt.Printf("--- 🔄 迭代 %d/%d ---\n", i+1, a.maxIter)

		// 1. 调用 LLM 进行推理
		response := a.llm.Generate(a.history, query)

		// 2. 记录思考步骤
		if response.Thought != "" {
			a.addStep(StepThought, response.Thought, "", "")
			fmt.Printf("  💭 思考: %s\n", response.Thought)
		}

		// 3. 如果有最终回答，结束循环
		if response.FinalAnswer != "" {
			a.addStep(StepFinalAnswer, response.FinalAnswer, "", "")
			fmt.Printf("  ✅ 最终回答: %s\n\n", response.FinalAnswer)
			return response.FinalAnswer
		}

		// 4. 执行工具调用（Action 步骤）
		if response.Action != "" {
			a.addStep(StepAction, "", response.Action, response.ActionInput)
			fmt.Printf("  🔧 调用工具: %s（输入: %s）\n", response.Action, response.ActionInput)

			// 查找并执行工具
			tool, exists := a.tools.Get(response.Action)
			if !exists {
				observation := fmt.Sprintf("错误：工具 %q 不存在", response.Action)
				a.addStep(StepObservation, observation, "", "")
				a.history = append(a.history, observation)
				fmt.Printf("  👁️ 观察: %s\n", observation)
				continue
			}

			// 执行工具并记录结果
			result := tool.Execute(response.ActionInput)
			a.addStep(StepObservation, result, "", "")
			a.history = append(a.history, result)
			fmt.Printf("  👁️ 观察: %s\n\n", result)
		}
	}

	// 达到最大迭代次数
	timeout := "抱歉，经过多次推理仍未得出结论。请尝试简化问题。"
	a.addStep(StepFinalAnswer, timeout, "", "")
	fmt.Printf("  ⚠️ 达到最大迭代次数 (%d)，强制结束\n", a.maxIter)
	return timeout
}

// addStep 记录一个推理步骤
func (a *Agent) addStep(stepType StepType, content, toolName, toolInput string) {
	a.steps = append(a.steps, AgentStep{
		Type:      stepType,
		Content:   content,
		ToolName:  toolName,
		ToolInput: toolInput,
		Timestamp: time.Now(),
	})
}

// PrintReasoningChain 打印完整的推理链
// 便于调试和理解 Agent 的决策过程
func (a *Agent) PrintReasoningChain() {
	fmt.Println("=== 完整推理链 ===")
	for i, step := range a.steps {
		prefix := "  "
		switch step.Type {
		case StepThought:
			fmt.Printf("%s%d. [💭 %s] %s\n", prefix, i+1, step.Type, step.Content)
		case StepAction:
			fmt.Printf("%s%d. [🔧 %s] 工具=%s, 输入=%s\n", prefix, i+1, step.Type, step.ToolName, step.ToolInput)
		case StepObservation:
			fmt.Printf("%s%d. [👁️ %s] %s\n", prefix, i+1, step.Type, step.Content)
		case StepFinalAnswer:
			fmt.Printf("%s%d. [✅ %s] %s\n", prefix, i+1, step.Type, step.Content)
		}
	}
	fmt.Printf("  共 %d 个步骤\n\n", len(a.steps))
}

// ============================================================
// 工具实现
// ============================================================

// createWeatherTool 创建天气查询工具
func createWeatherTool() AgentTool {
	return AgentTool{
		Name:        "weather",
		Description: "查询指定城市的天气信息",
		Execute: func(city string) string {
			// 模拟天气数据
			data := map[string]string{
				"北京": "北京：晴，22°C，湿度45%，北风3级",
				"上海": "上海：多云，26°C，湿度72%，东风2级",
				"深圳": "深圳：雷阵雨，30°C，湿度85%，南风4级",
				"成都": "成都：阴，20°C，湿度68%，微风",
			}
			if result, ok := data[city]; ok {
				return result
			}
			return fmt.Sprintf("未找到 %s 的天气数据", city)
		},
	}
}

// createCalculatorTool 创建计算器工具
func createCalculatorTool() AgentTool {
	return AgentTool{
		Name:        "calculator",
		Description: "执行数学计算，输入格式如：3 + 5, 10 * 20",
		Execute: func(expr string) string {
			// 简单解析四则运算
			expr = strings.TrimSpace(expr)

			// 尝试解析 "a op b" 格式
			var a, b float64
			var op string
			n, _ := fmt.Sscanf(expr, "%f %s %f", &a, &op, &b)
			if n == 3 {
				var result float64
				switch op {
				case "+":
					result = a + b
				case "-":
					result = a - b
				case "*":
					result = a * b
				case "/":
					if b == 0 {
						return "错误：除数不能为零"
					}
					result = a / b
				default:
					return fmt.Sprintf("不支持的运算符: %s", op)
				}
				// 如果结果是整数则不显示小数点
				if result == float64(int64(result)) {
					return fmt.Sprintf("计算结果: %s = %.0f", expr, result)
				}
				return fmt.Sprintf("计算结果: %s = %.2f", expr, result)
			}
			return fmt.Sprintf("无法解析表达式: %s", expr)
		},
	}
}

// createSearchTool 创建知识搜索工具
func createSearchTool() AgentTool {
	return AgentTool{
		Name:        "search",
		Description: "搜索知识库获取相关信息",
		Execute: func(query string) string {
			// 模拟知识库搜索
			if containsAny(query, "Go", "golang") {
				return "Go 是 Google 于 2009 年发布的编程语言，以简洁的语法、出色的并发支持和高效的编译速度著称。"
			}
			return fmt.Sprintf("未找到与 %q 相关的知识", query)
		},
	}
}

// ============================================================
// 辅助函数
// ============================================================

// containsAny 检查字符串是否包含指定关键词中的任意一个
func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// extractCity 从查询中提取城市名
func extractCity(query string) string {
	cities := []string{"北京", "上海", "深圳", "成都"}
	for _, city := range cities {
		if strings.Contains(query, city) {
			return city
		}
	}
	return "北京" // 默认城市
}

// extractExpression 从查询中提取数学表达式
func extractExpression(query string) string {
	// 简单提取：查找数字和运算符
	if strings.Contains(query, "加") {
		return "100 + 200"
	}
	if strings.Contains(query, "乘") {
		return "12 * 15"
	}
	return "42 + 58"
}

// ============================================================
// 主函数 —— 演示完整的 Agent 工作流
// ============================================================

func main() {
	fmt.Println("=== Day 21: AI Agent —— ReAct 模式 ===")
	fmt.Println()

	// ReAct 模式说明
	fmt.Println("📖 ReAct (Reasoning + Acting) 模式：")
	fmt.Println("  1. 💭 Thought —— Agent 分析当前状态，规划下一步")
	fmt.Println("  2. 🔧 Action  —— 调用外部工具获取信息")
	fmt.Println("  3. 👁️ Observe —— 处理工具返回的结果")
	fmt.Println("  4. 🔄 循环以上步骤直到得出最终回答")
	fmt.Println()

	// ---- 1. 注册工具 ----
	tools := NewAgentToolRegistry()
	tools.Register(createWeatherTool())
	tools.Register(createCalculatorTool())
	tools.Register(createSearchTool())

	fmt.Println("📦 可用工具：")
	fmt.Print(tools.ListDescriptions())
	fmt.Println()

	// ---- 2. 场景一：天气查询（单工具调用）----
	fmt.Println("========================================")
	fmt.Println("📋 场景一：天气查询（单工具调用）")
	fmt.Println("========================================")
	agent1 := NewAgent(tools, 5)
	agent1.Run("北京天气怎么样？")
	agent1.PrintReasoningChain()

	// ---- 3. 场景二：数学计算（单工具调用）----
	fmt.Println("========================================")
	fmt.Println("📋 场景二：数学计算（单工具调用）")
	fmt.Println("========================================")
	agent2 := NewAgent(tools, 5)
	agent2.Run("请帮我计算 100 加 200 等于多少？")
	agent2.PrintReasoningChain()

	// ---- 4. 场景三：城市天气对比（多工具调用）----
	fmt.Println("========================================")
	fmt.Println("📋 场景三：城市天气对比（多工具调用）")
	fmt.Println("========================================")
	agent3 := NewAgent(tools, 5)
	agent3.Run("请对比北京和上海两个城市的天气")
	agent3.PrintReasoningChain()

	// ---- 5. 场景四：简单问题（无需工具）----
	fmt.Println("========================================")
	fmt.Println("📋 场景四：简单问题（无需工具调用）")
	fmt.Println("========================================")
	agent4 := NewAgent(tools, 5)
	agent4.Run("你好，你能做什么？")
	agent4.PrintReasoningChain()

	// ---- 6. 总结 ----
	fmt.Println("🎓 学习要点：")
	fmt.Println("  1. ReAct 模式通过交替推理和行动来解决复杂问题")
	fmt.Println("  2. Agent 的核心循环：Think → Act → Observe")
	fmt.Println("  3. 工具注册机制让 Agent 能力可扩展")
	fmt.Println("  4. 对话历史（记忆）保证了多步推理的连贯性")
	fmt.Println("  5. 最大迭代次数限制防止 Agent 陷入无限循环")
}
