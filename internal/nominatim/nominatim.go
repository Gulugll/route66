// Package nominatim 封装 OSM 的地名搜索服务(Nominatim)。
// 高德搜索被门控后的默认搜索来源:公共实例免费、无 key,
// 但有使用政策 —— 1 QPS、必须带 User-Agent、结果需署名,这里如实遵守。
package nominatim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"awesomeProject/internal/model"
)

type Client struct {
	base string
	http *http.Client
	ua   string
}

func NewClient(base string) *Client {
	return &Client{
		base: base,
		http: &http.Client{Timeout: 5 * time.Second},
		// Nominatim 使用政策要求可识别的 User-Agent(带联系方式更佳)
		ua: "Route66/1.0 (self-hosted route planner)",
	}
}

// NewClientWithBase 测试注入用,语义同 valhalla.NewClientWithBase。
func NewClientWithBase(base string) *Client { return NewClient(base) }

type rawPlace struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// SearchPlaces 关键词搜索。city 非空时拼进查询词(Nominatim 没有"城市"参数,
// 词级限定是公共实例下最稳的做法)。返回形态与 amap.SearchPlaces 一致,
// 前端 / api 层契约不变。
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
	u := c.base + "/search?" + url.Values{
		"q":               {q},
		"format":          {"jsonv2"},
		"limit":           {strconv.Itoa(limit)},
		"accept-language": {"zh-CN"},
	}.Encode()

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("nominatim: HTTP %d: %s", resp.StatusCode, b)
	}
	var raw []rawPlace
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("nominatim 响应解析失败: %w", err)
	}
	places := make([]model.Place, 0, len(raw))
	for _, r := range raw {
		lat, _ := strconv.ParseFloat(r.Lat, 64)
		lng, _ := strconv.ParseFloat(r.Lon, 64)
		name := r.Name
		if name == "" {
			// jsonv2 下 POI 通常有 name;街道级结果只有 display_name,取首段
			name = r.DisplayName
			if i := strings.Index(name, ","); i > 0 {
				name = name[:i]
			}
		}
		places = append(places, model.Place{Name: name, Address: r.DisplayName, Lat: lat, Lng: lng})
	}
	return places, nil
}
