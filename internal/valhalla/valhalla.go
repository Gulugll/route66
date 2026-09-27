// Package valhalla 封装自托管 Valhalla(OpenStreetMap 路网引擎)的 HTTP API,
// 与 amap.Client 实现同一个 geo.Source 接口 —— 距离/轨迹来源可替换的具体兑现。
//
// 与高德的关键差异:
//   - 矩阵一次请求算完(sources×targets),没有"逐列批量"的拆分,也没有配额;
//   - 轨迹走 /route,shape_format=geojson 直接返回 [lng,lat] 数组;
//   - 不支持公交(costing 无 transit),返回错误交由上层降级;
//   - 无 key,可用性 = 服务可达性,失败一律视为降级(和 Redis/高德同一套语义)。
//
// 接口用 POST + JSON(Valhalla 惯例),路径常量收口在本文件,根地址是 Client 字段。
package valhalla

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

const (
	pathMatrix = "/sources_to_targets"
	pathRoute  = "/route"

	// costing:Valhalla 的出行方式名,和本项目的 Mode 是两套词表,在此收口映射
	costingAuto      = "auto"
	costingPedestran = "pedestrian"
)

// Client Valhalla HTTP 客户端。base 是根地址(如 http://localhost:8002),
// 作为实例字段而非包级变量,测试可指向本地假服务器(与 amap 同一个约定)。
type Client struct {
	base  string
	http  *http.Client
	cache cache.Cache // 单段 Distance 的缓存;nil = 不缓存(矩阵本身单请求,不缓存)
}

func NewClient(base string, c cache.Cache) *Client {
	return NewClientWithBase(base, c)
}

// NewClientWithBase 测试注入用:指定根地址。生产走 NewClient。
func NewClientWithBase(base string, c cache.Cache) *Client {
	return &Client{
		base:  base,
		http:  &http.Client{Timeout: 5 * time.Second},
		cache: c,
	}
}

// HasAPIKey 让 valhalla.Client 满足 geo.Source:它没有 key 概念,
// 装配了即视为可用;真正的失败在请求时发生,由上层按降级处理。
func (c *Client) HasAPIKey() bool { return true }

// SearchPlaces Valhalla 不提供搜索,搜索由 Nominatim 负责。
// 保留该方法只为满足 geo.Source 接口形状。
func (c *Client) SearchPlaces(string, string, int) ([]model.Place, error) {
	return nil, fmt.Errorf("valhalla 不提供搜索,请使用 Nominatim")
}

// costing 把本项目的 Mode 映射为 Valhalla costing;transit 无对应值。
func costing(m model.Mode) (string, error) {
	switch m {
	case model.ModeDriving:
		return costingAuto, nil
	case model.ModeWalking:
		return costingPedestran, nil
	default:
		return "", fmt.Errorf("valhalla 不支持公交,该模式走 haversine 降级")
	}
}

type ll struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func toLL(p model.Point) ll { return ll{Lat: p.Lat, Lon: p.Lng} }

// post POST JSON 并把响应体反序列化到 out。非 200 一律报错。
func (c *Client) post(path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.base+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("valhalla %s: HTTP %d: %s", path, resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("valhalla %s 响应解析失败: %w", path, err)
	}
	return nil
}

// Matrix n×n 距离矩阵(公里),一次请求算完。
func (c *Client) DistanceMatrix(points []model.Point, mode model.Mode) ([][]float64, error) {
	cost, err := costing(mode)
	if err != nil {
		return nil, err
	}
	// sources 与 targets 用同一组点:我们要的就是完整 n×n
	pts := make([]ll, len(points))
	for i, p := range points {
		pts[i] = toLL(p)
	}
	var resp struct {
		Cells [][]struct {
			Distance float64 `json:"distance"`
		} `json:"sources_to_targets"`
	}
	if err := c.post(pathMatrix, map[string]any{
		"sources": pts,
		"targets": pts,
		"costing": cost,
		"units":   "km",
	}, &resp); err != nil {
		return nil, err
	}
	n := len(points)
	if len(resp.Cells) != n {
		return nil, fmt.Errorf("valhalla 矩阵行数 %d != 点数 %d", len(resp.Cells), n)
	}
	out := make([][]float64, n)
	for i := 0; i < n; i++ {
		if len(resp.Cells[i]) != n {
			return nil, fmt.Errorf("valhalla 矩阵第 %d 行列数 %d != %d", i, len(resp.Cells[i]), n)
		}
		out[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			out[i][j] = resp.Cells[i][j].Distance
		}
	}
	return out, nil
}

// Distance 两点间距离(公里)。走 2×2 矩阵取 [0][1],结果进缓存 ——
// planner 的步行/公交分支会对最终顺序的每条边单独问一次,重复规划能整段命中。
func (c *Client) Distance(a, b model.Point, mode model.Mode) (float64, error) {
	if c.cache != nil {
		key := cacheKey(a, b, mode)
		if km, ok := c.cache.Get(key); ok {
			return km, nil
		}
		dists, err := c.DistanceMatrix([]model.Point{a, b}, mode)
		if err != nil {
			return 0, err
		}
		km := dists[0][1]
		c.cache.Set(key, km)
		return km, nil
	}
	dists, err := c.DistanceMatrix([]model.Point{a, b}, mode)
	if err != nil {
		return 0, err
	}
	return dists[0][1], nil
}

func cacheKey(a, b model.Point, mode model.Mode) string {
	// 与 amap 的 key 结构保持"方式|点对"形状,但加来源前缀:
	// 两家引擎对同一点对的距离值不同,共用 key 会互相污染。
	return fmt.Sprintf("dist:valhalla:%s|%f,%f->%f,%f", mode, a.Lng, a.Lat, b.Lng, b.Lat)
}

// RoutePolyline 两点间路网轨迹([lng,lat] 数组,与高德轨迹同形,前端零改动)。
// transit 返回 (nil, nil):公交无连续轨迹是业务事实,前端退化为直线。
func (c *Client) RoutePolyline(a, b model.Point, mode model.Mode) ([][2]float64, error) {
	cost, err := costing(mode)
	if err != nil {
		return nil, nil // transit:不是故障,是语义
	}
	var resp struct {
		Trip struct {
			Legs []struct {
				Shape [][2]float64 `json:"shape"`
			} `json:"legs"`
		} `json:"trip"`
	}
	payload := map[string]any{
		"locations":    []ll{toLL(a), toLL(b)},
		"costing":      cost,
		"shape_format": "geojson",
	}
	if err := c.post(pathRoute, payload, &resp); err != nil {
		return nil, err
	}
	// 多段 leg 首尾拼接,拼接点去重
	var out [][2]float64
	for i, leg := range resp.Trip.Legs {
		for k, pt := range leg.Shape {
			if i > 0 && k == 0 {
				continue // 与上一段末点重合
			}
			out = append(out, pt)
		}
	}
	return out, nil
}
