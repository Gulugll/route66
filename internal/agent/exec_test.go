package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeDeferred 最小委托执行工具:Run 按脚本返回 ExecTask(挂起请求),
// RunWithResult 遇哨兵 chainNext 时再挂一次(链式),否则返回预设结果。
const chainNext = "\x00chain"

type fakeDeferred struct {
	spec      ToolSpec
	runs      []string // 每次挂起的 phase 记录
	responses []string // RunWithResult 的返回;chainNext 表示"再挂一次"(链式)
}

func (t *fakeDeferred) Spec() ToolSpec { return t.spec }

func (t *fakeDeferred) Run(_ context.Context, _ json.RawMessage) (string, error) {
	phase := "matrix"
	if len(t.runs) > 0 {
		phase = t.runs[len(t.runs)-1]
	}
	t.runs = append(t.runs, "next")
	return "", &ExecNeeded{Task: ExecTask{Phase: phase, Params: json.RawMessage(`{"p":1}`), Hint: "由前端直连 MCP 执行"}}
}

func (t *fakeDeferred) RunWithResult(_ context.Context, _ json.RawMessage, _ ExecTask, _ string) (string, error) {
	if len(t.responses) == 0 {
		return "", &ExecNeeded{Task: ExecTask{Phase: "legs", Params: json.RawMessage(`{"p":2}`)}}
	}
	resp := t.responses[0]
	t.responses = t.responses[1:]
	if resp == chainNext {
		return "", &ExecNeeded{Task: ExecTask{Phase: "legs", Params: json.RawMessage(`{"p":2}`)}}
	}
	return resp, nil
}

// 首次 Run 触发挂起:整轮不出结果,Pending 携带执行任务。
func TestRunDeferSuspend(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "mcp_plan", `{"mode":"driving"}`)}},
	}}
	tool := &fakeDeferred{spec: ToolSpec{Name: "mcp_plan"}}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	answer, steps, err := a.Run(context.Background(), []Message{NewUserMsg("规划")})
	var es *ExecSuspended
	if !asExecSuspended(err, &es) {
		t.Fatalf("应返回 ExecSuspended: answer=%q err=%v", answer, err)
	}
	if answer != "" {
		t.Errorf("挂起时不应有答案: %q", answer)
	}
	if len(steps) != 1 {
		t.Fatalf("挂起轮应记入 steps,实际 %d", len(steps))
	}
	if es.Pending.Kind != "exec" {
		t.Errorf("Pending.Kind 应为 exec,实际 %q", es.Pending.Kind)
	}
	if es.Pending.Task.Phase != "matrix" || es.Pending.Call.ID != "call-1" {
		t.Errorf("Pending 任务/调用不对: %+v", es.Pending)
	}
	if !strings.Contains(es.Pending.Task.Hint, "前端") {
		t.Errorf("Hint 应传给前端: %q", es.Pending.Task.Hint)
	}
}

// 回填结果续跑:结果回填历史,模型据此给最终答案。
func TestResumeExecCompletes(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "mcp_plan", `{}`)}},
		{Text: "方案已生成"},
	}}
	tool := &fakeDeferred{
		spec:      ToolSpec{Name: "mcp_plan"},
		responses: []string{`{"total_km":42.5}`},
	}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	_, steps, err := a.Run(context.Background(), []Message{NewUserMsg("规划")})
	var es *ExecSuspended
	if !asExecSuspended(err, &es) {
		t.Fatalf("首次应挂起: %v", err)
	}

	answer, steps2, err := a.ResumeExec(context.Background(), es.Pending, `{"total_km":42.5}`)
	if err != nil {
		t.Fatalf("ResumeExec: %v", err)
	}
	if answer != "方案已生成" {
		t.Errorf("最终答案不对: %q", answer)
	}
	// 挂起轮 + 续跑轮
	if len(steps2) != 2 {
		t.Fatalf("应有 2 轮,实际 %d", len(steps2))
	}
	// 续跑轮的历史里必须有回填的 tool 消息
	last := model.seen[len(model.seen)-1]
	found := false
	for _, m := range last {
		if m.Role == RoleTool && m.ToolCallID == "call-1" && strings.Contains(*m.Content, "42.5") {
			found = true
		}
	}
	if !found {
		t.Error("回填结果必须进历史")
	}
	_ = steps
}

// 链式执行:第一次回填后工具再挂一次(两阶段 plan_route),二次回填后才出结果。
func TestResumeExecChained(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "mcp_plan", `{}`)}},
		{Text: "完成"},
	}}
	tool := &fakeDeferred{
		spec:      ToolSpec{Name: "mcp_plan"},
		responses: []string{chainNext, `{"legs":1}`}, // 第一次回填 → 再挂;第二次 → 出结果
	}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	_, _, err := a.Run(context.Background(), []Message{NewUserMsg("规划")})
	var es1 *ExecSuspended
	if !asExecSuspended(err, &es1) {
		t.Fatalf("首次应挂起: %v", err)
	}

	_, _, err = a.ResumeExec(context.Background(), es1.Pending, `matrix-result`)
	var es2 *ExecSuspended
	if !asExecSuspended(err, &es2) {
		t.Fatalf("链式应再挂起: %v", err)
	}
	if es2.Pending.Task.Phase != "legs" {
		t.Errorf("第二阶段应为 legs,实际 %q", es2.Pending.Task.Phase)
	}
	if es2.Pending.Kind != "exec" || es2.Pending.Call.ID != "call-1" {
		t.Errorf("链式 Pending 应延续同一调用: %+v", es2.Pending)
	}
	// 链式挂起的历史仍停在 assistant(calls):第一阶段结果被工具内部消化,
	// 最终结果(阶段 2 出参)才是回填给模型的那条 tool 消息。
	for _, m := range es2.Pending.History {
		if m.Role == RoleTool && m.ToolCallID == "call-1" {
			t.Error("链式挂起时不应已有 tool 结果消息")
		}
	}

	answer, _, err := a.ResumeExec(context.Background(), es2.Pending, `legs-result`)
	if err != nil || answer != "完成" {
		t.Fatalf("二次回填后应完成: answer=%q err=%v", answer, err)
	}
	if tool.runs[len(tool.runs)-1] != "next" {
		t.Errorf("工具调用次数不符: %v", tool.runs)
	}
}

func asExecSuspended(err error, target **ExecSuspended) bool {
	if err == nil {
		return false
	}
	es, ok := err.(*ExecSuspended)
	if !ok {
		return false
	}
	*target = es
	return true
}
