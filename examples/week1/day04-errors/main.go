// Day 04 - 错误处理（Error Handling）
// 本示例演示 Go 的错误处理机制：
// - 自定义错误类型
// - 使用 fmt.Errorf("%w") 包装错误
// - 使用 errors.Is 判断错误链中的特定值
// - 使用 errors.As 提取错误链中的特定类型

package main

import (
	"errors"
	"fmt"
	"time"
)

// =============================
// 自定义错误类型
// =============================

// FileNotFoundError 表示文件未找到错误
type FileNotFoundError struct {
	// FileName 是未找到的文件名
	FileName string
}

// Error 实现 error 接口
func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("文件未找到: %s", e.FileName)
}

// PermissionError 表示权限不足错误
type PermissionError struct {
	// FileName 是访问受限的文件名
	FileName string
	// User 是尝试访问的用户
	User string
}

// Error 实现 error 接口
func (e *PermissionError) Error() string {
	return fmt.Sprintf("权限不足: 用户 %q 无法访问 %s", e.User, e.FileName)
}

// TimeoutError 表示读取超时错误
type TimeoutError struct {
	// Operation 是超时的操作名称
	Operation string
	// Duration 是超时时长
	Duration time.Duration
}

// Error 实现 error 接口
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("操作超时: %s（等待了 %v）", e.Operation, e.Duration)
}

// Timeout 返回 true，标识这是一个超时错误
// 这是一个常见的接口约定，net 包也使用此模式
func (e *TimeoutError) Timeout() bool {
	return true
}

// =============================
// 哨兵错误（Sentinel Errors）
// =============================

// 定义哨兵错误：用于 errors.Is 精确匹配
var (
	ErrFileNotFound = errors.New("file not found")
	ErrPermission   = errors.New("permission denied")
	ErrTimeout      = errors.New("read timeout")
)

// =============================
// 模拟文件读取函数
// =============================

// readFile 模拟文件读取操作，根据文件名返回不同的错误
func readFile(filename string) (string, error) {
	// 模拟不同的错误场景
	switch filename {
	case "missing.txt":
		// 返回自定义错误类型
		return "", &FileNotFoundError{FileName: filename}
	case "secret.txt":
		// 返回自定义权限错误
		return "", &PermissionError{FileName: filename, User: "guest"}
	case "slow.txt":
		// 返回自定义超时错误
		return "", &TimeoutError{Operation: "读取文件", Duration: 30 * time.Second}
	case "hello.txt":
		// 成功读取
		return "你好，Go 错误处理！", nil
	default:
		// 使用哨兵错误
		return "", fmt.Errorf("读取 %s: %w", filename, ErrFileNotFound)
	}
}

// =============================
// 错误包装链示例
// =============================

// processFile 在 readFile 之上添加一层错误包装
// 使用 %w 动词将原始错误包装进新的上下文信息中
func processFile(filename string) error {
	content, err := readFile(filename)
	if err != nil {
		// 使用 fmt.Errorf 和 %w 包装错误，保留原始错误链
		return fmt.Errorf("处理文件 %q 失败: %w", filename, err)
	}
	fmt.Printf("  📄 文件内容: %s\n", content)
	return nil
}

// loadConfig 在 processFile 之上再添加一层包装
// 展示多层错误包装
func loadConfig(filename string) error {
	err := processFile(filename)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}
	return nil
}

// =============================
// 演示函数
// =============================

// demonstrateCustomErrors 展示自定义错误类型的使用
func demonstrateCustomErrors() {
	fmt.Println("=== 自定义错误类型 ===")

	files := []string{"missing.txt", "secret.txt", "slow.txt", "hello.txt"}

	for _, f := range files {
		content, err := readFile(f)
		if err != nil {
			fmt.Printf("  ❌ 读取 %s: %v\n", f, err)
		} else {
			fmt.Printf("  ✅ 读取 %s: %q\n", f, content)
		}
	}
	fmt.Println()
}

// demonstrateErrorsIs 展示 errors.Is 的用法
func demonstrateErrorsIs() {
	fmt.Println("=== errors.Is —— 检查错误链中的特定值 ===")

	// 创建一个多层包装的错误
	err := loadConfig("unknown.txt")
	fmt.Printf("  完整错误: %v\n", err)

	// errors.Is 会遍历整个错误链查找匹配的哨兵错误
	if errors.Is(err, ErrFileNotFound) {
		fmt.Println("  ✅ errors.Is 找到了 ErrFileNotFound（即使经过多层包装）")
	}
	if !errors.Is(err, ErrPermission) {
		fmt.Println("  ❌ errors.Is 未找到 ErrPermission（符合预期）")
	}
	if !errors.Is(err, ErrTimeout) {
		fmt.Println("  ❌ errors.Is 未找到 ErrTimeout（符合预期）")
	}
	fmt.Println()
}

// demonstrateErrorsAs 展示 errors.As 的用法
func demonstrateErrorsAs() {
	fmt.Println("=== errors.As —— 从错误链中提取特定类型 ===")

	// 场景 1：提取 FileNotFoundError
	err1 := loadConfig("missing.txt")
	var fnfErr *FileNotFoundError
	// errors.As 遍历错误链，找到匹配类型后赋值给目标变量
	if errors.As(err1, &fnfErr) {
		fmt.Printf("  ✅ 找到 FileNotFoundError: 文件名=%s\n", fnfErr.FileName)
	}

	// 场景 2：提取 PermissionError
	err2 := loadConfig("secret.txt")
	var permErr *PermissionError
	if errors.As(err2, &permErr) {
		fmt.Printf("  ✅ 找到 PermissionError: 用户=%s, 文件=%s\n", permErr.User, permErr.FileName)
	}

	// 场景 3：提取 TimeoutError 并调用其方法
	err3 := loadConfig("slow.txt")
	var timeoutErr *TimeoutError
	if errors.As(err3, &timeoutErr) {
		fmt.Printf("  ✅ 找到 TimeoutError: 操作=%s, 时长=%v, 是超时=%t\n",
			timeoutErr.Operation, timeoutErr.Duration, timeoutErr.Timeout())
	}

	// 场景 4：尝试提取不存在的类型
	if !errors.As(err1, &permErr) {
		fmt.Println("  ❌ 在 FileNotFoundError 中未找到 PermissionError（符合预期）")
	}
	fmt.Println()
}

// demonstrateErrorWrapping 展示错误包装链的解包过程
func demonstrateErrorWrapping() {
	fmt.Println("=== 错误包装与解包 ===")

	err := loadConfig("missing.txt")
	fmt.Println("  错误链（从外到内）:")

	// 使用 errors.Unwrap 逐层解包
	current := err
	depth := 1
	for current != nil {
		fmt.Printf("    第 %d 层: %v\n", depth, current)
		current = errors.Unwrap(current)
		depth++
	}
	fmt.Println()
}

// demonstrateErrorHandlingPattern 展示实际项目中的错误处理模式
func demonstrateErrorHandlingPattern() {
	fmt.Println("=== 实际错误处理模式 ===")

	// 模拟处理一批文件
	files := []string{"hello.txt", "missing.txt", "secret.txt", "slow.txt"}

	for _, filename := range files {
		err := processFile(filename)
		if err == nil {
			continue
		}

		// 根据错误类型采取不同的恢复策略
		var fnfErr *FileNotFoundError
		var permErr *PermissionError
		var timeoutErr *TimeoutError

		switch {
		case errors.As(err, &fnfErr):
			fmt.Printf("  📁 文件 %s 不存在，跳过\n", fnfErr.FileName)
		case errors.As(err, &permErr):
			fmt.Printf("  🔒 用户 %s 无权访问 %s，请联系管理员\n", permErr.User, permErr.FileName)
		case errors.As(err, &timeoutErr):
			// 超时错误可以尝试重试
			fmt.Printf("  ⏰ 操作 %s 超时，正在重试...\n", timeoutErr.Operation)
		default:
			fmt.Printf("  ⚠️  未知错误: %v\n", err)
		}
	}
	fmt.Println()
}

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║   Day 04 - Go 错误处理深入       ║")
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Println()

	// 1. 自定义错误类型
	demonstrateCustomErrors()

	// 2. errors.Is 示例
	demonstrateErrorsIs()

	// 3. errors.As 示例
	demonstrateErrorsAs()

	// 4. 错误包装链
	demonstrateErrorWrapping()

	// 5. 实际错误处理模式
	demonstrateErrorHandlingPattern()

	fmt.Println("📚 小结：Go 的错误处理强调显式检查，errors.Is 和 errors.As 是处理错误链的关键工具！")
}
