package amap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

// 测试不依赖真实高德:用 httptest 在本地起一个"假高德服务器"。
// 好处:①不需要 key,②不花钱,③能精确控制返回内容,④能数请求次数验证缓存。
// 这是测试外部 API 调用的标准姿势——把"外部世界"模拟出来,测我们的代码。

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

// 两点:广州塔 / 白云山。高德返回距离单位是"米",我们内部用"公里"。
var (
	gt = model.Point{Name: "广州塔", Lat: 23.1066, Lng: 113.3245}
	by = model.Point{Name: "白云山", Lat: 23.1835, Lng: 113.3042}
)

func TestDistanceMatrixRequestsAndConversion(t *testing.T) {
	// 假高德:记录收到的每个请求,按真实格式回固定距离(米)。
	// 实测高德批量语义是"多个起点 → 单个终点",这里就按这个断言。
	type req struct{ origins, destination string }
	var got []req
	srv, _ := fakeAmap(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("key") != "test-key" {
			t.Errorf("key = %q, want test-key", q.Get("key"))
		}
		if q.Get("type") != "1" {
			t.Errorf("type = %q, want 1 (驾车距离)", q.Get("type"))
		}
		got = append(got, req{q.Get("origins"), q.Get("destination")})
		w.Header().Set("Content-Type", "application/json")
		// 模拟高德响应:status=1 成功,results 与 origins 一一对应,距离单位米。
		w.Write([]byte(`{"status":"1","info":"OK","results":[
			{"distance":"12290","duration":"2454"}
		]}`))
	})
	baseURL = srv.URL // 把客户端指向假服务器
	defer func() { baseURL = "https://restapi.amap.com/v3/distance" }()

	c := NewClient("test-key", nil) // nil 缓存:这个用例只测请求与换算
	dists, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving)
	if err != nil {
		t.Fatalf("DistanceMatrix: %v", err)
	}

	// 教学点:请求格式必须和高德文档+实测一致,这里就是"契约"。
	// 逐列批量:一次请求 = 多个起点 origins + 单个终点 destination,
	// 格式是"经度,纬度"(注意经度在前,和 model.Point 字段顺序相反)。
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2 (每列一次)", len(got))
	}
	// 第 0 列:所有其他点(白云山) → 广州塔
	if want := "113.304200,23.183500"; got[0].origins != want {
		t.Errorf("req[0] origins = %q, want %q (白云山)", got[0].origins, want)
	}
	if want := "113.324500,23.106600"; got[0].destination != want {
		t.Errorf("req[0] destination = %q, want %q (广州塔)", got[0].destination, want)
	}
	// 第 1 列反过来:广州塔 → 白云山
	if want := "113.324500,23.106600"; got[1].origins != want {
		t.Errorf("req[1] origins = %q, want %q (广州塔)", got[1].origins, want)
	}
	if want := "113.304200,23.183500"; got[1].destination != want {
		t.Errorf("req[1] destination = %q, want %q (白云山)", got[1].destination, want)
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
	baseURL = srv.URL
	defer func() { baseURL = "https://restapi.amap.com/v3/distance" }()

	c := NewClient("test-key", cache.NewMemory(0)) // 默认 24h TTL
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
	baseURL = srv.URL
	defer func() { baseURL = "https://restapi.amap.com/v3/distance" }()

	c := NewClient("test-key", nil)
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
	baseURL = srv.URL
	defer func() { baseURL = "https://restapi.amap.com/v3/distance" }()

	c := NewClient("test-key", nil)
	if _, err := c.DistanceMatrix([]model.Point{gt, by}, model.ModeDriving); err == nil {
		t.Fatal("want error on result count mismatch, got nil")
	}
}
