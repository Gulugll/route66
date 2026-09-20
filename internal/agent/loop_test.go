package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeModel 按脚本逐轮返回预设 Completion,并记录每次收到的消息,
// 用于验证循环回填的历史是否正确。
type fakeModel struct {
	script []Completion // 第 i 次调用返回 script[i]
	calls  int
	seen   [][]Message // 每次调用收到的消息快照
}

func (f *fakeModel) Complete(_ context.Context, msgs []Message, _ []ToolSpec) (Completion, error) {
	f.seen = append(f.seen, append([]Message(nil), msgs...))
	if f.calls >= len(f.script) {
		return Completion{}, nil
	}
	c := f.script[f.calls]
	f.calls++
	return c, nil
}

// echoTool 记录入参并返回固定内容。
// 循环会并发调用工具,inputs 的写入必须加锁。
type echoTool struct {
	mu     sync.Mutex
	spec   ToolSpec
	inputs []string
	resp   string
}

func (t *echoTool) Spec() ToolSpec { return t.spec }
func (t *echoTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	// 与真实工具一致:先反序列化参数,非法参数返回错误。
	var v map[string]any
	if err := json.Unmarshal(input, &v); err != nil {
		return "", fmt.Errorf("工具参数不是合法 JSON: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputs = append(t.inputs, string(input))
	return t.resp, nil
}

func toolCall(id, name, args string) ToolCall {
	c := ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// 两轮标准流程:第 1 轮调用工具,第 2 轮给出最终答案。
func TestRunToolCallThenAnswer(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "search_place", `{"keyword":"天坛"}`)}},
		{Text: "天坛在北京市东城区。"},
	}}
	tool := &echoTool{spec: ToolSpec{Name: "search_place"}, resp: `[{"name":"天坛公园"}]`}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	answer, steps, err := a.Run(context.Background(), []Message{NewUserMsg("天坛在哪")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "天坛在北京市东城区。" {
		t.Errorf("最终答案不对: %q", answer)
	}
	if len(steps) != 2 {
		t.Fatalf("应有 2 轮,实际 %d", len(steps))
	}
	if len(tool.inputs) != 1 || tool.inputs[0] != `{"keyword":"天坛"}` {
		t.Errorf("工具入参不对: %v", tool.inputs)
	}

	// 第 2 次调用时,历史中必须包含 assistant 的调用记录与 role=tool 的结果。
	last := model.seen[1]
	var foundAssistant, foundTool bool
	for _, m := range last {
		if m.Role == RoleAssistant && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "call-1" {
			foundAssistant = true
		}
		if m.Role == RoleTool && m.ToolCallID == "call-1" && strings.Contains(*m.Content, "天坛公园") {
			foundTool = true
		}
	}
	if !foundAssistant || !foundTool {
		t.Errorf("历史消息不完整: assistant调用=%v 工具结果=%v", foundAssistant, foundTool)
	}
}

// 未知工具与非法参数都应以错误文本回填历史,而不是中断循环。
func TestRunToolErrorsFeedBack(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{
			toolCall("call-1", "no_such_tool", `{}`),       // 不存在的工具
			toolCall("call-2", "search_place", `bad json`), // 非法参数
		}},
		{Text: "好的,搜索出了点问题。"},
	}}
	tool := &echoTool{spec: ToolSpec{Name: "search_place"}, resp: "unused"}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	answer, steps, err := a.Run(context.Background(), []Message{NewUserMsg("hi")})
	if err != nil || answer != "好的,搜索出了点问题。" {
		t.Fatalf("工具报错不该中断循环: answer=%q err=%v", answer, err)
	}
	results := steps[0].Results
	if !strings.Contains(results[0], "不存在名为") {
		t.Errorf("未知工具应返回错误文本: %q", results[0])
	}
	if !strings.Contains(results[1], "不是合法 JSON") {
		t.Errorf("坏参数应返回错误文本: %q", results[1])
	}
	// 非法参数应在反序列化阶段失败,不触达工具本体。
	if len(tool.inputs) != 0 {
		t.Errorf("坏参数不应执行到工具本体: %v", tool.inputs)
	}
}

// 模型持续请求工具时触发 MaxIterations 上限。
func TestRunMaxIterations(t *testing.T) {
	call := toolCall("call-x", "search_place", `{}`)
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{call}}, {Calls: []ToolCall{call}}, {Calls: []ToolCall{call}},
	}}
	tool := &echoTool{spec: ToolSpec{Name: "search_place"}, resp: "ok"}
	a := &Agent{Model: model, Tools: []Tool{tool}, MaxIterations: 3}

	_, steps, err := a.Run(context.Background(), []Message{NewUserMsg("hi")})
	if err == nil || !strings.Contains(err.Error(), "最大迭代") {
		t.Fatalf("应报最大迭代错误: %v", err)
	}
	if len(steps) != 3 {
		t.Errorf("超限前应有 3 轮记录,实际 %d", len(steps))
	}
}

// 单轮内多个工具调用应全部执行,且结果与调用按下标一一对应。
func TestRunParallelToolCalls(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{
			toolCall("call-1", "search_place", `{"keyword":"天安门"}`),
			toolCall("call-2", "search_place", `{"keyword":"天坛"}`),
			toolCall("call-3", "search_place", `{"keyword":"故宫"}`),
		}},
		{Text: "都查到了"},
	}}
	tool := &echoTool{spec: ToolSpec{Name: "search_place"}, resp: "place"}
	a := &Agent{Model: model, Tools: []Tool{tool}}

	_, steps, err := a.Run(context.Background(), []Message{NewUserMsg("hi")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(steps[0].Results) != 3 {
		t.Fatalf("3 个调用应有 3 条结果,实际 %d", len(steps[0].Results))
	}
	last := model.seen[1]
	toolMsgs := 0
	for _, m := range last {
		if m.Role == RoleTool {
			toolMsgs++
		}
	}
	if toolMsgs != 3 {
		t.Errorf("应有 3 条 role=tool 消息,实际 %d", toolMsgs)
	}
}
