// Package photon 封装 Photon(基于 Elasticsearch 的 OSM 搜索服务)。
// 之所以有这个包:公共 Nominatim 实例在中国大陆网络不可达,而 Photon 可达,
// 数据同源(OpenStreetMap)—— 搜索来源遵循与距离来源一致的"可替换"约定。
// 使用公共实例(photon.komoot.io)遵守其 fair-use 政策;自托管无需改代码,换 base 即可。
package photon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"awesomeProject/internal/model"
)

type Client struct {
	base string
	http *http.Client
}

func NewClient(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 5 * time.Second}}
}

// NewClientWithBase 测试注入用,语义同其他地理来源客户端。
func NewClientWithBase(base string) *Client { return NewClient(base) }

type rawFeature struct {
	Geometry struct {
		Coordinates []float64 `json:"coordinates"` // [lon, lat],GeoJSON 顺序
	} `json:"geometry"`
	Properties struct {
		Name    string `json:"name"`
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"properties"`
}

// SearchPlaces 关键词搜索。city 非空时拼进查询词(与 nominatim 同一约定)。
// 返回形态与 amap.SearchPlaces / nominatim.SearchPlaces 一致,api 层契约不变。
func (c *Client) SearchPlaces(keyword, city string, limit int) ([]model.Place, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, fmt.Errorf("关键词为空")
	}
	q := keyword
	if strings.TrimSpace(city) != "" {
		q = keyword + " " + city
	}
	if limit <= 0 {
		limit = 6
	}
	u := c.base + "/api?" + url.Values{
		"q":     {q},
		"limit": {fmt.Sprint(limit)},
		"lang":  {"default"},
	}.Encode()

	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("photon: HTTP %d: %s", resp.StatusCode, b)
	}
	var raw struct {
		Features []rawFeature `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("photon 响应解析失败: %w", err)
	}

	places := make([]model.Place, 0, len(raw.Features))
	for _, f := range raw.Features {
		p := f.Properties
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		name := p.Name
		if name == "" {
			name = p.Street
		}
		// 地址由 city/country 拼一句,足够让用户确认是不是想去的那个
		address := strings.TrimSpace(p.City + " " + p.Country)
		places = append(places, model.Place{
			Name:    name,
			Address: address,
			Lat:     f.Geometry.Coordinates[1],
			Lng:     f.Geometry.Coordinates[0],
		})
	}
	return places, nil
}
