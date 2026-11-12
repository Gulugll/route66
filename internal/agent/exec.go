package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// exec.go —— 委托执行(前端执行型工具)的暂停-恢复机制。
//
// 与 HITL 确认(hitl.go)同构:工具 Run 不出结果,而是返回 *ExecNeeded 描述
// "要前端直连地图 MCP 执行什么";循环捕获后整轮挂起,api 层把 Pending 存会话
// 并向前端发 tool_exec 事件;前端执行完经 /agent/tool_result 回填,ResumeExec
// 以结果续跑(RunWithResult)。工具可在 RunWithResult 里链式再挂起
// (plan_route 两阶段:先取距离矩阵解序,再按序取轨迹)。
//
// 设计约束:用户的厂商 key 只存在用户设备,后端只搬运"任务描述"与"归一化结果",
// 永不经手密钥 —— 这是合规立场的实现层落点。

// ExecTask 一次前端执行任务。Params 是工具产出的执行参数(如点列表+出行方式),
// Hint 给前端展示"正在做什么"。
type ExecTask struct {
	Phase  string          `json:"phase"`  // 工具内部阶段标识,如 matrix/legs/search
	Params json.RawMessage `json:"params"` // 前端执行所需参数
	Hint   string          `json:"hint"`   // 界面提示文案
}

// ExecNeeded 委托执行工具请求前端执行时返回。
type ExecNeeded struct {
	Task ExecTask
}

func (e *ExecNeeded) Error() string {
	return fmt.Sprintf("agent: 等待前端执行(%s)", e.Task.Phase)
}

// Deferred 可委托执行工具:Run 返回 *ExecNeeded 后,由 ResumeExec 以
// 前端回传结果续跑;返回值即回填历史的最终工具结果。
// task 原样传回触发本次挂起的任务 —— 多阶段工具靠 Task.Params 跨阶段携带状态。
type Deferred interface {
	Tool
	RunWithResult(ctx context.Context, input json.RawMessage, task ExecTask, result string) (string, error)
}

// ExecSuspended Run/ResumeExec 暂停时返回的错误;调用方用类型断言取出 Pending。
type ExecSuspended struct {
	Pending Pending
}

func (e *ExecSuspended) Error() string {
	return fmt.Sprintf("agent: 等待前端执行: %s", e.Pending.Task.Phase)
}
