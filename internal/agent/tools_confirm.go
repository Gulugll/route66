package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// tools_confirm.go —— 确认类工具(HITL):第一次 Run 出确认卡片请求,
// 用户答复后经 RunConfirmed 拿到真实结果。与普通工具的区别就是"多问一句"。

// confirmPlaceTool 地名确认:唯一候选直通返回(不打扰),多候选出卡片让人挑。
// 消歧是草图 HITL 的第一站 —— AI 标记地点前必须锁定唯一坐标。

type confirmPlaceTool struct {
	searcher placeSearcher
}

type confirmPlaceInput struct {
	Keyword string `json:"keyword"`
	City    string `json:"city"`
}

func (t *confirmPlaceTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "confirm_place",
		Description: "确认一个地点并返回其坐标。会搜索关键词:唯一匹配时直接返回;有多个候选时会向用户展示选项,用户选择后返回选中的坐标。凡是把地名变成'要标记的地点'都应该用它,而不是 search_place。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{"type": "string", "description": "地点关键词,如 天坛"},
				"city":    map[string]any{"type": "string", "description": "限定城市,如 北京;建议尽量传以减少歧义"},
			},
			"required": []string{"keyword"},
		},
	}
}

func (t *confirmPlaceTool) optionText(p place) string {
	return p.Name + "（" + p.Address + "）"
}

type place struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

func (t *confirmPlaceTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	places, err := t.search(input)
	if err != nil {
		return "", err
	}
	switch {
	case len(places) == 0:
		return "没有找到匹配的地点,请换一个关键词或补上 city 再试。", nil
	case len(places) == 1:
		return marshalIndent(places), nil
	}
	opts := make([]string, 0, len(places))
	for _, p := range places {
		opts = append(opts, t.optionText(p))
	}
	return "", &ConfirmError{
		Prompt:  "「" + keywordOf(input) + "」有 " + fmt.Sprint(len(places)) + " 个匹配,请选择正确的地点",
		Options: opts,
	}
}

// RunConfirmed:用户答复 = 卡片选项原文(“名称（地址）”)。候选列表不跨请求缓存,
// 这里用答复原文重新搜索并按选项文本精确匹配 —— 搜索来源免费且幂等,不值得为此加会话存储。
func (t *confirmPlaceTool) RunConfirmed(ctx context.Context, input json.RawMessage, answer string) (string, error) {
	places, err := t.search(input)
	if err != nil {
		return "", err
	}
	for _, p := range places {
		if t.optionText(p) == answer {
			return marshalIndent([]place{p}), nil
		}
	}
	return "", fmt.Errorf("候选已变化,未匹配到 %q,请重新确认", answer)
}

func (t *confirmPlaceTool) search(input json.RawMessage) ([]place, error) {
	var in confirmPlaceInput
	if err := parseInput(input, &in); err != nil {
		return nil, err
	}
	if t.searcher == nil {
		return nil, fmt.Errorf("地点搜索失败: 搜索来源未装配(需要 SEARCH_SOURCE 配 photon 或 nominatim)")
	}
	places, err := t.searcher.SearchPlaces(in.Keyword, in.City, 5)
	if err != nil {
		return nil, fmt.Errorf("地点搜索失败: %w", err)
	}
	out := make([]place, 0, len(places))
	for _, p := range places {
		out = append(out, place{Name: p.Name, Address: p.Address, Lat: p.Lat, Lng: p.Lng})
	}
	return out, nil
}

// keywordOf 从原始入参里抠出关键词,只用于确认文案展示。
func keywordOf(input json.RawMessage) string {
	var in confirmPlaceInput
	_ = json.Unmarshal(input, &in)
	return in.Keyword
}

// confirmModeTool 出行方式确认:总是出卡片让用户拍板(草图第二站 HITL)。
// 方式是"这段路怎么走"的产品决策,不替用户做主。

type confirmModeTool struct{}

func (t *confirmModeTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "confirm_mode",
		Description: "向用户询问出行方式(驾车/步行/公交)。规划路线前如果用户没有明确说明方式,必须先用它询问,不要自己猜。",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	}
}

func (t *confirmModeTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "", &ConfirmError{
		Prompt:  "这段行程用什么出行方式?",
		Options: []string{"driving 驾车", "walking 步行", "transit 公交"},
	}
}

// RunConfirmed:答复形如 "driving 驾车"(卡片选项原文)或直接 "driving"。
func (t *confirmModeTool) RunConfirmed(_ context.Context, _ json.RawMessage, answer string) (string, error) {
	mode := strings.Fields(strings.TrimSpace(answer))
	if len(mode) == 0 {
		return "", fmt.Errorf("未选择出行方式")
	}
	switch mode[0] {
	case "driving", "walking", "transit":
		return marshalIndent(map[string]string{"mode": mode[0]}), nil
	}
	return "", fmt.Errorf("无效的出行方式 %q,可选 driving/walking/transit", answer)
}
