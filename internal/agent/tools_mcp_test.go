package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// tools_mcp_test.go —— MCP 模式工具(前端委托执行)的状态机测试。
// 直调工具层:Run/RunWithResult 返回 *ExecNeeded(请求前端执行);
// 循环层(loop.go)才把它包装成 ExecSuspended,包装逻辑在 exec_test.go 已覆盖。

const threePoints = `{"points":[{"name":"故宫","lat":39.917,"lng":116.397},
	{"name":"天坛","lat":39.882,"lng":116.410},
	{"name":"天安门","lat":39.908,"lng":116.397}],"manual":true}`

// mustTask 断言返回的是前端执行请求,并取出任务。
func mustTask(t *testing.T, err error) *ExecTask {
	t.Helper()
	var en *ExecNeeded
	if !errors.As(err, &en) {
		t.Fatalf("应返回前端执行请求: %v", err)
	}
	return &en.Task
}

// —— mcp_search ——

func TestMcpSearchSuspendAndPassthrough(t *testing.T) {
	tool := &mcpSearchTool{}
	task := mustTask(t, func() error {
		_, err := tool.Run(context.Background(), json.RawMessage(`{"keyword":"天坛","city":"北京","limit":3}`))
		return err
	}())
	if task.Phase != "search" {
		t.Errorf("phase 应为 search,实际 %q", task.Phase)
	}
	if !strings.Contains(string(task.Params), "天坛") {
		t.Errorf("执行参数应含关键词: %s", task.Params)
	}

	out, err := tool.RunWithResult(context.Background(),
		json.RawMessage(`{"keyword":"天坛"}`), *task, `[{"name":"天坛公园","lat":39.88,"lng":116.41}]`)
	if err != nil {
		t.Fatalf("RunWithResult: %v", err)
	}
	if !strings.Contains(out, "天坛公园") {
		t.Errorf("结果应透传给模型: %s", out)
	}

	// 前端回传非 JSON:报错而非毒化历史
	if _, err := tool.RunWithResult(context.Background(), json.RawMessage(`{}`), *task, `not-json`); err == nil {
		t.Error("非 JSON 回填应报错")
	}
}

func TestMcpSearchBadInput(t *testing.T) {
	tool := &mcpSearchTool{}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("缺 keyword 应报参数错误,不应挂起")
	}
}

// —— mcp_plan ——

// manual=true 不解序:直接进 legs 阶段,顺序=列表顺序,距离由逐段返回
func TestMcpPlanManualSkipsMatrix(t *testing.T) {
	tool := &mcpPlanTool{}
	task := mustTask(t, func() error {
		_, err := tool.Run(context.Background(), json.RawMessage(threePoints))
		return err
	}())
	if task.Phase != "legs" {
		t.Errorf("manual 应直接进 legs 阶段,实际 %q", task.Phase)
	}
	if !strings.Contains(string(task.Params), `"order_idx":[0,1,2]`) {
		t.Errorf("legs 阶段参数应携带列表顺序: %s", task.Params)
	}
}

func TestMcpPlanMatrixPhaseThenOrder(t *testing.T) {
	tool := &mcpPlanTool{}
	input := strings.Replace(threePoints, `"manual":true`, `"manual":false`, 1)
	task := mustTask(t, func() error {
		_, err := tool.Run(context.Background(), json.RawMessage(input))
		return err
	}())
	if task.Phase != "matrix" {
		t.Errorf("非 manual 应先进 matrix 阶段,实际 %q", task.Phase)
	}

	// 矩阵回填 → 解序 → 链式挂起进 legs 阶段,参数携带解出的顺序
	// 构造矩阵:最优顺序 0→2→1(0-2 近,2-1 近,0-1 远)
	matrix := `{"matrix":[[0,10,2],[10,0,8],[2,8,0]],"degraded":false,"warnings":[]}`
	next := mustTask(t, func() error {
		_, err := tool.RunWithResult(context.Background(), json.RawMessage(input), *task, matrix)
		return err
	}())
	if next.Phase != "legs" {
		t.Fatalf("矩阵回填后应进入 legs 阶段: got %q", next.Phase)
	}
	if !strings.Contains(string(next.Params), `"order_idx":[0,2,1]`) {
		t.Errorf("legs 参数应携带 TSP 解出的顺序 [0,2,1]: %s", next.Params)
	}
}

func TestMcpPlanAssembleLegs(t *testing.T) {
	tool := &mcpPlanTool{}
	task := mustTask(t, func() error {
		_, err := tool.Run(context.Background(), json.RawMessage(threePoints))
		return err
	}())

	legs := `{"legs":[
		{"km":1.5,"duration_min":6,"polyline":[[116.397,39.917],[116.398,39.91]]},
		{"km":2.5,"duration_min":10,"polyline":[[116.398,39.91],[116.410,39.882]],"warnings":["该段路线获取失败,已降级直线距离"]}
	],"degraded":true}`
	out, err := tool.RunWithResult(context.Background(),
		json.RawMessage(string(task.Params)), *task, legs)
	if err != nil {
		t.Fatalf("RunWithResult(legs): %v", err)
	}

	var res struct {
		Order      []string `json:"order"`
		OrderIdx   []int    `json:"order_idx"`
		TotalKm    float64  `json:"total_km"`
		Warnings   []string `json:"warnings"`
		IsDegraded bool     `json:"is_degraded"`
		Legs       []McpLeg `json:"legs"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("结果不是合法 JSON: %v\n%s", err, out)
	}
	if len(res.Order) != 3 || res.Order[0] != "故宫" || res.Order[1] != "天坛" || res.Order[2] != "天安门" {
		t.Errorf("order 不对: %v", res.Order)
	}
	if res.TotalKm != 4.0 {
		t.Errorf("总里程应为 4.0,实际 %v", res.TotalKm)
	}
	if !res.IsDegraded || len(res.Warnings) == 0 {
		t.Errorf("降级警告应汇总: %+v", res)
	}
	if len(res.Legs) != 2 || len(res.Legs[0].Polyline) != 2 {
		t.Errorf("legs 轨迹应原样保留: %+v", res.Legs)
	}
	if res.Legs[1].From != "天坛" || res.Legs[1].To != "天安门" {
		t.Errorf("leg 起终点应按列表顺序: %+v", res.Legs)
	}
}

func TestMcpPlanBadInput(t *testing.T) {
	tool := &mcpPlanTool{}
	// 少于 2 点:参数错误,不挂起
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"points":[{"name":"a","lat":1,"lng":1}],"manual":true}`)); err == nil {
		t.Error("少于 2 点应报参数错误")
	}
	// 非法 mode
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"points":[{"name":"a","lat":1,"lng":1},{"name":"b","lat":2,"lng":2}],"mode":"flying","manual":true}`)); err == nil {
		t.Error("非法 mode 应报参数错误")
	}
}

// transit 无矩阵:直接列表顺序 + 逐段轨迹
func TestMcpPlanTransitSkipsMatrix(t *testing.T) {
	tool := &mcpPlanTool{}
	input := strings.Replace(threePoints, `"manual":true`, `"mode":"transit","manual":false`, 1)
	task := mustTask(t, func() error {
		_, err := tool.Run(context.Background(), json.RawMessage(input))
		return err
	}())
	if task.Phase != "legs" {
		t.Errorf("transit 应直接进 legs 阶段,实际 %q", task.Phase)
	}
}
