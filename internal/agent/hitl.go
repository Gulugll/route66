package agent

// hitl.go —— 人机协同(HITL)的暂停-恢复机制。
//
// 语义:确认类工具的第一次 Run 不出结果,而是返回 *ConfirmError 描述"要问用户什么";
// 循环捕获后整轮挂起,由调用方(api 层)把 Pending 存会话并向用户出确认卡片;
// 用户答复后经 Resume 以答复续跑工具(RunConfirmed),拿到真实结果回填历史继续循环。
// 无歧义场景工具应直通返回,不打扰用户 —— 确认只发生在真正需要人拍板的地方。

import (
	"context"
	"encoding/json"
	"fmt"
)

// ConfirmError 确认类工具请求用户输入时返回。
type ConfirmError struct {
	Prompt  string   // 卡片上的问题
	Options []string // 候选项;为空表示自由文本输入
}

func (e *ConfirmError) Error() string { return "需要用户确认: " + e.Prompt }

// Confirmable 可确认工具:Run 返回 *ConfirmError 后,由 Resume 以用户答复续跑。
type Confirmable interface {
	Tool
	RunConfirmed(ctx context.Context, input json.RawMessage, answer string) (string, error)
}

// Pending 一次暂停(HITL 确认或前端委托执行)的完整状态,调用方保存后凭它恢复循环。
type Pending struct {
	Kind    string    `json:"kind,omitempty"` // "confirm"|"exec";空串视为 confirm(兼容旧会话)
	Call    ToolCall  // 触发确认/执行的调用
	Prompt  string    // confirm:卡片问题
	Options []string  // confirm:候选项
	Task    *ExecTask // exec:前端执行任务
	History []Message // 已含 assistant(calls),缺该轮全部 tool 结果
	Steps   []Step    // 暂停前的过程记录,恢复后 steps 接着累加
}

// ConfirmNeeded Run 暂停时返回的错误;调用方用类型断言取出 Pending。
type ConfirmNeeded struct {
	Pending Pending
}

func (e *ConfirmNeeded) Error() string {
	return fmt.Sprintf("agent: 等待用户确认: %s", e.Pending.Prompt)
}
