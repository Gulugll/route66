package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
)

// RouteTools 将本项目的业务能力封装为 agent 工具。
// 校验、解算、降级等规则全部复用 planner.Compute 与 amap.Client,
// 与同步接口、异步 worker 走同一条解算链,不复制业务逻辑。
func RouteTools(m *matrix.Service, am *amap.Client) []Tool {
	return []Tool{
		&searchPlaceTool{am: am},
		&planRouteTool{m: m, am: am},
	}
}

// searchPlaceTool 按关键词搜索地点,返回含坐标的候选列表。
// agent 的典型流程是先搜索拿到 lat/lng,再交给规划工具。

type searchPlaceTool struct {
	am *amap.Client
}

type searchPlaceInput struct {
	Keyword string `json:"keyword"`
	City    string `json:"city"`
	Limit   int    `json:"limit"`
}

func (t *searchPlaceTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "search_place",
		Description: "按关键词搜索地点,返回候选列表(含 name/address/lat/lng 坐标)。规划路线前如果只有地名没有坐标,先用它把坐标查出来。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{"type": "string", "description": "搜索关键词,如 天坛"},
				"city":    map[string]any{"type": "string", "description": "限定城市,如 北京;不传则全国搜索"},
				"limit":   map[string]any{"type": "integer", "description": "返回条数,默认 5,最多 10"},
			},
			"required": []string{"keyword"},
		},
	}
}

func (t *searchPlaceTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in searchPlaceInput
	if err := parseInput(input, &in); err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	places, err := t.am.SearchPlaces(in.Keyword, in.City, limit)
	if err != nil {
		return "", fmt.Errorf("地点搜索失败: %w", err)
	}
	if len(places) == 0 {
		return "没有找到匹配的地点,可以换一个关键词或补上 city 再试。", nil
	}
	return marshalIndent(places), nil
}

// planRouteTool 多点路线规划,直通 planner.Compute:
// 白名单、点数上限、坐标校验、降级处理均由 Compute 负责,本层不重复校验。
// 未配置 AMAP_KEY 时 Compute 自动走 haversine,结果中的警告原样交给模型。

type planRouteTool struct {
	m  *matrix.Service
	am *amap.Client
}

type planRouteInput struct {
	Points []struct {
		Name string  `json:"name"`
		Lat  float64 `json:"lat"`
		Lng  float64 `json:"lng"`
	} `json:"points"`
	Mode   string `json:"mode"`
	Manual bool   `json:"manual"`
}

func (t *planRouteTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "plan_route",
		Description: "多点路线规划:给定一组地点(坐标,第 1 个为起点),自动求解总距离最短的访问顺序(TSP),或按给定顺序(manual=true)计算总距离。返回访问顺序、总里程和降级警告。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"points": map[string]any{
					"type":        "array",
					"description": "地点列表,第 1 个是起点。坐标先用 search_place 查,禁止编造",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
							"lat":  map[string]any{"type": "number"},
							"lng":  map[string]any{"type": "number"},
						},
						"required": []string{"name", "lat", "lng"},
					},
				},
				"mode":   map[string]any{"type": "string", "enum": []string{"driving", "walking", "transit"}, "description": "出行方式,默认 driving"},
				"manual": map[string]any{"type": "boolean", "description": "true=按给定顺序不算最短路,默认 false"},
			},
			"required": []string{"points"},
		},
	}
}

func (t *planRouteTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in planRouteInput
	if err := parseInput(input, &in); err != nil {
		return "", err
	}
	// mode 的白名单转换复用 planner.ParseMode(单一真相源)。
	mode, err := planner.ParseMode(in.Mode)
	if err != nil {
		return "", err
	}

	points := make([]model.Point, 0, len(in.Points))
	for _, p := range in.Points {
		points = append(points, model.Point{Name: p.Name, Lat: p.Lat, Lng: p.Lng})
	}

	result, err := planner.Compute(points, mode, in.Manual, nil, t.m, t.am)
	if err != nil {
		return "", err
	}
	return marshalIndent(result), nil
}
