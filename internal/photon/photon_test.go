package photon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"awesomeProject/internal/model"
)

// Photon 是 Komoot 维护的 OSM 搜索服务(基于 Elasticsearch)。
// 之所以有这个包:公共 Nominatim 实例在中国大陆网络不可达,而 Photon 可达,
// 且都基于 OSM 数据 —— 搜索来源也遵循"可替换"的同一约定。
// 接口:GET /api?q=&limit=&lang=,响应是 GeoJSON FeatureCollection,
// geometry.coordinates 为 [lon, lat](经度在前)。

func TestSearch(t *testing.T) {
	var gotQuery, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			t.Errorf("path = %q, want /api", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("q")
		gotLimit = r.URL.Query().Get("limit")
		// 真实 Photon 响应的裁剪版
		w.Write([]byte(`{"features":[
			{"geometry":{"coordinates":[116.3972,39.9163]},
			 "properties":{"name":"故宫博物院","city":"北京","country":"中国"}},
			{"geometry":{"coordinates":[116.4,39.9]},
			 "properties":{"name":"","street":"南池子大街","city":"北京","country":"中国"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL)
	places, err := c.SearchPlaces("故宫", "", 6)
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if gotQuery != "故宫" {
		t.Errorf("q = %q", gotQuery)
	}
	if gotLimit != "6" {
		t.Errorf("limit = %q", gotLimit)
	}
	if len(places) != 2 {
		t.Fatalf("候选数 = %d, want 2", len(places))
	}
	p0 := places[0]
	if p0.Name != "故宫博物院" || p0.Lat != 39.9163 || p0.Lng != 116.3972 {
		t.Errorf("第一个候选解析错(coordinates 应为 [lon,lat]): %+v", p0)
	}
	if p0.Address == "" {
		t.Errorf("address 应由 city/country 拼出: %+v", p0)
	}
	// name 为空时回退 street
	if places[1].Name != "南池子大街" {
		t.Errorf("name 回退 street 失败: %q", places[1].Name)
	}
}

func TestSearchCityJoinsQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Write([]byte(`{"features":[]}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL)
	if _, err := c.SearchPlaces("西湖", "杭州", 6); err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if gotQuery != "西湖 杭州" {
		t.Errorf("city 应拼进查询词, q = %q", gotQuery)
	}
}

func TestSearchEmpty(t *testing.T) {
	c := NewClientWithBase("http://localhost:1")
	if _, err := c.SearchPlaces("  ", "", 6); err == nil {
		t.Fatal("空关键词应直接报错,不发请求")
	}
	if _, err := c.SearchPlaces("故宫", "", 6); err == nil {
		t.Fatal("服务不可达应返回 error(触发 502)")
	}
}

var _ = model.Place{} // 保持 model 依赖(接口形状与 nominatim 一致)
