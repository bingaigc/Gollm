// Day 02 - 切片（Slice）内部机制
// 本示例深入探讨切片的三元素头部（指针、长度、容量）、
// append 行为以及容量增长策略

package main

import (
	"fmt"
	"sort"
	"unsafe"
)

// === 切片创建方式 ===
// demonstrateCreation 展示创建切片的不同方法
func demonstrateCreation() {
	fmt.Println("=== 切片的多种创建方式 ===")

	// 方式一：使用字面量创建
	s1 := []int{1, 2, 3, 4, 5}
	fmt.Printf("字面量创建:   %v (len=%d, cap=%d)\n", s1, len(s1), cap(s1))

	// 方式二：使用 make 创建（指定长度和容量）
	s2 := make([]int, 3, 10)
	fmt.Printf("make 创建:    %v (len=%d, cap=%d)\n", s2, len(s2), cap(s2))

	// 方式三：从数组中切取
	arr := [5]string{"Go", "是", "最", "棒", "的"}
	s3 := arr[1:4] // 包含索引 1、2、3（左闭右开）
	fmt.Printf("数组切取:     %v (len=%d, cap=%d)\n", s3, len(s3), cap(s3))

	// 方式四：声明 nil 切片
	var s4 []int
	fmt.Printf("nil 切片:     %v (len=%d, cap=%d, nil=%t)\n", s4, len(s4), cap(s4), s4 == nil)

	// 方式五：空切片（非 nil）
	s5 := []int{}
	fmt.Printf("空切片:       %v (len=%d, cap=%d, nil=%t)\n", s5, len(s5), cap(s5), s5 == nil)

	fmt.Println()
}

// === 切片头部信息 ===
// showSliceHeader 展示切片的内部结构：指针、长度、容量
func showSliceHeader(label string, s []int) {
	// 切片头部由三个字段组成：
	// 1. 指向底层数组的指针
	// 2. 长度（len）：当前元素个数
	// 3. 容量（cap）：底层数组从切片起始位置到末尾的长度
	if len(s) > 0 {
		ptr := unsafe.Pointer(&s[0])
		fmt.Printf("[%s] 数据指针=%p, len=%d, cap=%d, 值=%v\n", label, ptr, len(s), cap(s), s)
	} else {
		fmt.Printf("[%s] 数据指针=<空>, len=%d, cap=%d, 值=%v\n", label, len(s), cap(s), s)
	}
}

// === append 增长行为 ===
// demonstrateAppendGrowth 展示 append 如何触发容量增长和内存重新分配
func demonstrateAppendGrowth() {
	fmt.Println("=== append 容量增长行为 ===")
	fmt.Println("当 append 超过当前容量时，Go 会分配新的底层数组")
	fmt.Println()

	s := make([]int, 0, 2)
	showSliceHeader("初始状态", s)

	// 连续 append，观察容量变化和地址变化
	for i := 1; i <= 10; i++ {
		oldCap := cap(s)
		var oldPtr unsafe.Pointer
		if len(s) > 0 {
			oldPtr = unsafe.Pointer(&s[0])
		}

		s = append(s, i)

		var newPtr unsafe.Pointer
		if len(s) > 0 {
			newPtr = unsafe.Pointer(&s[0])
		}

		// 当容量发生变化时，说明触发了重新分配
		if cap(s) != oldCap {
			fmt.Printf("  ⚡ 追加 %d → 容量从 %d 增长到 %d（地址: %p → %p 已重新分配！）\n",
				i, oldCap, cap(s), oldPtr, newPtr)
		} else {
			fmt.Printf("  ✅ 追加 %d → 容量不变 %d（地址: %p 未变）\n",
				i, cap(s), newPtr)
		}
	}

	fmt.Println()
}

// === 切片共享底层数组 ===
// demonstrateSharedArray 展示多个切片共享同一底层数组的行为
func demonstrateSharedArray() {
	fmt.Println("=== 切片共享底层数组 ===")

	original := []int{10, 20, 30, 40, 50}
	// sub 和 original 共享同一个底层数组
	sub := original[1:3]

	fmt.Printf("原始切片:   %v\n", original)
	fmt.Printf("子切片[1:3]: %v\n", sub)

	// 修改子切片会影响原始切片
	sub[0] = 999
	fmt.Printf("\n修改 sub[0] = 999 后：\n")
	fmt.Printf("原始切片:   %v  ← 也被修改了！\n", original)
	fmt.Printf("子切片:     %v\n", sub)

	// 使用 copy 创建独立副本
	independent := make([]int, len(sub))
	copy(independent, sub)
	independent[0] = 0
	fmt.Printf("\n使用 copy 创建独立副本后修改：\n")
	fmt.Printf("原始切片:   %v  ← 不受影响\n", original)
	fmt.Printf("独立副本:   %v\n", independent)

	fmt.Println()
}

// === 排序示例 ===
// demonstrateSorting 展示使用 sort 包对切片排序
func demonstrateSorting() {
	fmt.Println("=== 排序示例 ===")

	// 对整数切片排序
	numbers := []int{42, 17, 8, 99, 3, 56, 21}
	fmt.Printf("排序前: %v\n", numbers)
	sort.Ints(numbers)
	fmt.Printf("排序后: %v\n", numbers)

	// 对字符串切片排序
	names := []string{"张三", "Alice", "李四", "Bob", "王五"}
	fmt.Printf("\n字符串排序前: %v\n", names)
	sort.Strings(names)
	fmt.Printf("字符串排序后: %v\n", names)

	// 自定义排序：按字符串长度排序
	words := []string{"Go", "语言", "切片", "是", "非常强大的", "数据结构"}
	fmt.Printf("\n按长度排序前: %v\n", words)
	sort.Slice(words, func(i, j int) bool {
		return len(words[i]) < len(words[j])
	})
	fmt.Printf("按长度排序后: %v\n", words)

	fmt.Println()
}

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║   Day 02 - Go 切片深入解析       ║")
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Println()

	// 1. 切片创建方式
	demonstrateCreation()

	// 2. 切片头部信息
	fmt.Println("=== 切片头部信息（三元素结构） ===")
	s := []int{100, 200, 300}
	showSliceHeader("完整切片", s)
	showSliceHeader("子切片[1:]", s[1:])
	showSliceHeader("空切片", []int{})
	fmt.Println()

	// 3. append 增长行为
	demonstrateAppendGrowth()

	// 4. 共享底层数组
	demonstrateSharedArray()

	// 5. 排序
	demonstrateSorting()

	fmt.Println("📚 小结：切片是 Go 中最常用的数据结构之一，理解其内部机制非常重要！")
}
