package amap

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

// 测试不依赖真实高德:用 httptest 在本地起一个"假高德服务器"。
// 好处:①不需要 key,②不花钱,③能精确控制返回内容,④能数请求次数验证缓存。
// 这是测试外部 API 调用的标准做法——模拟外部服务,只测我们自己的代码。

// fakeAmap 起一个假高德服务器,记录收到的请求参数,按真实格式回 JSON。
// 返回服务器 + 一个计数器(收到几次请求)。
func fakeAmap(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		respond(w, r)
	}))
	t.Cleanup(srv.Close) // 测试结束自动关服务器
	return srv, &calls
}

// fakeClient 建一个"指向假服务器"的客户端。
//
// 关键点:高德根地址是 Client 的**实例字段**,不是包级变量。
// 以前是 `var baseURL = ".../v3/distance"`,测试里改它、再用 defer 改回去——
// 这种写法只在"同包测试串行执行"的前提下成立,谁加一个 t.Parallel() 就立刻 race;
// 而且它只能替换一个接口,想测"步行接口挂了会怎样"根本没辙。
// 改成实例字段后:每个测试各拿各的客户端,互不干扰,所有接口都能被假服务器替换。
func fakeClient(srv *httptest.Server, c cache.Cache) *Client {
	return NewClientWithBase("test-key", srv.URL, c)
}

// 两点:广州塔 / 白云山。高德返回距离单位是"米",我们内部用"公里"。
var (
	gt = model.Point{Name: "广州塔", Lat: 23.1066, Lng: 113.3245}
	by = model.Point{Name: "白云山", Lat: 23.1835, Lng: 113.3042}
)

func TestDistanceMatrixRequestsAndConversion(t *testing.T) {
	// 假高德:记录收到的每个请求,按真实格式回固定距离(米)。
	// 实测高德批量语义是"多个起点 → 单个终点",这里就按这个断言。
	type req struct{ origins, destination string }
	var (
		mu  sync.Mutex             // 保护 got:并发版 drivingMatrix 会同时调 handler,
		got = map[string]req{}     // 测试代码自己也得并发安全,否则 -race 先抓的是测试
	)
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("key") != "test-key" {
			t.Errorf("key = %q, want test-key", q.Get("key"))
		}
		if q.Get("type") != "1" {
			t.Errorf("type = %q, want 1 (驾车距离)", q.Get("type"))
		}
		mu.Lock()
		got[q.Get("destination")] = req{q.Get("origins"), q.Get("destination")}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// 模拟高德响应:status=1 成功,results 与 origins 一一对应,距离单位米。
		w.Write([]byte(`{"status":"1","info":"OK","results":[
			{"distance":"12290","duration":"2454"}
		]}`))
	})

	c := fakeClient(srv, nil) // nil 缓存:这个用例只测请求与换算
	dists, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving)
	if err != nil {
		t.Fatalf("DistanceMatrix: %v", err)
	}

	// 教学点:请求格式必须和高德文档+实测一致,这里就是"契约"。
	// 逐列批量:一次请求 = 多个起点 origins + 单个终点 destination,
	// 格式是"经度,纬度"(注意经度在前,和 model.Point 字段顺序相反)。
	//
	// ⚠️ 并发改造后**不能**再按"到达顺序"(got[0]、got[1])断言:
	// 哪列先发完是调度器的事,每次运行都可能不同。
	// 改成按 destination 索引——"每个 destination 都有一列正确请求",
	// 这才是并发前后都成立的契约;而"请求顺序"从来不是我们的承诺。
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2 (每列一次)", len(got))
	}
	byOrigin := "113.304200,23.183500" // 白云山 "lng,lat"
	gtOrigin := "113.324500,23.106600" // 广州塔 "lng,lat"
	// 第 0 列:白云山 → 广州塔(destination=广州塔,origins=[白云山])
	if r, ok := got[gtOrigin]; !ok {
		t.Errorf("missing request: destination=广州塔")
	} else if r.origins != byOrigin {
		t.Errorf("dest=广州塔 origins = %q, want %q (白云山)", r.origins, byOrigin)
	}
	// 第 1 列:广州塔 → 白云山
	if r, ok := got[byOrigin]; !ok {
		t.Errorf("missing request: destination=白云山")
	} else if r.origins != gtOrigin {
		t.Errorf("dest=白云山 origins = %q, want %q (广州塔)", r.origins, gtOrigin)
	}

	// 12290 米 → 12.29 公里。转换错了测试立刻抓住。
	if got := dists[0][1]; got != 12.29 {
		t.Errorf("dists[0][1] = %v, want 12.29 (米→公里换算)", got)
	}
	if dists[0][0] != 0 {
		t.Errorf("dists[0][0] = %v, want 0 (自己到自己)", dists[0][0])
	}
}

func TestCacheHits(t *testing.T) {
	// 假高德:不管来什么请求都回同一个距离。
	srv, calls := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"1","info":"OK","results":[
			{"distance":"12290","duration":"2454"}
		]}`))
	})

	c := fakeClient(srv, cache.NewMemory(0)) // 默认 24h TTL
	pts := []model.Point{gt, by}

	// 第一次:2 行请求(每行一行),都要发出去。
	if _, err := c.DistanceMatrix(pts, model.ModeDriving); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := calls.Load()
	if first != 2 {
		t.Errorf("first call requests = %d, want 2", first)
	}

	// 第二次:同样的点,缓存全命中,一行请求都不该发。
	if _, err := c.DistanceMatrix(pts, model.ModeDriving); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := calls.Load(); got != first {
		t.Errorf("requests after cache = %d, want still %d (缓存应拦截所有请求)", got, first)
	}
}

func TestAmapFailureReturnsError(t *testing.T) {
	// 模拟高德业务失败:status=0(比如 key 配额超了),info 带原因。
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"0","info":"USER_DAILY_QUERY_OVER_LIMIT"}`))
	})

	c := fakeClient(srv, nil)
	if _, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving); err == nil {
		t.Fatal("want error on amap status=0, got nil")
	} else if !strings.Contains(err.Error(), "USER_DAILY_QUERY_OVER_LIMIT") {
		t.Errorf("error should carry amap info, got: %v", err)
	}
}

func TestResultCountMismatchReturnsError(t *testing.T) {
	// 高德返回的结果数量少于请求的对数——契约被破坏,宁可报错也不给错数据。
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"1","info":"OK","results":[]}`))
	})

	c := fakeClient(srv, nil)
	if _, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving); err == nil {
		t.Fatal("want error on result count mismatch, got nil")
	}
}

// TestSearchPlacesParsesLngLatOrder 钉住一个最容易写反的地方:
// 高德返回的 location 是 "经度,纬度"(lng 在前),而 model.Point 的字段顺序是 Lat, Lng。
// 一旦写反,搜索结果会跑到地球另一边,而且悄无声息——所以必须有测试钉住。
func TestSearchPlacesParsesLngLatOrder(t *testing.T) {
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPlaceText {
			t.Errorf("path = %q, want %q", r.URL.Path, pathPlaceText)
		}
		if got := r.URL.Query().Get("keywords"); got != "广州塔" {
			t.Errorf("keywords = %q, want 广州塔", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"1","info":"OK","pois":[
			{"name":"广州塔","address":"阅江西路222号","location":"113.324500,23.106600"}
		]}`))
	})

	c := fakeClient(srv, nil)
	places, err := c.SearchPlaces("广州塔", "", 6)
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if len(places) != 1 {
		t.Fatalf("places = %d, want 1", len(places))
	}
	if places[0].Lat != 23.1066 || places[0].Lng != 113.3245 {
		t.Errorf("经纬度解反了: got lat=%v lng=%v, want lat=23.1066 lng=113.3245",
			places[0].Lat, places[0].Lng)
	}
}

// TestRoutePolylineModeBehaviour 钉住"做不到"和"参数错了"的区别:
//   - transit:公交由步行段+公交段拼成,轨迹天然不连续 → (nil, nil),不是错误
//   - 未知 mode:必须报错。以前这里返回 (nil, nil),于是 /route?mode=cycling
//     会 200 + 空数组,前端默默画直线,把"参数拼错了"伪装成"正常返回空"
func TestRoutePolylineModeBehaviour(t *testing.T) {
	srv, calls := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"1","info":"OK","route":{"paths":[{"steps":[{"polyline":"113.3,23.1;113.31,23.11"}]}]}}`))
	})
	c := fakeClient(srv, nil)

	t.Run("transit 不报错,返回空", func(t *testing.T) {
		pts, err := c.RoutePolyline(gt, by, model.ModeTransit)
		if err != nil {
			t.Fatalf("transit 不该报错: %v", err)
		}
		if pts != nil {
			t.Errorf("transit 应返回 nil, got %v", pts)
		}
	})

	t.Run("未知 mode 必须报错,且不发请求", func(t *testing.T) {
		before := calls.Load()
		if _, err := c.RoutePolyline(gt, by, model.Mode("cycling")); err == nil {
			t.Fatal("cycling 应报错, got nil")
		}
		if calls.Load() != before {
			t.Error("未知 mode 不应该发出任何 HTTP 请求")
		}
	})

	t.Run("驾车返回轨迹点", func(t *testing.T) {
		pts, err := c.RoutePolyline(gt, by, model.ModeDriving)
		if err != nil {
			t.Fatalf("driving: %v", err)
		}
		if len(pts) != 2 {
			t.Fatalf("轨迹点数 = %d, want 2", len(pts))
		}
		// 解析出来必须是 [lng, lat]
		if pts[0][0] != 113.3 || pts[0][1] != 23.1 {
			t.Errorf("轨迹点解析错了: got %v, want [113.3 23.1]", pts[0])
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 并发版 drivingMatrix 的三块测试
//
// 并发代码的测试目标和串行不同——不光要"结果对",还要"并发行为对":
//   - 结果对:各列写进各自的格子,没有串位(并发写错位是经典 bug,
//     串行测试永远测不出来,因为串行根本不会写错位)
//   - 行为对:同时在途的请求数真的被信号量摁住了(不然限流只是写在注释里的愿望)
//   - 失败对:一列挂,整体必须报错,不能漏出半张矩阵
//
// 跑这些测试务必带 -race(go test -race ./...):
// 数据竞争在单次运行里往往不发作,靠断言抓不住,race detector 才是探针。
// ─────────────────────────────────────────────────────────────────────────────

// TestDrivingMatrixConcurrentFill 验证"各列写对位置"。
// 假高德按每个 origin 的纬度返回距离:米数 = 该起点纬度 × 1000,
// 换算成公里后 dists[i][j] 应该恰好等于 points[i].Lat——
// "从 i 出发"的距离就只该由 i 决定。results 顺序对不上 origins、
// 或者哪个 goroutine 把行号/列号写串了,这个断言立刻红。
func TestDrivingMatrixConcurrentFill(t *testing.T) {
	// 6 个纬度各不相同的点 → 6 列,每列 5 个起点。
	// 列数超过信号量容量(4)才会真正排队,串位 bug 才有机会暴露。
	pts := make([]model.Point, 6)
	for i := range pts {
		pts[i] = model.Point{Name: fmt.Sprintf("P%d", i), Lat: 20.0 + float64(i), Lng: 110.0 + float64(i)}
	}

	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 契约:第 k 个 result 对应第 k 个 origin(fetchColumn 就是按这个填行的)。
		// 所以假服务器必须**逐个 origin** 生成距离:origin 纬度 × 1000 米,
		// 换算成公里后 dists[i][j] 恰好等于 points[i].Lat。
		// 按每个 origin 各回各的值,连"origins 顺序 ↔ results 顺序"的对齐也一起钉住了。
		origins := strings.Split(r.URL.Query().Get("origins"), "|")
		results := make([]string, len(origins))
		for k, o := range origins {
			parts := strings.Split(o, ",")
			lat, err := strconv.ParseFloat(parts[1], 64) // "lng,lat",纬度在第二位
			if err != nil {
				t.Errorf("假服务器解析 origin %q 失败: %v", o, err)
			}
			results[k] = `{"distance":"` + strconv.FormatFloat(lat*1000, 'f', -1, 64) + `","duration":"100"}`
		}
		fmt.Fprintf(w, `{"status":"1","info":"OK","results":[%s]}`, strings.Join(results, ","))
	})

	c := fakeClient(srv, nil)
	dists, err := c.DistanceMatrix(pts, model.ModeDriving)
	if err != nil {
		t.Fatalf("DistanceMatrix: %v", err)
	}

	for i := range pts {
		for j := range pts {
			if i == j {
				continue
			}
			// 米→公里除了个 1000,浮点会有 1ulp 级别的误差,用容差比较。
			// 容差 1e-9 对 20~25 公里的值绰绰有余,真串位会差出几公里。
			if math.Abs(dists[i][j]-pts[i].Lat) > 1e-9 {
				t.Errorf("dists[%d][%d] = %v, want %v(第 %d 个起点的距离被写串了?)", i, j, dists[i][j], pts[i].Lat, i)
			}
		}
	}
}

// TestDrivingMatrixConcurrencyLimit 验证信号量真的在限流。
// 假服务器跟踪"同时在途请求数"的峰值:每个请求 sleep 30ms 模拟网络往返,
// 把并发窗口拉开,让超限行为有机会发生。
// 断言两个方向都卡住:
//   - peak ≤ maxColumnConcurrent:限流生效,没超发
//   - peak > 1:确实并发了——如果退化成串行,peak=1 也能过这条断言的前半段,
//     那"限流"就成了测试了个寂寞。两个断言合起来才是"限得刚刚好"。
func TestDrivingMatrixConcurrencyLimit(t *testing.T) {
	var mu sync.Mutex // 保护 inFlight/peak:handler 被多个 goroutine 并发调用
	inFlight, peak := 0, 0

	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()

		time.Sleep(30 * time.Millisecond) // 假装网络很慢,给并发留出观察窗口

		mu.Lock()
		inFlight--
		mu.Unlock()

		// results 数量必须和 origins 对齐——fetchColumn 有数量契约校验,
		// 假服务器糊弄不了它(这正是那道校验存在的意义)。
		n := len(strings.Split(r.URL.Query().Get("origins"), "|"))
		results := make([]string, n)
		for i := range results {
			results[i] = `{"distance":"1000","duration":"100"}`
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"1","info":"OK","results":[%s]}`, strings.Join(results, ","))
	})

	pts := make([]model.Point, 8) // 8 列 > maxColumnConcurrent(4),必然排队
	for i := range pts {
		pts[i] = model.Point{Name: fmt.Sprintf("P%d", i), Lat: 20.0 + float64(i), Lng: 110.0 + float64(i)}
	}

	c := fakeClient(srv, nil)
	if _, err := c.DistanceMatrix(pts, model.ModeDriving); err != nil {
		t.Fatalf("DistanceMatrix: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > maxColumnConcurrent {
		t.Errorf("同时在途峰值 = %d, 超过了信号量上限 %d——限流没生效?", peak, maxColumnConcurrent)
	}
	if peak <= 1 {
		t.Errorf("同时在途峰值 = %d, 没有发生并发(退化成串行了?)", peak)
	}
}

// TestDrivingMatrixOneBadColumnFailsAll 验证"一列挂,整体报错"。
// 白云山那一列返回 CUQPS 限流,其余列正常成功。DistanceMatrix 必须:
//  1. 返回 error(上层 matrix 靠它整体降级 haversine,
//     绝不能拿着"半张真实半张直线"的矩阵去算 TSP);
//  2. 错误里带高德原始 info(没有上下文的错误没法排查);
//  3. 错误里提示调 maxColumnConcurrent(错误信息要能指导行动);
//  4. 矩阵返回 nil(失败就别给半成品)。
func TestDrivingMatrixOneBadColumnFailsAll(t *testing.T) {
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// coord() 是 "%.6f" 格式、经度在前,和真实请求里的字符串逐字节一致。
		if r.URL.Query().Get("destination") == "113.304200,23.183500" { // 白云山列
			w.Write([]byte(`{"status":"0","info":"CUQPS_HAS_EXCEEDED_THE_LIMIT"}`))
			return
		}
		w.Write([]byte(`{"status":"1","info":"OK","results":[{"distance":"12290","duration":"2454"}]}`))
	})

	c := fakeClient(srv, nil)
	dists, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving)
	if err == nil {
		t.Fatal("一列失败必须整体报错, got nil")
	}
	if !strings.Contains(err.Error(), "CUQPS_HAS_EXCEEDED_THE_LIMIT") {
		t.Errorf("错误应携带高德原始 info, got: %v", err)
	}
	if !strings.Contains(err.Error(), "maxColumnConcurrent") {
		t.Errorf("限流类错误应提示调参, got: %v", err)
	}
	if dists != nil {
		t.Errorf("失败时不应返回半张矩阵, got %v", dists)
	}
}
