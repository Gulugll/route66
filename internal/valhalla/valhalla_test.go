package valhalla

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

// fakeValhalla 起一个本地假 Valhalla:按路径分流,记录收到的请求体,
// 由各测试注入响应。路径只有矩阵(/sources_to_targets)和轨迹(/route)两个。
type fakeValhalla struct {
	srv      *httptest.Server
	bodies   []string // 依次记录收到的请求体
	matrixFn func(body string) string
	routeFn  func(body string) string
}

func newFake(t *testing.T, matrixFn, routeFn func(body string) string) *fakeValhalla {
	t.Helper()
	f := &fakeValhalla{matrixFn: matrixFn, routeFn: routeFn}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sources_to_targets":
			io.WriteString(w, f.matrixFn(string(body)))
		case "/route":
			io.WriteString(w, f.routeFn(string(body)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func pts(n int) []model.Point {
	out := make([]model.Point, n)
	for i := range out {
		out[i] = model.Point{Name: "p", Lat: float64(30 + i), Lng: float64(120 + i)}
	}
	return out
}

func TestDistanceMatrix(t *testing.T) {
	calls := 0
	f := newFake(t,
		func(body string) string {
			calls++
			var req struct {
				Sources []struct{ Lat float64 } `json:"sources"`
				Targets []struct{ Lat float64 } `json:"targets"`
				Costing string                  `json:"costing"`
			}
			if err := json.Unmarshal([]byte(body), &req); err != nil {
				t.Fatalf("请求体不是合法 JSON: %v", err)
			}
			if req.Costing != "auto" {
				t.Errorf("驾车应映射 costing=auto, got %q", req.Costing)
			}
			if len(req.Sources) != 2 || len(req.Targets) != 2 {
				t.Errorf("2 个点应各出 2 个 source/target, got %d/%d", len(req.Sources), len(req.Targets))
			}
			// 2×2 小矩阵即可,客户端只需按 [i][j] 取数
			return `{"sources_to_targets":[[{"distance":1.5,"time":2},{"distance":3,"time":4}],[{"distance":5,"time":6},{"distance":7,"time":8}]],"units":"km"}`
		},
		nil)
	// 客户端拼的是 n×n,但 fake 只回 2×2——这里传 2 个点对齐
	c := NewClientWithBase(f.srv.URL, nil)
	got, err := c.DistanceMatrix(pts(2), model.ModeDriving)
	if err != nil {
		t.Fatalf("DistanceMatrix: %v", err)
	}
	if got[0][1] != 3 || got[1][0] != 5 || got[1][1] != 7 {
		t.Errorf("矩阵取数错位: %v", got)
	}
	if calls != 1 {
		t.Errorf("整张矩阵应只发 1 次请求, got %d", calls)
	}
}

func TestDistanceMatrixWalkingCosting(t *testing.T) {
	f := newFake(t,
		func(body string) string {
			var req struct {
				Costing string `json:"costing"`
			}
			json.Unmarshal([]byte(body), &req)
			if req.Costing != "pedestrian" {
				t.Errorf("步行应映射 costing=pedestrian, got %q", req.Costing)
			}
			return `{"sources_to_targets":[[{"distance":1,"time":1},{"distance":2,"time":2}],[{"distance":3,"time":3},{"distance":4,"time":4}]],"units":"km"}`
		},
		nil)
	c := NewClientWithBase(f.srv.URL, nil)
	if _, err := c.DistanceMatrix(pts(2), model.ModeWalking); err != nil {
		t.Fatalf("DistanceMatrix walking: %v", err)
	}
}

func TestDistanceMatrixTransitUnsupported(t *testing.T) {
	f := newFake(t, func(string) string {
		t.Error("公交不应发出任何请求")
		return `{}`
	}, nil)
	c := NewClientWithBase(f.srv.URL, nil)
	if _, err := c.DistanceMatrix(pts(2), model.ModeTransit); err == nil {
		t.Fatal("transit 应返回错误,让上层降级 haversine")
	}
}

func TestDistanceMatrixUpstreamError(t *testing.T) {
	f := newFake(t, func(string) string {
		return `{"error_code":2000,"error":"No suitable edges near location"}`
	}, nil)
	c := NewClientWithBase(f.srv.URL, nil)
	if _, err := c.DistanceMatrix(pts(2), model.ModeDriving); err == nil {
		t.Fatal("上游 error_code 应转成 error(触发上层降级)")
	}
}

func TestRoutePolyline(t *testing.T) {
	f := newFake(t, nil,
		func(body string) string {
			var req struct {
				Locations   []struct{ Lat float64 } `json:"locations"`
				Costing     string                  `json:"costing"`
				ShapeFormat string                  `json:"shape_format"`
			}
			json.Unmarshal([]byte(body), &req)
			if req.Costing != "auto" {
				t.Errorf("costing = %q, want auto", req.Costing)
			}
			if req.ShapeFormat != "geojson" {
				t.Errorf("shape_format = %q, want geojson", req.ShapeFormat)
			}
			if len(req.Locations) != 2 {
				t.Errorf("locations = %d, want 2", len(req.Locations))
			}
			// 两段 leg,拼接处 [2,2] 应去重
			return `{"trip":{"legs":[{"shape":[[1,1],[2,2]]},{"shape":[[2,2],[3,3]]}],"summary":{"length":5}}}`
		})
	c := NewClientWithBase(f.srv.URL, nil)
	got, err := c.RoutePolyline(model.Point{Lat: 1, Lng: 1}, model.Point{Lat: 3, Lng: 3}, model.ModeDriving)
	if err != nil {
		t.Fatalf("RoutePolyline: %v", err)
	}
	want := [][2]float64{{1, 1}, {2, 2}, {3, 3}}
	if len(got) != len(want) {
		t.Fatalf("拼接后 %d 个点, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("shape[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestRoutePolylineTransitNil(t *testing.T) {
	f := newFake(t, nil, func(string) string {
		t.Error("公交轨迹不应发出请求")
		return `{}`
	})
	c := NewClientWithBase(f.srv.URL, nil)
	got, err := c.RoutePolyline(model.Point{Lat: 1, Lng: 1}, model.Point{Lat: 2, Lng: 2}, model.ModeTransit)
	if err != nil || got != nil {
		t.Fatalf("transit 应返回 (nil, nil) —— 前端退化为直线, got %v, %v", got, err)
	}
}

func TestDistanceUsesCache(t *testing.T) {
	calls := 0
	f := newFake(t, func(string) string {
		calls++
		return `{"sources_to_targets":[[{"distance":9,"time":1},{"distance":9,"time":1}],[{"distance":9,"time":1},{"distance":9,"time":1}]],"units":"km"}`
	}, nil)
	mem := cache.NewMemory(time.Hour)
	c := NewClientWithBase(f.srv.URL, mem)
	a, b := model.Point{Lat: 1, Lng: 1}, model.Point{Lat: 2, Lng: 2}
	km1, err := c.Distance(a, b, model.ModeDriving)
	if err != nil {
		t.Fatalf("Distance: %v", err)
	}
	km2, _ := c.Distance(a, b, model.ModeDriving)
	if km1 != 9 || km2 != 9 {
		t.Fatalf("距离值错: %v %v", km1, km2)
	}
	if calls != 1 {
		t.Fatalf("同一对点第二次应命中缓存, 上游被调了 %d 次", calls)
	}
}

func TestDistanceCacheKeySeparatesModes(t *testing.T) {
	calls := 0
	f := newFake(t, func(string) string {
		calls++
		return `{"sources_to_targets":[[{"distance":9,"time":1},{"distance":9,"time":1}],[{"distance":9,"time":1},{"distance":9,"time":1}]],"units":"km"}`
	}, nil)
	c := NewClientWithBase(f.srv.URL, cache.NewMemory(time.Hour))
	a, b := model.Point{Lat: 1, Lng: 1}, model.Point{Lat: 2, Lng: 2}
	c.Distance(a, b, model.ModeDriving)
	c.Distance(a, b, model.ModeWalking)
	if calls != 2 {
		t.Fatalf("不同方式必须各发一次(key 要带方式), got %d", calls)
	}
}
