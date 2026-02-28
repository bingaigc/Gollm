// Day 03 - 接口（Interface）
// 本示例演示 Go 接口的核心概念：
// - 接口的定义与隐式实现
// - 多种实现（JSON、YAML、纯文本）
// - 接口作为函数参数实现多态
// - 接口切换与类型断言

package main

import (
	"fmt"
	"strings"
	"time"
)

// Formatter 是一个格式化接口
// 任何实现了 Format(msg string) string 方法的类型都自动满足此接口
type Formatter interface {
	Format(msg string) string
}

// === JSON 格式化器 ===

// JSONFormatter 将消息格式化为 JSON 风格的输出
type JSONFormatter struct {
	// Indent 控制是否使用缩进（美化输出）
	Indent bool
}

// Format 实现 Formatter 接口 —— JSON 格式
func (f JSONFormatter) Format(msg string) string {
	timestamp := time.Now().Format("2006-01-02T15:04:05Z")
	if f.Indent {
		// 美化 JSON 格式
		return fmt.Sprintf("{\n  \"timestamp\": \"%s\",\n  \"message\": \"%s\",\n  \"level\": \"info\"\n}", timestamp, msg)
	}
	// 紧凑 JSON 格式
	return fmt.Sprintf("{\"timestamp\":\"%s\",\"message\":\"%s\",\"level\":\"info\"}", timestamp, msg)
}

// === YAML 格式化器 ===

// YAMLFormatter 将消息格式化为 YAML 风格的输出
type YAMLFormatter struct{}

// Format 实现 Formatter 接口 —— YAML 风格格式
func (f YAMLFormatter) Format(msg string) string {
	timestamp := time.Now().Format("2006-01-02T15:04:05Z")
	lines := []string{
		"---",
		fmt.Sprintf("timestamp: \"%s\"", timestamp),
		fmt.Sprintf("message: \"%s\"", msg),
		"level: info",
	}
	return strings.Join(lines, "\n")
}

// === 纯文本格式化器 ===

// PlainTextFormatter 将消息格式化为简单的纯文本
type PlainTextFormatter struct {
	// Prefix 是添加在消息前面的前缀
	Prefix string
}

// Format 实现 Formatter 接口 —— 纯文本格式
func (f PlainTextFormatter) Format(msg string) string {
	timestamp := time.Now().Format("15:04:05")
	prefix := f.Prefix
	if prefix == "" {
		prefix = "INFO"
	}
	return fmt.Sprintf("[%s] %s | %s", timestamp, prefix, msg)
}

// === 接口作为函数参数 ===

// PrintWithFormat 接受任意实现了 Formatter 接口的对象
// 这就是 Go 多态的核心：面向接口编程
func PrintWithFormat(f Formatter, msg string) {
	result := f.Format(msg)
	fmt.Println(result)
}

// === 类型断言与接口切换 ===

// describeFormatter 使用类型断言（type switch）识别具体类型
func describeFormatter(f Formatter) {
	// type switch 是 Go 中检查接口底层类型的惯用方法
	switch v := f.(type) {
	case JSONFormatter:
		indent := "紧凑"
		if v.Indent {
			indent = "美化"
		}
		fmt.Printf("  → 这是 JSON 格式化器（%s模式）\n", indent)
	case YAMLFormatter:
		fmt.Println("  → 这是 YAML 格式化器")
	case PlainTextFormatter:
		fmt.Printf("  → 这是纯文本格式化器（前缀: %q）\n", v.Prefix)
	default:
		fmt.Println("  → 未知的格式化器类型")
	}
}

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║   Day 03 - Go 接口与多态         ║")
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Println()

	// 创建三种不同的格式化器
	jsonFmt := JSONFormatter{Indent: true}
	yamlFmt := YAMLFormatter{}
	plainFmt := PlainTextFormatter{Prefix: "LOG"}

	// 将所有格式化器放入同一个切片中
	// 这是接口的强大之处：不同类型可以统一处理
	formatters := []Formatter{jsonFmt, yamlFmt, plainFmt}
	labels := []string{"JSON", "YAML", "纯文本"}

	msg := "Go 接口是隐式实现的，无需显式声明"

	// 遍历所有格式化器，展示多态行为
	for i, f := range formatters {
		fmt.Printf("--- %s 格式 ---\n", labels[i])
		PrintWithFormat(f, msg)
		describeFormatter(f)
		fmt.Println()
	}

	// === 接口切换示例 ===
	fmt.Println("=== 动态切换格式化器 ===")
	formats := []string{"json", "yaml", "plain", "unknown"}
	for _, name := range formats {
		var f Formatter
		// 根据名称动态选择格式化器
		switch name {
		case "json":
			f = JSONFormatter{Indent: false}
		case "yaml":
			f = YAMLFormatter{}
		case "plain":
			f = PlainTextFormatter{Prefix: "DEBUG"}
		default:
			fmt.Printf("⚠️  未知格式 %q，使用默认纯文本格式\n", name)
			f = PlainTextFormatter{Prefix: "DEFAULT"}
		}
		fmt.Printf("[%s] ", name)
		PrintWithFormat(f, "动态切换测试")
	}

	fmt.Println()

	// === 类型断言（单类型） ===
	fmt.Println("=== 类型断言示例 ===")
	var f Formatter = JSONFormatter{Indent: true}

	// 安全的类型断言：使用 ok 模式避免 panic
	if jf, ok := f.(JSONFormatter); ok {
		fmt.Printf("✅ 类型断言成功：JSONFormatter{Indent: %t}\n", jf.Indent)
	}

	// 断言为错误的类型
	if _, ok := f.(YAMLFormatter); !ok {
		fmt.Println("❌ 类型断言失败：不是 YAMLFormatter（符合预期）")
	}

	fmt.Println()
	fmt.Println("📚 小结：Go 的接口是隐式实现的，这使得代码更加灵活和解耦！")
}
