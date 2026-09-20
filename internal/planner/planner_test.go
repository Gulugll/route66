package planner

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
)

// 本文件测试的是"直线预矩阵解序 + 沿最终顺序逐边查真路"这条新路径
// (specs/2026-09-17-prematrix-sa.md)。核心手法是**请求计数**:
// 假高德不但要能应答,还要统计请求次数——
// "建全矩阵"和"只查 n-1 条边"在结果上可能一样,
// 只有请求次数能区分两种实现路径。

// 四个北京点,从天安门一路往北:坐标拉开,直线距离(约 1km 级)
// 和假高德的固定 2km 明显不同,断言才能区分 total 来自真路还是直线兜底。
var (
	ptQianmen  = model.Point{Name: "前门", Lat: 39.8994, Lng: 116.3986}
	ptGugong   = model.Point{Name: "故宫", Lat: 39.9163, Lng: 116.3972}
	ptJingshan = model.Point{Name: "景山", Lat: 39.9254, Lng: 116.3966}
	ptNanluo   = model.Point{Name: "南锣鼓巷", Lat: 39.9372, Lng: 116.4031}
)

func fourPoints() []model.Point {
	return []model.Point{ptQianmen, ptGugong, ptJingshan, ptNanluo}
}

// amapCoord 复刻 amap 包 coord() 的坐标格式("lng,lat" 各 6 位小数)。
// 故意不复用内部函数:测试要钉住的是线上请求里真实出现的字符串——
// 格式变更时这里第一时间失败,而不是跟随实现漂移(与"路径写字面量"同理)。
func amapCoord(p model.Point) string {
	return strconv.FormatFloat(p.Lng, 'f', 6, 64) + "," + strconv.FormatFloat(p.Lat, 'f', 6, 64)
}

// fakeWalkingServer 假的高德步行 direction 接口:每对固定回 2000 米。
// failDest 非空时,destination 命中该坐标的请求返回 status=0
// (精确弄坏"某一条边",模拟部分失败)。
// 返回请求计数器 —— 这才是主角。
func fakeWalkingServer(t *testing.T, failDest string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if failDest != "" && r.URL.Query().Get("destination") == failDest {
			w.Write([]byte(`{"status":"0","info":"USER_DAILY_QUERY_OVER_LIMIT"}`))
			return
		}
		w.Write([]byte(`{"status":"1","info":"OK","route":{"paths":[{"distance":"2000"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// isPerm0 断言 order 是 [0..n-1] 的排列且从 0 开始(起点不变量)。
func isPerm0(order []int) bool {
	if len(order) == 0 || order[0] != 0 {
		return false
	}
	sorted := append([]int(nil), order...)
	slices.Sort(sorted)
	for i, v := range sorted {
		if v != i {
			return false
		}
	}
	return true
}

// TestWalkingManualQueriesOnlyAdjacentEdges:手动模式不重排顺序,
// 现状却在建全量矩阵(4 点 = 12 次请求)只为求 3 条边的和 —— 纯浪费。
// 改后应只发 3 次:每条相邻边一次。
func TestWalkingManualQueriesOnlyAdjacentEdges(t *testing.T) {
	srv, hits := fakeWalkingServer(t, "")
	am := amap.NewClientWithBase("test-key", srv.URL, nil)

	res, err := Compute(fourPoints(), model.ModeWalking, true, nil, matrix.New(), am)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("manual 只该查 n-1=3 条边, 实际 %d 次请求(建全矩阵会是 12 次)", got)
	}
	if res.TotalKm != 6.0 { // 3 条边 × 固定 2km
		t.Errorf("total_km = %v, want 6(全部来自真实路网)", res.TotalKm)
	}
	if res.IsDegraded {
		t.Errorf("不该降级, warnings=%v", res.Warnings)
	}
	if want := []int{0, 1, 2, 3}; !slices.Equal(res.OrderIdx, want) {
		t.Errorf("manual 顺序 = %v, want %v", res.OrderIdx, want)
	}
}

// TestWalkingAutoSAQueriesOnlyAdjacentEdges:自动模式用直线预矩阵跑 SA,
// 顺序定了才逐边查真路 —— 解序阶段 0 API,查路 3 次。
func TestWalkingAutoSAQueriesOnlyAdjacentEdges(t *testing.T) {
	srv, hits := fakeWalkingServer(t, "")
	am := amap.NewClientWithBase("test-key", srv.URL, nil)

	res, err := Compute(fourPoints(), model.ModeWalking, false, nil, matrix.New(), am)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("自动模式应只查最终顺序的 n-1=3 条边, 实际 %d 次", got)
	}
	if !isPerm0(res.OrderIdx) {
		t.Errorf("order_idx = %v, 应为 0 开头的全排列", res.OrderIdx)
	}
	if res.TotalKm != 6.0 {
		t.Errorf("total_km = %v, want 6(任何顺序都是 3 条边 × 2km)", res.TotalKm)
	}
}

// TestWalkingEdgeDegradation:只弄坏中间那条边(故宫→景山)。
// 前后两条边仍是真实路网,中间退直线 —— 降级必须是"路段级"的,
// 和混合出行分支同一套表现:degraded 有路段名,is_degraded=true。
func TestWalkingEdgeDegradation(t *testing.T) {
	srv, _ := fakeWalkingServer(t, amapCoord(ptJingshan)) // dest=景山 的请求全失败
	am := amap.NewClientWithBase("test-key", srv.URL, nil)

	res, err := Compute(fourPoints(), model.ModeWalking, true, nil, matrix.New(), am)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	want := 2.0 + 2.0 + matrix.Haversine(ptGugong, ptJingshan)
	if diff := res.TotalKm - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_km = %v, want %v(两条真实 + 中间直线兜底)", res.TotalKm, want)
	}
	if wantSeg := []string{"故宫→景山"}; !slices.Equal(res.Degraded, wantSeg) {
		t.Errorf("degraded = %v, want %v", res.Degraded, wantSeg)
	}
	if !res.IsDegraded || len(res.Warnings) == 0 {
		t.Errorf("降级必须可见: is_degraded=%v warnings=%v", res.IsDegraded, res.Warnings)
	}
}

// TestWalkingNoKeyIsNotDegraded:没配 key 时直线就是"本来该用的算法",
// 不报警告 —— 和驾车路径的既有约定保持一致。
func TestWalkingNoKeyIsNotDegraded(t *testing.T) {
	res, err := Compute(fourPoints(), model.ModeWalking, true, nil, matrix.New(), nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if res.IsDegraded {
		t.Errorf("没配 key 不算降级, warnings=%v", res.Warnings)
	}
	want := matrix.Haversine(ptQianmen, ptGugong) +
		matrix.Haversine(ptGugong, ptJingshan) +
		matrix.Haversine(ptJingshan, ptNanluo)
	if diff := res.TotalKm - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_km = %v, want %v(全直线)", res.TotalKm, want)
	}
}
