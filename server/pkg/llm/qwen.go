// Package llm 提供驱动机器人行为所需的最小 LLM 客户端接口（能力补齐项：给
// 此前只有 schema 预留、从未真正跑通的"LLM驱动机器人参与社交"设计补一个最小
// 验证，见 server/cmd/bot）。
//
// 只实现最小可用的子集：一次"系统提示词+用户提示词 -> 生成文本"的对话补全
// 调用，不涉及流式输出、工具调用（function calling）、多轮历史等——这些超出
// 了"机器人在房间里发一条 LLM 生成的开场白"这个最小验证场景的需要。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client 是驱动机器人行为的 LLM 客户端抽象，供 cmd/bot 依赖；真实实现见
// QwenClient（通义千问/百炼平台，DashScope 提供的 OpenAI 兼容模式接口）。
// 抽成接口是为了未来若要切换/新增其它 LLM 供应商（`config.LLMProvider`
// 字段已预留），cmd/bot 的调用代码不需要改动。
type Client interface {
	// GenerateBotMessage 调用 LLM 生成一段文本，systemPrompt 设定角色/风格，
	// userPrompt 是具体的生成请求（如"给这个房间生成一条开场白"）。返回内容
	// 与供服务日志/bot_action_log 留痕使用的原始响应摘要（RawUsageSummary，
	// 便于证明这条内容确实来自一次真实的 LLM 调用，而非硬编码模板）。
	GenerateBotMessage(ctx context.Context, systemPrompt, userPrompt string) (content string, rawUsageSummary string, err error)
}

// chatMessage / chatCompletionRequest / chatCompletionResponse 是 OpenAI 兼容
// 的 Chat Completions 协议最小子集（DashScope 的 compatible-mode 端点、以及
// 绝大多数其它 LLM 供应商都实现了这套协议的一个子集，未来切换供应商时这部分
// 结构体大概率可以直接复用）。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

// chatCompletionErrorResponse 覆盖 DashScope/OpenAI 兼容接口返回的错误结构，
// 用于把 4xx/5xx 响应体里的具体错误信息（而非只有状态码）暴露给调用方，
// 方便真实排查（如 API Key 无效/欠费/模型名不存在等）。
type chatCompletionErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// QwenClient 是通义千问（百炼/DashScope 平台）的 Client 实现，通过其 OpenAI
// 兼容模式端点（`{baseURL}/chat/completions`）调用，不引入官方 SDK 依赖——
// 与本项目 Web Push（pkg/webpush）"自己理解并实现协议"的一贯风格保持一致，
// 且 OpenAI 兼容协议本身足够简单，没必要为此新增 go.mod 依赖。
type QwenClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewQwenClient 构造一个 QwenClient；baseURL/model 传空字符串时分别回落到
// DashScope 默认值，方便调用方直接把 config.Config 里可能为空的字段传进来。
func NewQwenClient(baseURL, apiKey, model string) *QwenClient {
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	if model == "" {
		model = "qwen-plus"
	}
	return &QwenClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GenerateBotMessage 实现 Client 接口，见接口注释。
func (c *QwenClient) GenerateBotMessage(ctx context.Context, systemPrompt, userPrompt string) (string, string, error) {
	if c.apiKey == "" {
		return "", "", fmt.Errorf("llm api key is empty: 请设置 LLM_API_KEY 或 DASHSCOPE_API_KEY 环境变量")
	}

	reqBody, err := json.Marshal(chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal chat completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("call llm api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read llm response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp chatCompletionErrorResponse
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error.Message != "" {
			return "", "", fmt.Errorf("llm api returned %d: %s (type=%s, code=%s)",
				resp.StatusCode, errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		}
		return "", "", fmt.Errorf("llm api returned %d: %s", resp.StatusCode, string(body))
	}

	var result chatCompletionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("unmarshal llm response: %w (raw=%s)", err, string(body))
	}
	if len(result.Choices) == 0 {
		return "", "", fmt.Errorf("llm response contains no choices (raw=%s)", string(body))
	}

	rawUsageSummary := fmt.Sprintf("model=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d",
		result.Model, result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
	return result.Choices[0].Message.Content, rawUsageSummary, nil
}
