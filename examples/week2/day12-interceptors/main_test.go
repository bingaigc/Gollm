package main

import (
	"context"
	"testing"
)

func TestChainInterceptors(t *testing.T) {
	// 简单的透传拦截器
	passThrough := func(ctx context.Context, req *Request, next Handler) (*Response, error) {
		return next(ctx, req)
	}

	chain := ChainInterceptors(passThrough)
	handler := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Code: 200, Message: "OK"}, nil
	}

	req := &Request{Method: "Test", Body: "hello"}
	resp, err := chain(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("链式调用不应返回错误, 实际: %v", err)
	}
	if resp.Code != 200 {
		t.Errorf("状态码应为 200, 实际: %d", resp.Code)
	}
	if resp.Message != "OK" {
		t.Errorf("消息应为 OK, 实际: %s", resp.Message)
	}
}

func TestRateLimiter(t *testing.T) {
	bucket := NewTokenBucket(1.0, 2) // 每秒1个，容量2

	// 前两个请求应被允许（突发容量为2）
	if !bucket.Allow() {
		t.Error("第1个请求应被允许")
	}
	if !bucket.Allow() {
		t.Error("第2个请求应被允许")
	}
	// 第三个请求应被拒绝（令牌已耗尽）
	if bucket.Allow() {
		t.Error("第3个请求应被拒绝（令牌已耗尽）")
	}
}

func TestChatHandler(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyUserID, "test-user")
	ctx = context.WithValue(ctx, ctxKeyRequestID, "req-001")

	req := &Request{Method: "Chat", Body: "你好"}
	resp, err := chatHandler(ctx, req)
	if err != nil {
		t.Fatalf("chatHandler 不应返回错误, 实际: %v", err)
	}
	if resp.Code != 200 {
		t.Errorf("状态码应为 200, 实际: %d", resp.Code)
	}
	if resp.Message == "" {
		t.Error("响应消息不应为空")
	}
}
