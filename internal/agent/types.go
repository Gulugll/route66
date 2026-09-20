// Package agent 实现一个手写的最小 ReAct 智能体。
//
// 核心循环只有四步:调用模型 → 执行模型请求的工具 → 结果回填历史 → 循环,
// 直到模型返回纯文本即最终答案。手写一遍是为了看清控制流全貌;
// 后续如引入 Eino 等框架,此处可作为对照实现保留。
package agent

import "encoding/json"

// Role 消息角色,对应 OpenAI Chat 协议的四种。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 一条对话消息。
// Content 用指针是为了区分 null 和空串:assistant 发起工具调用时,
// 协议要求 content 为 null,部分服务端对两者的处理不一致。
type Message struct {
	Role       Role       `json:"role"`
	Content    *string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // 仅 role=tool:结果对应的调用 ID
}

// ToolCall 模型发起的一次工具调用。
// Arguments 保持原始 JSON 字符串,反序列化和校验由各工具的 Run 负责。
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // 固定 "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolSpec 工具描述:模型完全依据这份说明决定何时用工具、如何填参。
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// Completion 模型的一次应答:文本与工具调用可同时存在。
type Completion struct {
	Text  string
	Calls []ToolCall
}

// —— 消息构造辅助 ——

func NewSystemMsg(s string) Message { return Message{Role: RoleSystem, Content: &s} }
func NewUserMsg(s string) Message   { return Message{Role: RoleUser, Content: &s} }

// NewToolMsg 包装工具执行结果;ToolCallID 必须回填,服务端靠它对账。
func NewToolMsg(callID, result string) Message {
	return Message{Role: RoleTool, Content: &result, ToolCallID: callID}
}

func NewAssistantMsg(text string, calls []ToolCall) Message {
	return Message{Role: RoleAssistant, Content: &text, ToolCalls: calls}
}

// marshalIndent 用于调试输出与工具结果序列化。
func marshalIndent(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
