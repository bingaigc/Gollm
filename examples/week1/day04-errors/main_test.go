package main

import (
	"errors"
	"testing"
)

func TestReadFileSuccess(t *testing.T) {
	content, err := readFile("hello.txt")
	if err != nil {
		t.Fatalf("读取 hello.txt 不应返回错误, 实际: %v", err)
	}
	if content == "" {
		t.Error("读取 hello.txt 应返回非空内容")
	}
}

func TestReadFileMissing(t *testing.T) {
	_, err := readFile("missing.txt")
	if err == nil {
		t.Fatal("读取 missing.txt 应返回错误")
	}
	var fnfErr *FileNotFoundError
	if !errors.As(err, &fnfErr) {
		t.Errorf("错误类型应为 FileNotFoundError, 实际: %T", err)
	}
}

func TestReadFilePermission(t *testing.T) {
	_, err := readFile("secret.txt")
	if err == nil {
		t.Fatal("读取 secret.txt 应返回错误")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Errorf("错误类型应为 PermissionError, 实际: %T", err)
	}
}

func TestReadFileTimeout(t *testing.T) {
	_, err := readFile("slow.txt")
	if err == nil {
		t.Fatal("读取 slow.txt 应返回错误")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Errorf("错误类型应为 TimeoutError, 实际: %T", err)
	}
}

func TestErrorsIs(t *testing.T) {
	// loadConfig("unknown.txt") 包装链中包含 ErrFileNotFound 哨兵错误
	err := loadConfig("unknown.txt")
	if err == nil {
		t.Fatal("loadConfig 应返回错误")
	}
	if !errors.Is(err, ErrFileNotFound) {
		t.Error("错误链中应包含 ErrFileNotFound")
	}
	if errors.Is(err, ErrPermission) {
		t.Error("错误链中不应包含 ErrPermission")
	}
}

func TestErrorsAs(t *testing.T) {
	// loadConfig("missing.txt") 包装链中包含 FileNotFoundError
	err := loadConfig("missing.txt")
	if err == nil {
		t.Fatal("loadConfig 应返回错误")
	}
	var fnfErr *FileNotFoundError
	if !errors.As(err, &fnfErr) {
		t.Error("应能从错误链中提取 FileNotFoundError")
	}
	if fnfErr.FileName != "missing.txt" {
		t.Errorf("文件名应为 missing.txt, 实际: %s", fnfErr.FileName)
	}
}

func TestTimeoutErrorInterface(t *testing.T) {
	te := &TimeoutError{Operation: "test", Duration: 0}
	if !te.Timeout() {
		t.Error("TimeoutError.Timeout() 应返回 true")
	}
}
