package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// confirmStub 可确认的假工具:Run 恒返回确认请求,RunConfirmed 返回用户答复的加工结果。
type confirmStub struct {
	spec    ToolSpec
	prompt  string
	options []string
	answers []string // 记录 RunConfirmed 收到的答复
}

func (t *confirmStub) Spec() ToolSpec { return t.spec }
func (t *confirmStub) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "", &ConfirmError{Prompt: t.prompt, Options: t.options}
}
func (t *confirmStub) RunConfirmed(_ context.Context, input json.RawMessage, answer string) (string, error) {
	t.answers = append(t.answers, answer)
	return "确认结果:" + answer, nil
}

// 暂停-恢复主流程:首轮触发确认 → Run 返回 ConfirmNeeded → Resume 带答复续跑至出答案。
func TestRunConfirmPauseAndResume(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "ask_mode", `{"q":"去哪"}`)}},
		{Text: "好的,已按驾车规划。"},
	}}
	tool := &confirmStub{spec: ToolSpec{Name: "ask_mode"}, prompt: "选出行方式", options: []string{"驾车", "步行"}}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	answer, steps, err := a.Run(context.Background(), []Message{NewUserMsg("帮我规划")})
	var cn *ConfirmNeeded
	if err == nil {
		t.Fatalf("首次 Run 应返回 ConfirmNeeded,实际 answer=%q err=nil", answer)
	}
	if !asConfirmNeeded(err, &cn) {
		t.Fatalf("错误应为 *ConfirmNeeded,实际 %T: %v", err, err)
	}
	if cn.Pending.Prompt != "选出行方式" || len(cn.Pending.Options) != 2 {
		t.Errorf("Pending 应携带卡片内容: prompt=%q options=%v", cn.Pending.Prompt, cn.Pending.Options)
	}
	if cn.Pending.Call.ID != "call-1" {
		t.Errorf("Pending 应携带待确认调用: %+v", cn.Pending.Call)
	}
	// 暂停时普通文本答案不该有
	if answer != "" {
		t.Errorf("暂停时不应有最终答案,实际 %q", answer)
	}

	// Resume:用户答复"驾车" → 工具真正执行 → 模型第 2 轮出答案
	answer, steps, err = a.Resume(context.Background(), cn.Pending, "驾车")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if answer != "好的,已按驾车规划。" {
		t.Errorf("Resume 后答案不对: %q", answer)
	}
	if len(tool.answers) != 1 || tool.answers[0] != "驾车" {
		t.Errorf("RunConfirmed 应收到用户答复: %v", tool.answers)
	}
	if len(steps) < 2 {
		t.Fatalf("Resume 应包含确认轮+答案轮,实际 %d 轮", len(steps))
	}

	// 恢复后模型看到的 history 必须含 role=tool 的确认结果(OpenAI 协议要求对账)
	last := model.seen[len(model.seen)-1]
	var foundTool bool
	for _, m := range last {
		if m.Role == RoleTool && m.ToolCallID == "call-1" && strings.Contains(*m.Content, "确认结果:驾车") {
			foundTool = true
		}
	}
	if !foundTool {
		t.Errorf("恢复后历史缺少工具确认结果: %+v", last)
	}
}

// 链式确认:第一次 Resume 又触发新的确认,第二次 Resume 才出答案。
func TestResumeChainedConfirm(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "ask_mode", `{}`)}},
		{Calls: []ToolCall{toolCall("call-2", "ask_mode", `{}`)}},
		{Text: "都确认完了"},
	}}
	tool := &confirmStub{spec: ToolSpec{Name: "ask_mode"}, prompt: "确认", options: []string{"是", "否"}}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	_, _, err := a.Run(context.Background(), []Message{NewUserMsg("hi")})
	var cn *ConfirmNeeded
	if !asConfirmNeeded(err, &cn) {
		t.Fatalf("首次应 ConfirmNeeded: %v", err)
	}
	_, _, err = a.Resume(context.Background(), cn.Pending, "第一次答复")
	if !asConfirmNeeded(err, &cn) {
		t.Fatalf("第一次 Resume 应再次 ConfirmNeeded(链式): %v", err)
	}
	answer, _, err := a.Resume(context.Background(), cn.Pending, "第二次答复")
	if err != nil || answer != "都确认完了" {
		t.Fatalf("第二次 Resume 应完成: answer=%q err=%v", answer, err)
	}
}

// 普通工具报错不被误判为确认请求;ConfirmError 之外的原生错误仍走错误文本回填。
func TestRunNormalToolErrorNotConfirm(t *testing.T) {
	model := &fakeModel{script: []Completion{
		{Calls: []ToolCall{toolCall("call-1", "boom", `{}`)}},
		{Text: "工具坏了,我知道了"},
	}}
	tool := &echoTool{spec: ToolSpec{Name: "boom"}, resp: "unused"}
	a := &Agent{Model: model, Tools: []Tool{tool}, SystemPrompt: "test"}

	answer, _, err := a.Run(context.Background(), []Message{NewUserMsg("hi")})
	if err != nil {
		t.Fatalf("普通工具错误应回填历史而不是中断: %v", err)
	}
	if answer != "工具坏了,我知道了" {
		t.Errorf("答案不对: %q", answer)
	}
}

// asConfirmNeeded 类型断言辅助(测试与 api 层都要用)。
func asConfirmNeeded(err error, out **ConfirmNeeded) bool {
	cn, ok := err.(*ConfirmNeeded)
	if ok {
		*out = cn
	}
	return ok
}
