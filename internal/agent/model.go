package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Delta 模型流式输出的一片增量。
// Content 是给用户看的正文;Reasoning 是推理模型的思考过程
// (如 deepseek-v4-pro 的 reasoning_content),两者在 UI 上区别呈现。
type Delta struct {
	Content   string
	Reasoning string
}

// Model 是对"对话 + 工具调用"能力的最小抽象。
// onDelta 为流式回调(可为 nil):模型的每片增量文本都会推给它,
// 上层据此把"思考中/正在回答"实时呈现给用户。
type Model interface {
	Complete(ctx context.Context, msgs []Message, tools []ToolSpec, onDelta func(Delta)) (Completion, error)
}

// OpenAICompatible 实现 OpenAI Chat Completions 协议的模型客户端(SSE 流式)。
// DeepSeek / Kimi / Qwen 兼容模式 / Ollama 均兼容此协议,换厂商只改 BaseURL 和 Model。
type OpenAICompatible struct {
	BaseURL string // 如 https://api.deepseek.com,不带 /chat/completions
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewOpenAICompatible 组装客户端。HTTP 为空时使用 120s 超时:
// LLM 推理耗时显著高于普通接口,超时上限相应放宽,但必须存在。
func NewOpenAICompatible(baseURL, apiKey, model string) *OpenAICompatible {
	return &OpenAICompatible{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// chatRequest 只声明协议中实际用到的字段,与厂商 SDK 保持独立。
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []any     `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// streamChunk SSE 流里每个 data: 帧的增量形状。
// tool_calls 以增量方式到达:首帧带 id/name,后续帧只有 arguments 的片段,
// 必须按 index 累加拼接。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// Complete 发送一次流式对话请求,增量经 onDelta 推出,聚合结果作为返回值。
// 工具清单按协议包装;无工具时不发送 tools 字段。
func (o *OpenAICompatible) Complete(ctx context.Context, msgs []Message, tools []ToolSpec, onDelta func(Delta)) (Completion, error) {
	var toolPayload []any
	for _, t := range tools {
		toolPayload = append(toolPayload, map[string]any{"type": "function", "function": t})
	}

	body, err := json.Marshal(chatRequest{Model: o.Model, Messages: msgs, Tools: toolPayload, Stream: true})
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
	if resp.StatusCode != http.StatusOK {
		// 流式出错时错误体是普通 JSON,读出来才有排查价值
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		if msg := chatErrorMessage(buf.Bytes()); msg != "" {
			return Completion{}, fmt.Errorf("agent: 模型返回错误: %s", msg)
		}
		return Completion{}, fmt.Errorf("agent: 模型 http %d: %s", resp.StatusCode, buf.String())
	}

	out := Completion{}
	callsByIndex := map[int]*ToolCall{}
	push := func(d Delta) {
		if onDelta != nil && (d.Content != "" || d.Reasoning != "") {
			onDelta(d)
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 单帧解析失败跳过,不断流
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			out.Text += delta.Content
			push(Delta{Content: delta.Content})
		}
		if delta.ReasoningContent != "" {
			push(Delta{Reasoning: delta.ReasoningContent})
		}
		for _, tc := range delta.ToolCalls {
			cur := callsByIndex[tc.Index]
			if cur == nil {
				cur = &ToolCall{ID: tc.ID, Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: tc.Function.Name}}
				callsByIndex[tc.Index] = cur
			}
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("agent: 读取模型流: %w", err)
	}

	// 按 index 升序还原调用顺序(map 无序,而调用顺序是历史回放的语义之一)
	for i := 0; i < len(callsByIndex); i++ {
		if c, ok := callsByIndex[i]; ok {
			out.Calls = append(out.Calls, *c)
		}
	}
	if len(out.Calls) == 0 && out.Text == "" {
		return out, fmt.Errorf("agent: 模型流结束但没有产出任何内容")
	}
	return out, nil
}

// chatErrorMessage 识别厂商"非 200 + {error:{message}}"的错误形态。
func chatErrorMessage(body []byte) string {
	var wrapper struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wrapper) == nil && wrapper.Error != nil {
		return fmt.Sprintf("(%s) %s", wrapper.Error.Type, wrapper.Error.Message)
	}
	return ""
}
