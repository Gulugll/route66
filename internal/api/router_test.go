package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
)

// TestMain 把 gin 调成测试模式:关掉启动时的路由清单和 debug 警告,
// 让 `go test` 的输出只剩下测试自己的信息(否则一屏全是 [GIN-debug])。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// fakeAmapServer 起一个"假高德",按路径分发。
//
// failPaths 里列出的路径改成回 status=0(模拟配额超限/key 失效),
// 这是本项目最重要的一个测试能力:只有能单独弄坏某一个接口,
// 才测得出"降级了有没有告诉用户"。
func fakeAmapServer(t *testing.T, failPaths ...string) *httptest.Server {
	t.Helper()
	fail := make(map[string]bool, len(failPaths))
	for _, p := range failPaths {
		fail[p] = true
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fail[r.URL.Path] {
			w.Write([]byte(`{"status":"0","info":"USER_DAILY_QUERY_OVER_LIMIT"}`))
			return
		}
		switch r.URL.Path {
		// 这些路径写成字面量,而不是引用 amap 包里的常量:
		// 测试要钉住的是"线上真实发出去的路径"。如果引用常量,
		// 哪天有人把常量改错了,测试会跟着一起错、一起通过——那就白测了。
		case "/v3/distance":
			// 驾车批量:results 必须与 origins 一一对应,少一个客户端就会报错。
			n := len(strings.Split(r.URL.Query().Get("origins"), "|"))
			items := make([]string, n)
			for i := range items {
				items[i] = `{"distance":"10000","duration":"600"}`
			}
			w.Write([]byte(`{"status":"1","info":"OK","results":[` + strings.Join(items, ",") + `]}`))
		case "/v3/direction/driving":
			w.Write([]byte(`{"status":"1","info":"OK","route":{"paths":[{"distance":"12000"}]}}`))
		case "/v5/direction/walking":
			w.Write([]byte(`{"status":"1","info":"OK","route":{"paths":[{"distance":"3000"}]}}`))
		case "/v3/direction/walking":
			// 只有 RoutePolyline(步行)走这条,需要 steps[].polyline
			w.Write([]byte(`{"status":"1","info":"OK","route":{"paths":[{"steps":[{"polyline":"116.4,39.91;116.41,39.92"}]}]}}`))
		case "/v3/direction/transit/integrated":
			w.Write([]byte(`{"status":"1","info":"OK","route":{"transits":[{"distance":"8000"}]}}`))
		case "/v3/place/text":
			w.Write([]byte(`{"status":"1","info":"OK","pois":[]}`))
		default:
			t.Errorf("假高德收到未预期的路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestRouter 组装一个"后端 + 假高德"的完整环境。
// withAmap=false 用来模拟"没配 AMAP_KEY"的部署形态。
func newTestRouter(t *testing.T, withAmap bool, failPaths ...string) *gin.Engine {
	t.Helper()
	if !withAmap {
		return NewRouter(matrix.New(), nil, nil, nil) // 没有高德客户端 → 纯 haversine;不装配异步任务
	}
	srv := fakeAmapServer(t, failPaths...)
	// nil 缓存:测试之间不共享缓存,避免互相"喂"出假的命中
	am := amap.NewClientWithBase("test-key", srv.URL, nil)
	return NewRouter(matrix.NewWithAmap(am), am, nil, nil)
}

// doJSON 发一个请求,返回状态码和原始 body。
// cookie 是可选的(变参):传了就带上 —— API 保护启用时,newAuthedTestRouter
// 返回的合法 cookie 让业务请求正常通过;不传就是"游客请求"。
func doJSON(t *testing.T, r *gin.Engine, method, target, body string, cookie ...string) (int, string) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if len(cookie) > 0 && cookie[0] != "" {
		req.Header.Set("Cookie", cookie[0])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// planRespJSON 是**独立于** api.planResp 重写的一份解码结构。
// 刻意不直接复用 planResp:测试要对齐的是"线上真实发出去的 JSON 字段名"
// (wire contract),而不是内部类型。复用内部类型的话,哪天有人给字段改个
// json tag,测试会跟着一起改、一起通过——那就白测了。
type planRespJSON struct {
	Order      []string `json:"order"`
	OrderIdx   []int    `json:"order_idx"`
	TotalKm    float64  `json:"total_km"`
	Warnings   []string `json:"warnings"`
	Degraded   []string `json:"degraded"`
	IsDegraded bool     `json:"is_degraded"`
}

func decodePlan(t *testing.T, body string) planRespJSON {
	t.Helper()
	var out planRespJSON
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\nbody=%s", err, body)
	}
	return out
}

// 北京三个点(项目的默认城市)。坐标之间几公里量级,haversine 断言好算。
var (
	pointTiananmen = model.Point{Name: "天安门", Lat: 39.9087, Lng: 116.3975}
	pointGugong    = model.Point{Name: "故宫", Lat: 39.9163, Lng: 116.3972}
	pointTiantan   = model.Point{Name: "天坛", Lat: 39.8822, Lng: 116.4066}
)

// threePointsBody 返回一个三点请求体。manual=true 让顺序固定为 [0,1,2],
// 总距离于是可以精确预测——测试降级逻辑时这点很关键,
// 否则 TSP 自己会挑顺序,断言就只能写得很松。
func threePointsBody(extra string) string {
	return `{"origin":{"name":"天安门","lat":39.9087,"lng":116.3975},
	         "destinations":[{"name":"故宫","lat":39.9163,"lng":116.3972},
	                         {"name":"天坛","lat":39.8822,"lng":116.4066}],
	         "manual":true` + extra + `}`
}

// ---------- 参数校验:边界必须拦得住 ----------

// TestPlanRejectsInvalidMode 是本文件里最重要的一个用例。
// 它钉住的就是那个"实测确认过"的 bug:以前 mode=cycling 不会报错,
// 而是被 matrix 层吞掉降级成直线距离,返回 200 + is_degraded=false。
// 调用方拼错了参数,却拿到一个看起来完全正常的结果。
func TestPlanRejectsInvalidMode(t *testing.T) {
	r := newTestRouter(t, true)
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(`, "mode":"cycling"`))
	if code != http.StatusBadRequest {
		t.Fatalf("非法 mode 应返回 400, got %d body=%s", code, body)
	}
	if !strings.Contains(body, "driving/walking/transit") {
		t.Errorf("错误信息应告诉调用方有哪些合法值, got %s", body)
	}
}

func TestPlanRejectsBadCoordinate(t *testing.T) {
	r := newTestRouter(t, true)
	code, _ := doJSON(t, r, http.MethodPost, "/plan",
		`{"origin":{"name":"a","lat":999,"lng":116.4},"destinations":[{"name":"b","lat":39.9,"lng":116.4}]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("非法坐标应返回 400, got %d", code)
	}
}

func TestPlanRejectsSegmentLengthMismatch(t *testing.T) {
	r := newTestRouter(t, true)
	// 三个点 = 两段,只给一段 → 400
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(`, "segments":["walking"]`))
	if code != http.StatusBadRequest {
		t.Fatalf("segments 长度不符应返回 400, got %d body=%s", code, body)
	}
}

// TestPlanPointLimitDependsOnMode 验证"上限按最坏路径算":
// 同样是 12 个点,驾车(批量接口)要放行,步行(逐对接口)必须拦下。
// 只测一边是不够的——把上限一律设成 10 也能让"步行被拦"通过,
// 但那样就把驾车的能力白白砍掉了。
func TestPlanPointLimitDependsOnMode(t *testing.T) {
	coords := make([]string, 12)
	for i := range coords {
		coords[i] = `{"name":"p","lat":39.9,"lng":116.4}`
	}
	body := `{"origin":{"name":"p0","lat":39.91,"lng":116.40},"destinations":[` +
		strings.Join(coords, ",") + `],"manual":true`

	t.Run("12 点步行 → 400(逐对接口扛不住)", func(t *testing.T) {
		r := newTestRouter(t, true)
		code, resp := doJSON(t, r, http.MethodPost, "/plan", body+`, "mode":"walking"}`)
		if code != http.StatusBadRequest {
			t.Fatalf("应返回 400, got %d body=%s", code, resp)
		}
		if !strings.Contains(resp, "10") {
			t.Errorf("错误信息应说明上限是 10, got %s", resp)
		}
	})

	t.Run("12 点驾车 → 200(批量接口放行)", func(t *testing.T) {
		r := newTestRouter(t, true)
		code, resp := doJSON(t, r, http.MethodPost, "/plan", body+`, "mode":"driving"}`)
		if code != http.StatusOK {
			t.Fatalf("应返回 200, got %d body=%s", code, resp)
		}
	})
}

// ---------- 正常路径 ----------

func TestPlanDrivingHappyPath(t *testing.T) {
	r := newTestRouter(t, true)
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(""))
	if code != http.StatusOK {
		t.Fatalf("got %d body=%s", code, body)
	}
	got := decodePlan(t, body)

	// manual=true → 顺序就是列表顺序
	if want := []int{0, 1, 2}; !equalInts(got.OrderIdx, want) {
		t.Errorf("order_idx = %v, want %v", got.OrderIdx, want)
	}
	if want := []string{"天安门", "故宫", "天坛"}; !equalStrings(got.Order, want) {
		t.Errorf("order = %v, want %v", got.Order, want)
	}
	// 两段 × 10 km。假高德每段都回 10000 米。
	if got.TotalKm != 20 {
		t.Errorf("total_km = %v, want 20", got.TotalKm)
	}
	if got.IsDegraded {
		t.Errorf("不该降级, warnings=%v", got.Warnings)
	}
}

// TestPlanTSPKeepsOriginFirst 钉住一个不变量:做 TSP 优化时起点不能变。
// 算法只反转"中间段"(2-opt 的 i 从 0 开始,反转的是 i+1..j),
// 所以第 0 个点永远留在原位——用户指定的起点不该被算法挪走。
func TestPlanTSPKeepsOriginFirst(t *testing.T) {
	r := newTestRouter(t, true)
	// manual=false → 跑 TSP
	code, body := doJSON(t, r, http.MethodPost, "/plan",
		`{"origin":{"name":"天安门","lat":39.9087,"lng":116.3975},
		  "destinations":[{"name":"故宫","lat":39.9163,"lng":116.3972},
		                  {"name":"天坛","lat":39.8822,"lng":116.4066}],
		  "mode":"driving"}`)
	if code != http.StatusOK {
		t.Fatalf("got %d body=%s", code, body)
	}
	got := decodePlan(t, body)
	if len(got.OrderIdx) != 3 || got.OrderIdx[0] != 0 {
		t.Fatalf("起点应保持在原位: order_idx=%v", got.OrderIdx)
	}
	if !isPermutation(got.OrderIdx) {
		t.Fatalf("order_idx 必须是 0..n-1 的一个排列: %v", got.OrderIdx)
	}
	// 假高德每段都回 10 km → 任何顺序都是 20 km
	if got.TotalKm != 20 {
		t.Errorf("total_km = %v, want 20", got.TotalKm)
	}
}

// ---------- 降级必须被看见:这一组是本次修复的核心 ----------

// TestPlanReportsMatrixDegradation:高德挂了,矩阵整体降级成直线。
// 修复前:200 + is_degraded=false,用户以为拿到的是真实路网距离。
// 修复后:200 + is_degraded=true + warnings 说明这是估算。
func TestPlanReportsMatrixDegradation(t *testing.T) {
	r := newTestRouter(t, true, "/v3/distance") // 只弄坏驾车批量距离接口
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(""))
	if code != http.StatusOK {
		t.Fatalf("降级后仍应返回 200, got %d body=%s", code, body)
	}
	got := decodePlan(t, body)

	if !got.IsDegraded {
		t.Error("降级了就必须 is_degraded=true —— 这正是修复前漏掉的地方")
	}
	if len(got.Warnings) == 0 {
		t.Error("应至少有一条警告说明数字是估算的")
	}
	// 总距离应等于两段 haversine(顺序被 manual=true 固定,可精确预测)
	want := matrix.Haversine(pointTiananmen, pointGugong) + matrix.Haversine(pointGugong, pointTiantan)
	if diff := got.TotalKm - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_km = %v, want %v(haversine 直线)", got.TotalKm, want)
	}
}

// TestPlanNoAmapKeyIsNotDegraded:没配 key 时用的是 haversine,
// 但这**不是降级**——它是"本来就这么设计"。不该给用户报警告。
// "设计如此"和"出故障了"必须分开,否则告警会被噪音淹掉。
func TestPlanNoAmapKeyIsNotDegraded(t *testing.T) {
	r := newTestRouter(t, false) // 没有高德客户端
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(""))
	if code != http.StatusOK {
		t.Fatalf("got %d body=%s", code, body)
	}
	got := decodePlan(t, body)
	if got.IsDegraded {
		t.Errorf("没配 key 不算降级, warnings=%v", got.Warnings)
	}
	want := matrix.Haversine(pointTiananmen, pointGugong) + matrix.Haversine(pointGugong, pointTiantan)
	if diff := got.TotalKm - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_km = %v, want %v", got.TotalKm, want)
	}
}

// TestPlanMixedSegmentDegradesOnlyThatSegment:混合出行只坏第一段(步行),
// 第二段(驾车)必须照常走真实路网,并且降级信息里只出现第一段。
func TestPlanMixedSegmentDegradesOnlyThatSegment(t *testing.T) {
	r := newTestRouter(t, true, "/v5/direction/walking") // 只弄坏步行接口
	code, body := doJSON(t, r, http.MethodPost, "/plan",
		threePointsBody(`, "segments":["walking","driving"]`))
	if code != http.StatusOK {
		t.Fatalf("got %d body=%s", code, body)
	}
	got := decodePlan(t, body)

	if !got.IsDegraded {
		t.Error("第一段降级了,is_degraded 应为 true")
	}
	if len(got.Degraded) != 1 || got.Degraded[0] != "天安门→故宫" {
		t.Errorf("degraded = %v, want [天安门→故宫](只该有坏掉的那一段)", got.Degraded)
	}
	// 第一段 haversine + 第二段真实驾车 12 km
	want := matrix.Haversine(pointTiananmen, pointGugong) + 12
	if diff := got.TotalKm - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_km = %v, want %v", got.TotalKm, want)
	}
	// 混合出行不做 TSP,顺序必须是用户给的
	if want := []int{0, 1, 2}; !equalInts(got.OrderIdx, want) {
		t.Errorf("order_idx = %v, want %v", got.OrderIdx, want)
	}
}

// ---------- /route ----------

func TestRouteRejectsInvalidMode(t *testing.T) {
	r := newTestRouter(t, true)
	code, body := doJSON(t, r, http.MethodGet,
		"/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=cycling", "")
	if code != http.StatusBadRequest {
		t.Fatalf("/route 的非法 mode 应返回 400, got %d body=%s", code, body)
	}
}

// TestRouteTransitReturnsEmptyPolyline:transit 没有连续轨迹是**业务事实**,
// 不是错误 → 200 + 空数组,前端据此画直线。
func TestRouteTransitReturnsEmptyPolyline(t *testing.T) {
	r := newTestRouter(t, true)
	code, body := doJSON(t, r, http.MethodGet,
		"/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=transit", "")
	if code != http.StatusOK {
		t.Fatalf("got %d body=%s", code, body)
	}
	if !strings.Contains(body, `"polyline":[]`) {
		t.Errorf("transit 应返回空数组而不是 null, got %s", body)
	}
}

// TestRouteParamErrorBeatsDependencyError 钉住校验顺序:
// 先看看"调用方是不是写错了",再看"服务端能不能干活"。
// 没配 key 时,非法 mode 仍应 400 而不是 503 —— 否则调用方会以为
// "改成合法参数就能用",其实服务端根本没配 key,排查得多绕一圈。
func TestRouteParamErrorBeatsDependencyError(t *testing.T) {
	r := newTestRouter(t, false) // 没有高德客户端

	code, body := doJSON(t, r, http.MethodGet,
		"/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=cycling", "")
	if code != http.StatusBadRequest {
		t.Fatalf("参数错应优先 400, got %d body=%s", code, body)
	}

	code, body = doJSON(t, r, http.MethodGet,
		"/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=driving", "")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("参数合法但没配 key 应 503, got %d body=%s", code, body)
	}
}

// ---------- 小工具 ----------

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isPermutation 检查是不是 0..n-1 的一个排列:
// order_idx 是"机器用的下标",只要有重复或越界,前端就会画错线。
func isPermutation(idx []int) bool {
	seen := make(map[int]bool, len(idx))
	for _, v := range idx {
		if v < 0 || v >= len(idx) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
