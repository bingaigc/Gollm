package main

import (
	"strings"
	"testing"
)

func TestJSONFormatterIndent(t *testing.T) {
	f := JSONFormatter{Indent: true}
	result := f.Format("hello")
	if !strings.Contains(result, `"message":`) {
		t.Errorf("缩进模式应包含 \"message:\", 实际输出: %s", result)
	}
}

func TestJSONFormatterCompact(t *testing.T) {
	f := JSONFormatter{Indent: false}
	result := f.Format("hello")
	if strings.Contains(result, "\n") {
		t.Errorf("紧凑模式不应包含换行符, 实际输出: %s", result)
	}
	if !strings.Contains(result, `"message":"hello"`) {
		t.Errorf("紧凑模式应包含消息内容, 实际输出: %s", result)
	}
}

func TestYAMLFormatter(t *testing.T) {
	f := YAMLFormatter{}
	result := f.Format("hello")
	if !strings.HasPrefix(result, "---") {
		t.Errorf("YAML 输出应以 --- 开头, 实际输出: %s", result)
	}
}

func TestPlainTextFormatter(t *testing.T) {
	f := PlainTextFormatter{Prefix: "DEBUG"}
	result := f.Format("hello")
	if !strings.Contains(result, "DEBUG") {
		t.Errorf("纯文本输出应包含前缀 DEBUG, 实际输出: %s", result)
	}
}

func TestPlainTextFormatterDefaultPrefix(t *testing.T) {
	f := PlainTextFormatter{Prefix: ""}
	result := f.Format("hello")
	if !strings.Contains(result, "INFO") {
		t.Errorf("空前缀时应使用默认值 INFO, 实际输出: %s", result)
	}
}

func TestFormatterInterface(t *testing.T) {
	// 编译时检查：所有类型都实现了 Formatter 接口
	var _ Formatter = JSONFormatter{}
	var _ Formatter = YAMLFormatter{}
	var _ Formatter = PlainTextFormatter{}
}
