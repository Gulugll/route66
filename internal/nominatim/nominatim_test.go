package nominatim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchPlaces(t *testing.T) {
	var gotPath, gotUA, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUA, gotQuery = r.URL.Path, r.Header.Get("User-Agent"), r.URL.RawQuery
		io.WriteString(w, `[
			{"lat":"39.9163","lon":"116.3972","name":"故宫博物院","display_name":"故宫博物院, 景山前街4号, 北京"},
			{"lat":"39.9000","lon":"116.4000","name":"","display_name":"南池子大街, 北京"}
		]`)
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL)
	places, err := c.SearchPlaces("故宫", "北京", 6)
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if gotPath != "/search" {
		t.Errorf("path = %q", gotPath)
	}
	if gotUA == "" {
		t.Error("Nominatim 政策要求 User-Agent,请求没带")
	}
	if gotQuery == "" || !contains(gotQuery, "q=") {
		t.Errorf("query = %q", gotQuery)
	}
	if len(places) != 2 {
		t.Fatalf("候选数 = %d, want 2", len(places))
	}
	if places[0].Name != "故宫博物院" || places[0].Lat != 39.9163 || places[0].Lng != 116.3972 {
		t.Errorf("第一个候选解析错: %+v", places[0])
	}
	// 无 name 的结果取 display_name 首段
	if places[1].Name != "南池子大街" {
		t.Errorf("display_name 首段提取错: %q", places[1].Name)
	}
}

func TestSearchPlacesCityJoinsQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		io.WriteString(w, `[]`)
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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
