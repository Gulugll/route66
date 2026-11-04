package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"awesomeProject/internal/model"
)

// fakeSearcher 固定返回预设候选,记录每次搜索入参。
type fakeSearcher struct {
	places []model.Place
	err    error
	calls  int
}

func (f *fakeSearcher) SearchPlaces(keyword, city string, limit int) ([]model.Place, error) {
	f.calls++
	return f.places, f.err
}

// 多候选 → ConfirmError 带全部选项;用户答复后 RunConfirmed 精确回中。
func TestConfirmPlaceMultiCandidates(t *testing.T) {
	s := &fakeSearcher{places: []model.Place{
		{Name: "天坛公园", Address: "东城区天坛路甲1号", Lat: 39.8822, Lng: 116.4066},
		{Name: "天坛东门", Address: "东城区天坛东路", Lat: 39.8830, Lng: 116.4180},
	}}
	tool := &confirmPlaceTool{searcher: s}

	_, err := tool.Run(context.Background(), json.RawMessage(`{"keyword":"天坛","city":"北京"}`))
	ce, ok := err.(*ConfirmError)
	if !ok {
		t.Fatalf("多候选应返回 ConfirmError,实际 %v", err)
	}
	if len(ce.Options) != 2 || !strings.Contains(ce.Options[0], "天坛公园") {
		t.Errorf("选项应含全部候选: %v", ce.Options)
	}

	out, err := tool.RunConfirmed(context.Background(),
		json.RawMessage(`{"keyword":"天坛","city":"北京"}`), ce.Options[1])
	if err != nil {
		t.Fatalf("RunConfirmed: %v", err)
	}
	var got []place
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 1 {
		t.Fatalf("确认结果应为单元素数组: %s", out)
	}
	if got[0].Name != "天坛东门" || got[0].Lng != 116.4180 {
		t.Errorf("应命中用户选中的候选: %+v", got[0])
	}
	if s.calls != 2 {
		t.Errorf("候选不缓存,Run 与 RunConfirmed 各搜一次,实际 %d", s.calls)
	}
}

// 唯一候选直通返回,不出卡片;零候选返回引导文本。
func TestConfirmPlaceSingleAndNone(t *testing.T) {
	single := &fakeSearcher{places: []model.Place{
		{Name: "天坛公园", Address: "东城区天坛路甲1号", Lat: 39.8822, Lng: 116.4066},
	}}
	tool := &confirmPlaceTool{searcher: single}
	out, err := tool.Run(context.Background(), json.RawMessage(`{"keyword":"天坛"}`))
	if err != nil {
		t.Fatalf("唯一候选不应出确认卡片: %v", err)
	}
	if !strings.Contains(out, "天坛公园") {
		t.Errorf("应直通返回候选: %s", out)
	}

	empty := &fakeSearcher{}
	out, err = (&confirmPlaceTool{searcher: empty}).Run(context.Background(), json.RawMessage(`{"keyword":"不存在地点xyz"}`))
	if err != nil || !strings.Contains(out, "没有找到") {
		t.Errorf("零候选应返回引导文本: out=%q err=%v", out, err)
	}
}

// RunConfirmed 答复与候选对不上时明确报错(候选已变化)。
func TestConfirmPlaceStaleAnswer(t *testing.T) {
	s := &fakeSearcher{places: []model.Place{{Name: "天坛公园", Address: "甲1号", Lat: 1, Lng: 2}}}
	tool := &confirmPlaceTool{searcher: s}
	_, err := tool.RunConfirmed(context.Background(), json.RawMessage(`{"keyword":"天坛"}`), "别的地方（别的地址）")
	if err == nil || !strings.Contains(err.Error(), "候选已变化") {
		t.Fatalf("陈旧答复应报候选已变化: %v", err)
	}
}

// confirm_mode:Run 恒出卡片;RunConfirmed 接受选项原文或裸 mode 值,拒绝白名单外输入。
func TestConfirmMode(t *testing.T) {
	tool := &confirmModeTool{}
	_, err := tool.Run(context.Background(), nil)
	ce, ok := err.(*ConfirmError)
	if !ok || len(ce.Options) != 3 {
		t.Fatalf("应出三选项卡片: %v", err)
	}

	out, err := tool.RunConfirmed(context.Background(), nil, "walking 步行")
	if err != nil || !strings.Contains(out, `"walking"`) {
		t.Errorf("选项原文应可解析: out=%s err=%v", out, err)
	}
	out, err = tool.RunConfirmed(context.Background(), nil, "transit")
	if err != nil || !strings.Contains(out, `"transit"`) {
		t.Errorf("裸 mode 值应可解析: out=%s err=%v", out, err)
	}
	if _, err := tool.RunConfirmed(context.Background(), nil, "flying 飞行"); err == nil {
		t.Errorf("白名单外输入应报错")
	}
}
