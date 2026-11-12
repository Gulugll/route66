package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
	"awesomeProject/internal/solver"
)

// tools_mcp.go —— 用户绑定地图 MCP 后的 agent 工具集。
// 与 tools_plan.go 同名工具(plan_route/search_place):模型看到的 spec 一致,
// 行为不同 —— 不在本地解算,而是产出 ExecTask 委托前端直连厂商 MCP 执行。
// key 只在前端,后端只搬运任务描述与归一化结果(合规立场的实现落点)。
//
// 状态机依赖 ExecTask.Params 跨阶段携带状态(解序结果等):
// Pending.Task 随会话持久,ResumeExec 时原样传回,工具无隐藏状态。

// McpLeg 一段真实路网轨迹(前端已归一化为 WGS-84,经度在前)。
type McpLeg struct {
	From        string       `json:"from"`
	To          string       `json:"to"`
	Km          float64      `json:"km"`
	DurationMin float64      `json:"duration_min,omitempty"`
	Polyline    [][2]float64 `json:"polyline"` // 空数组 = 该段降级无轨迹
}

// McpPlanResult MCP 模式的规划产出:planner.Result 全字段 + 逐段轨迹。
// 前端拿 legs 直接画线,不再逐段调 /route(那是 Valhalla 的数据来源)。
type McpPlanResult struct {
	planner.Result
	Legs []McpLeg `json:"legs"`
}

// —— mcp_search ——

type mcpSearchTool struct{}

func (t *mcpSearchTool) Spec() ToolSpec {
	// 与 searchPlaceTool 同 spec:模型对两种模式的调用方式无需区分
	return (&searchPlaceTool{}).Spec()
}

func (t *mcpSearchTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in searchPlaceInput
	if err := parseInput(input, &in); err != nil {
		return "", err
	}
	if in.Keyword == "" {
		return "", fmt.Errorf("keyword 必填")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	params, _ := json.Marshal(map[string]any{"keyword": in.Keyword, "city": in.City, "limit": limit})
	return "", &ExecNeeded{Task: ExecTask{
		Phase:  "search",
		Params: params,
		Hint:   "正在通过绑定的地图服务检索地点…",
	}}
}

func (t *mcpSearchTool) RunWithResult(_ context.Context, _ json.RawMessage, _ ExecTask, result string) (string, error) {
	var places []model.Place
	if err := json.Unmarshal([]byte(result), &places); err != nil {
		return "", fmt.Errorf("地点检索结果格式无效: %w", err)
	}
	if len(places) == 0 {
		return "没有找到匹配的地点,可以换一个关键词或补上 city 再试。", nil
	}
	return marshalIndent(places), nil
}

// seqOrder 生成 [0,1,...,n-1](列表顺序;planner 包内同名函数未导出,此处独立一份)
func seqOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}

// —— mcp_plan ——

// matrixResult 前端回传的距离矩阵(km,三角矩阵/全矩阵均可)。
type matrixResult struct {
	Matrix   [][]float64 `json:"matrix"`
	Degraded bool        `json:"degraded"`
	Warnings []string    `json:"warnings"`
}

// legsResult 前端回传的逐段轨迹(按解序后的相邻点顺序)。
type legsResult struct {
	Legs []struct {
		Km          float64      `json:"km"`
		DurationMin float64      `json:"duration_min"`
		Polyline    [][2]float64 `json:"polyline"`
		Warnings    []string     `json:"warnings"`
	} `json:"legs"`
}

type mcpPlanTool struct{}

func (t *mcpPlanTool) Spec() ToolSpec {
	return (&planRouteTool{}).Spec()
}

func (t *mcpPlanTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in planRouteInput
	if err := parseInput(input, &in); err != nil {
		return "", err
	}
	mode, err := planner.ParseMode(in.Mode)
	if err != nil {
		return "", err
	}
	points := make([]model.Point, 0, len(in.Points))
	for _, p := range in.Points {
		points = append(points, model.Point{Name: p.Name, Lat: p.Lat, Lng: p.Lng})
	}
	if err := planner.Validate(points, mode, nil); err != nil {
		return "", err
	}

	if in.Manual || mode == model.ModeTransit {
		// 无需解序:列表顺序直接进轨迹阶段(总距离由逐段 km 求和)
		return "", t.legsTask(points, seqOrder(len(points)), mode, false, nil)
	}
	params, _ := json.Marshal(map[string]any{"points": points, "mode": mode})
	return "", &ExecNeeded{Task: ExecTask{
		Phase:  "matrix",
		Params: params,
		Hint:   "正在通过绑定的地图服务计算路网距离矩阵…",
	}}
}

// RunWithResult 按任务阶段处理前端回填:matrix → 解序并链式挂起 legs;legs → 组装结果。
func (t *mcpPlanTool) RunWithResult(_ context.Context, input json.RawMessage, task ExecTask, result string) (string, error) {
	switch task.Phase {
	case "matrix":
		return t.onMatrix(input, task, result)
	case "legs":
		return t.onLegs(task, result)
	default:
		return "", fmt.Errorf("未知的执行阶段: %q", task.Phase)
	}
}

func (t *mcpPlanTool) onMatrix(input json.RawMessage, _ ExecTask, result string) (string, error) {
	var in planRouteInput
	if err := parseInput(input, &in); err != nil {
		return "", err
	}
	mode, err := planner.ParseMode(in.Mode)
	if err != nil {
		return "", err
	}
	points := make([]model.Point, 0, len(in.Points))
	for _, p := range in.Points {
		points = append(points, model.Point{Name: p.Name, Lat: p.Lat, Lng: p.Lng})
	}

	var mr matrixResult
	if err := json.Unmarshal([]byte(result), &mr); err != nil {
		return "", fmt.Errorf("距离矩阵结果格式无效: %w", err)
	}
	n := len(points)
	if len(mr.Matrix) != n {
		return "", fmt.Errorf("矩阵维度不符: 需要 %d,实际 %d", n, len(mr.Matrix))
	}

	// 解序复用同一求解器(单一真相源,不在前端复制 TSP)
	order := solver.SimulatedAnnealing(mr.Matrix, 0)
	ordered := make([]model.Point, 0, n)
	for _, idx := range order {
		ordered = append(ordered, points[idx])
	}
	return "", t.legsTask(ordered, order, mode, mr.Degraded, mr.Warnings)
}

func (t *mcpPlanTool) legsTask(ordered []model.Point, order []int, mode model.Mode, degraded bool, warnings []string) error {
	params, err := json.Marshal(map[string]any{
		"points": ordered, "mode": mode, "order_idx": order,
		"matrix_degraded": degraded, "matrix_warnings": warnings,
	})
	if err != nil {
		return err
	}
	return &ExecNeeded{Task: ExecTask{
		Phase:  "legs",
		Params: params,
		Hint:   "正在获取逐段路网轨迹…",
	}}
}

func (t *mcpPlanTool) onLegs(task ExecTask, result string) (string, error) {
	var st struct {
		Points         []model.Point `json:"points"`
		Mode           model.Mode    `json:"mode"`
		OrderIdx       []int         `json:"order_idx"`
		MatrixDegraded bool          `json:"matrix_degraded"`
		MatrixWarnings []string      `json:"matrix_warnings"`
	}
	if err := json.Unmarshal(task.Params, &st); err != nil {
		return "", fmt.Errorf("阶段状态损坏: %w", err)
	}
	var lr legsResult
	if err := json.Unmarshal([]byte(result), &lr); err != nil {
		return "", fmt.Errorf("轨迹结果格式无效: %w", err)
	}
	if len(lr.Legs) != len(st.Points)-1 {
		return "", fmt.Errorf("轨迹段数不符: 需要 %d,实际 %d", len(st.Points)-1, len(lr.Legs))
	}

	var (
		res      McpPlanResult
		warnings = append([]string(nil), st.MatrixWarnings...)
	)
	res.OrderIdx = st.OrderIdx
	res.Order = make([]string, len(st.OrderIdx))
	for i, idx := range st.OrderIdx {
		res.Order[i] = st.Points[idx].Name
	}
	for i, leg := range lr.Legs {
		res.TotalKm += leg.Km
		res.Legs = append(res.Legs, McpLeg{
			From:        st.Points[i].Name,
			To:          st.Points[i+1].Name,
			Km:          leg.Km,
			DurationMin: leg.DurationMin,
			Polyline:    leg.Polyline,
		})
		warnings = append(warnings, leg.Warnings...)
		if len(leg.Warnings) > 0 {
			res.Degraded = append(res.Degraded, st.Points[i].Name+"→"+st.Points[i+1].Name)
		}
	}
	if st.MatrixDegraded {
		warnings = append(warnings, "路网距离获取失败,本次总距离含直线估算,仅供参考")
	}
	res.Warnings = warnings
	res.IsDegraded = len(warnings) > 0
	return marshalIndent(res), nil
}
