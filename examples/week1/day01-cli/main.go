// Day 01 - CLI 工具基础
// 本示例演示如何使用 flag 包构建命令行工具
// 支持 --name、--version 和 --help 标志

package main

import (
	"flag"
	"fmt"
	"os"
)

// 版本号常量
const version = "1.0.0"

func main() {
	// 定义命令行参数
	// flag.String 返回一个指向字符串的指针
	name := flag.String("name", "Gopher", "你的名字")
	// flag.Bool 返回一个指向布尔值的指针
	showVersion := flag.Bool("version", false, "显示版本号")
	// 解析命令行参数（必须在所有 flag 定义之后调用）
	flag.Parse()

	// 如果用户请求版本信息，打印后退出
	if *showVersion {
		fmt.Printf("Gollm CLI v%s\n", version)
		os.Exit(0)
	}

	// 使用解引用操作符 * 获取指针指向的值
	fmt.Printf("🚀 欢迎 %s 进入 Go 的世界！\n", *name)
	fmt.Println("💡 提示：使用 --help 查看所有选项")
}
