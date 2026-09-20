package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Model 是对"对话 + 工具调用"能力的最小抽象。
// 循环层只依赖此接口,测试时可注入假模型,不依赖任何厂商 SDK。
type Model interface {
	Complete(ctx context.Context, msgs []Message, tools []ToolSpec) (Completion, error)
}

// OpenAICompatible 实现 OpenAI Chat Completions 协议的模型客户端。
// DeepSeek / Kimi / Qwen 兼容模式 / Ollama 均兼容此协议,换厂商只改 BaseURL 和 Model。
type OpenAICompatible struct {
	BaseURL string // 如 https://api.deepseek.com,不带 /chat/completions
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewOpenAICompatible 组装客户端。HTTP 为空时使用 60s 超时:
// LLM 推理耗时显著高于普通接口,超时上限相应放宽,但必须存在。
func NewOpenAICompatible(baseURL, apiKey, model string) *OpenAICompatible {
	return &OpenAICompatible{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// chatRequest/chatResponse 只声明协议中实际用到的字段,
// 与厂商 SDK 保持独立,厂商新增字段不影响本层。
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []any     `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   *string    `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`

	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete 发送一次对话请求。工具清单按协议包装为
// {"type":"function","function":spec};无工具时不发送 tools 字段。
func (o *OpenAICompatible) Complete(ctx context.Context, msgs []Message, tools []ToolSpec) (Completion, error) {
	var toolPayload []any
	for _, t := range tools {
		toolPayload = append(toolPayload, map[string]any{"type": "function", "function": t})
	}

	body, err := json.Marshal(chatRequest{Model: o.Model, Messages: msgs, Tools: toolPayload})
	if err != nil {
		return Completion{}, fmt.Errorf("agent: 编码请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("agent: 构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return Completion{}, fmt.Errorf("agent: 模型请求失败: %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Completion{}, fmt.Errorf("agent: 解析模型响应(http %d): %w", resp.StatusCode, err)
	}
	// 部分厂商以 200 + error 字段返回业务错误,不能只看状态码。
	if out.Error != nil {
		return Completion{}, fmt.Errorf("agent: 模型返回错误(%s): %s", out.Error.Type, out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return Completion{}, fmt.Errorf("agent: 模型 http %d", resp.StatusCode)
	}
	if len(out.Choices) == 0 {
		return Completion{}, fmt.Errorf("agent: 模型响应没有 choices")
	}

	choice := out.Choices[0]
	c := Completion{Calls: choice.Message.ToolCalls}
	if choice.Message.Content != nil {
		c.Text = *choice.Message.Content
	}
	return c, nil
}
