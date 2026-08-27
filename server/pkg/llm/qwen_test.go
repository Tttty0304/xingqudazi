package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestQwenClient_GenerateBotMessage_Success 覆盖正常路径：验证请求体（model/
// messages/Authorization 头）构造正确，且能正确解析 OpenAI 兼容格式的响应，
// 提取出生成的文本内容与 usage 摘要（用真实 httptest.Server 模拟网络往返，
// 而非纯 mock 断言调用参数，与本项目其它 pkg 测试的一贯风格一致）。
func TestQwenClient_GenerateBotMessage_Success(t *testing.T) {
	var capturedAuth string
	var capturedBody chatCompletionRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		resp := chatCompletionResponse{Model: "qwen-plus"}
		resp.Choices = []struct {
			Message chatMessage `json:"message"`
		}{{Message: chatMessage{Role: "assistant", Content: "大家好，我是AI小助手，很高兴认识大家！"}}}
		resp.Usage.PromptTokens = 20
		resp.Usage.CompletionTokens = 15
		resp.Usage.TotalTokens = 35

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewQwenClient(server.URL, "sk-test-key", "qwen-plus")
	content, usageSummary, err := client.GenerateBotMessage(context.Background(), "系统提示词", "用户提示词")
	if err != nil {
		t.Fatalf("GenerateBotMessage failed: %v", err)
	}

	if capturedAuth != "Bearer sk-test-key" {
		t.Fatalf("expected Authorization header 'Bearer sk-test-key', got %q", capturedAuth)
	}
	if capturedBody.Model != "qwen-plus" {
		t.Fatalf("expected model=qwen-plus in request, got %q", capturedBody.Model)
	}
	if len(capturedBody.Messages) != 2 || capturedBody.Messages[0].Role != "system" || capturedBody.Messages[1].Role != "user" {
		t.Fatalf("expected [system, user] messages, got %+v", capturedBody.Messages)
	}
	if content != "大家好，我是AI小助手，很高兴认识大家！" {
		t.Fatalf("expected generated content to be extracted from response, got %q", content)
	}
	if !strings.Contains(usageSummary, "total_tokens=35") {
		t.Fatalf("expected usage summary to contain total_tokens=35, got %q", usageSummary)
	}
}

// TestQwenClient_GenerateBotMessage_EmptyAPIKey 覆盖边界：未配置 API Key 时
// 应直接返回明确的错误，而不是发出一个必然会被服务端拒绝的网络请求。
func TestQwenClient_GenerateBotMessage_EmptyAPIKey(t *testing.T) {
	client := NewQwenClient("http://localhost:1", "", "qwen-plus")
	_, _, err := client.GenerateBotMessage(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error when api key is empty")
	}
	if !strings.Contains(err.Error(), "api key is empty") {
		t.Fatalf("expected error message to mention empty api key, got: %v", err)
	}
}

// TestQwenClient_GenerateBotMessage_APIErrorResponse 覆盖非 200 响应：应把
// 服务端返回的具体错误信息（而非只有状态码）暴露给调用方，方便真实排查
// （如 API Key 无效/欠费等场景）。
func TestQwenClient_GenerateBotMessage_APIErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer server.Close()

	client := NewQwenClient(server.URL, "sk-invalid-key", "qwen-plus")
	_, _, err := client.GenerateBotMessage(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "Invalid API key provided") {
		t.Fatalf("expected error to surface the api error message, got: %v", err)
	}
}

// TestQwenClient_GenerateBotMessage_NoChoices 覆盖边界：响应体解析成功但
// choices 为空数组（理论上不应发生，但防御性处理，避免下游对空切片取索引0
// 导致 panic）。
func TestQwenClient_GenerateBotMessage_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"qwen-plus","choices":[],"usage":{}}`))
	}))
	defer server.Close()

	client := NewQwenClient(server.URL, "sk-test-key", "qwen-plus")
	_, _, err := client.GenerateBotMessage(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error when response has no choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected error to mention no choices, got: %v", err)
	}
}

// TestNewQwenClient_DefaultsWhenEmpty 覆盖 NewQwenClient 在 baseURL/model 传空
// 字符串时回落到 DashScope 默认值（方便调用方直接传 config.Config 里可能为空
// 的字段）。
func TestNewQwenClient_DefaultsWhenEmpty(t *testing.T) {
	client := NewQwenClient("", "sk-test", "")
	if client.baseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("unexpected default baseURL: %q", client.baseURL)
	}
	if client.model != "qwen-plus" {
		t.Fatalf("unexpected default model: %q", client.model)
	}
}
