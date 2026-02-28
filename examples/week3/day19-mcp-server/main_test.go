package main

import (
	"math"
	"testing"
)

func TestToolRegistration(t *testing.T) {
	registry := NewToolRegistry()
	registerWeatherTool(registry)
	registerCalculatorTool(registry)

	tools := registry.ListTools()
	if len(tools) != 2 {
		t.Fatalf("应注册 2 个工具, 实际: %d", len(tools))
	}
	if tools[0].Name != "get_weather" {
		t.Errorf("第一个工具应为 get_weather, 实际: %s", tools[0].Name)
	}
	if tools[1].Name != "calculator" {
		t.Errorf("第二个工具应为 calculator, 实际: %s", tools[1].Name)
	}
}

func TestCalculatorTool(t *testing.T) {
	registry := NewToolRegistry()
	registerCalculatorTool(registry)

	result, err := registry.CallTool("calculator", map[string]interface{}{
		"expression": "add 3 5",
	})
	if err != nil {
		t.Fatalf("计算器调用不应返回错误, 实际: %v", err)
	}
	if result.IsError {
		t.Errorf("计算结果不应标记为错误, 内容: %s", result.Content[0].Text)
	}
	if len(result.Content) == 0 {
		t.Fatal("计算结果不应为空")
	}
}

func TestWeatherTool(t *testing.T) {
	registry := NewToolRegistry()
	registerWeatherTool(registry)

	result, err := registry.CallTool("get_weather", map[string]interface{}{
		"city": "北京",
	})
	if err != nil {
		t.Fatalf("天气查询不应返回错误, 实际: %v", err)
	}
	if result.IsError {
		t.Errorf("天气查询不应标记为错误")
	}
	if len(result.Content) == 0 || result.Content[0].Text == "" {
		t.Error("天气查询应返回非空结果")
	}
}

func TestEvaluateExpression(t *testing.T) {
	tests := []struct {
		expr   string
		want   float64
		hasErr bool
	}{
		{"add 1 2", 3, false},
		{"sub 10 3", 7, false},
		{"mul 4 5", 20, false},
		{"div 10 2", 5, false},
		{"sqrt 16", 4, false},
		{"pow 2 10", 1024, false},
		{"div 10 0", 0, true},
	}

	for _, tt := range tests {
		result, err := evaluateExpression(tt.expr)
		if tt.hasErr {
			if err == nil {
				t.Errorf("表达式 %q 应返回错误", tt.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("表达式 %q 不应返回错误, 实际: %v", tt.expr, err)
			continue
		}
		if math.Abs(result-tt.want) > 1e-9 {
			t.Errorf("表达式 %q 期望 %v, 实际 %v", tt.expr, tt.want, result)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{42, "42"},
		{3.1415, "3.1415"},
		{100, "100"},
		{0, "0"},
	}

	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%v) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

func TestUnknownTool(t *testing.T) {
	registry := NewToolRegistry()

	result, err := registry.CallTool("nonexistent", map[string]interface{}{})
	if err == nil {
		t.Fatal("调用未知工具应返回错误")
	}
	if result == nil || !result.IsError {
		t.Error("调用未知工具应返回标记为错误的结果")
	}
}
