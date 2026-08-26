// Package amap 封装高德地图 Web 服务 API,回答两类问题:
//  1. "任意两点之间,按某种出行方式的真实距离是多少(公里)"
//  2. "某个关键词有哪些地点候选"(搜索)
//
// 出行方式对应不同的高德接口(实测确认):
//   driving → v3/distance(type=1 驾车,支持批量,逐列请求)
//   walking → v5/direction/walking(单对,逐对请求)
//   transit → v3/direction/transit/integrated(单对,逐对请求)
// 骑行(cycling)对个人开发者 key 返回 RESOURCE_UNAVAILABLE,暂不支持。
//
// 距离矩阵怎么算(driving):实测高德 v3/distance 的批量语义是
// "多个起点 → 单个终点"(文档写的一一对应是错的,多对多会返回 INVALID_PARAMS),
// 所以策略是"逐列批量"——一次请求把"所有点 i → 点 j"算出来,n 点共 n 次请求。
// walking/transit 没有批量接口,只能逐对调,一次矩阵 n×(n-1) 次请求。
package amap

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

// baseURL 高德 v3/distance 接口地址。
// 用 var 而不是 const,是为了测试里能替换成本地模拟服务器(httptest)。
var baseURL = "https://restapi.amap.com/v3/distance"

// Client 高德 Web 服务客户端。
// cache 字段可空:传 nil 表示不缓存(比如测试或想关掉缓存时)。
type Client struct {
	key   string
	http  *http.Client
	cache cache.Cache
}

// NewClient 创建一个高德客户端。key 是 Web 服务 key(不是 JS key)。
// c 可为 nil。超时 5 秒:外部 API 必须设超时,否则一个慢请求能把
// 整个 /plan 卡死——这是"外部依赖要有边界"的基本功。
func NewClient(key string, c cache.Cache) *Client {
	return &Client{
		key:   key,
		http:  &http.Client{Timeout: 5 * time.Second},
		cache: c,
	}
}

// DistanceMatrix 返回 n×n 距离矩阵(公里),dists[i][j] = 点 i 到 j(按 mode 出行)。
// 任何一步失败都返回 error——具体怎么降级由上层决定(matrix 会退回 haversine)。
func (c *Client) DistanceMatrix(points []model.Point, mode model.Mode) ([][]float64, error) {
	switch mode {
	case model.ModeDriving:
		return c.drivingMatrix(points)
	case model.ModeWalking, model.ModeTransit:
		return c.pairwiseMatrix(points, mode)
	default:
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
}

// drivingMatrix 驾车矩阵:逐列批量(v3/distance type=1)。
func (c *Client) drivingMatrix(points []model.Point) ([][]float64, error) {
	n := len(points)
	dists := make([][]float64, n)
	for i := range dists {
		dists[i] = make([]float64, n)
	}

	for j := 0; j < n; j++ {
		// 第 j 列:所有点 i≠j 到点 j 的距离。先查缓存,没命中的凑一批请求。
		origins := make([]string, 0, n-1) // 多个起点
		idxs := make([]int, 0, n-1)       // 记录每个结果该填到哪一行

		for i := 0; i < n; i++ {
			if i == j {
				continue
			}
			if km, ok := c.cached(cacheKey(model.ModeDriving, points[i], points[j])); ok {
				dists[i][j] = km
				continue
			}
			origins = append(origins, coord(points[i]))
			idxs = append(idxs, i)
		}
		if len(idxs) == 0 {
			continue // 这列全命中缓存,不用发请求
		}

		col, err := c.fetchColumn(origins, points[j])
		if err != nil {
			return nil, err
		}
		for k, i := range idxs {
			dists[i][j] = col[k]
			c.store(cacheKey(model.ModeDriving, points[i], points[j]), col[k]) // 写缓存,下次直接命中
		}
	}
	return dists, nil
}

// pairwiseMatrix 逐对矩阵(walking/transit):没有批量接口,只能一对一发请求。
// 注意两点:
//  1. 高德个人 key 的 QPS 限制很严(实测步行约 3/s),连发会返回
//     CUQPS_HAS_EXCEEDED_THE_LIMIT——所以请求之间要"节流"(sleep)尊重配额
//  2. 教学规模(≤10 点)可接受;点数多会慢,这是逐对接口的代价
func (c *Client) pairwiseMatrix(points []model.Point, mode model.Mode) ([][]float64, error) {
	n := len(points)
	dists := make([][]float64, n)
	for i := range dists {
		dists[i] = make([]float64, n)
	}

	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			k := cacheKey(mode, points[i], points[j])
			if km, ok := c.cached(k); ok {
				dists[i][j] = km
				continue
			}
			km, err := c.pairDistance(points[i], points[j], mode)
			if err != nil {
				return nil, err
			}
			dists[i][j] = km
			c.store(k, km)
			// 节流:每次请求后歇一歇,别打爆 QPS 被限流。
			// 这里只在实际发了请求后 sleep,缓存命中不 sleep(不费配额)。
			time.Sleep(350 * time.Millisecond)
		}
	}
	return dists, nil
}

// Distance 算单个点对 a→b 在 mode 下的距离(公里),带缓存 + 节流。
// 这是"逐段距离"的入口:混合出行时,每段用各自的方式单独算。
// 和 pairwiseMatrix 的区别:那个算全矩阵,这个只算一对。
func (c *Client) Distance(a, b model.Point, mode model.Mode) (float64, error) {
	k := cacheKey(mode, a, b)
	if km, ok := c.cached(k); ok {
		return km, nil
	}
	km, err := c.pairDistance(a, b, mode)
	if err != nil {
		return 0, err
	}
	c.store(k, km)
	time.Sleep(350 * time.Millisecond) // 节流,同 pairwiseMatrix
	return km, nil
}

// pairDistance 算单个点对 a→b 在 mode 下的距离(公里)。
func (c *Client) pairDistance(a, b model.Point, mode model.Mode) (float64, error) {
	var apiURL string
	switch mode {
	case model.ModeDriving:
		apiURL = "https://restapi.amap.com/v3/direction/driving"
	case model.ModeWalking:
		apiURL = "https://restapi.amap.com/v5/direction/walking"
	case model.ModeTransit:
		apiURL = "https://restapi.amap.com/v3/direction/transit/integrated"
	default:
		return 0, fmt.Errorf("unsupported mode %q", mode)
	}

	q := url.Values{}
	q.Set("key", c.key)
	q.Set("origin", coord(a))     // v5/v3 单对接口用 origin/destination
	q.Set("destination", coord(b))
	log.Printf("[amap] %s %s -> %s", mode, coord(a), coord(b))

	resp, err := c.http.Get(apiURL + "?" + q.Encode())
	if err != nil {
		return 0, fmt.Errorf("amap %s request: %w", mode, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("amap %s read body: %w", mode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("amap %s http %d: %s", mode, resp.StatusCode, body)
	}

	// 两种接口结构不同:walking 在 route.paths[],transit 在 route.transits[]。
	// 用一个宽松结构先解出 status/info,再按 mode 取对应的 distance 字段。
	var envelope struct {
		Status string          `json:"status"`
		Info   string          `json:"info"`
		Route  json.RawMessage `json:"route"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, fmt.Errorf("amap %s decode: %w", mode, err)
	}
	if envelope.Status != "1" {
		return 0, fmt.Errorf("amap %s status=%s info=%s", mode, envelope.Status, envelope.Info)
	}

	var meters float64
	switch mode {
	case model.ModeDriving, model.ModeWalking:
		// v3 driving / v5 walking 都是 route.paths[0].distance(米)
		var route struct {
			Paths []struct {
				Distance string `json:"distance"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(envelope.Route, &route); err != nil {
			return 0, fmt.Errorf("amap %s decode route: %w", mode, err)
		}
		if len(route.Paths) == 0 {
			return 0, fmt.Errorf("amap %s: no paths", mode)
		}
		meters, err = strconv.ParseFloat(route.Paths[0].Distance, 64)
	case model.ModeTransit:
		var route struct {
			Transits []struct {
				Distance string `json:"distance"`
			} `json:"transits"`
		}
		if err := json.Unmarshal(envelope.Route, &route); err != nil {
			return 0, fmt.Errorf("amap transit decode route: %w", err)
		}
		if len(route.Transits) == 0 {
			return 0, fmt.Errorf("amap transit: no transits")
		}
		// 多个公交方案取第一个(高德默认按推荐排序)
		meters, err = strconv.ParseFloat(route.Transits[0].Distance, 64)
	}
	if err != nil {
		return 0, fmt.Errorf("amap %s bad distance: %w", mode, err)
	}
	return meters / 1000, nil // 米 → 公里
}

// fetchColumn 发一次 v3/distance 请求:多个起点 → 单个终点。
// 高德返回的 results 顺序与请求里的 origins 一一对应。
func (c *Client) fetchColumn(origins []string, dest model.Point) ([]float64, error) {
	q := url.Values{}
	q.Set("key", c.key)
	q.Set("type", "1")                // 1 = 驾车导航距离,按真实路网
	q.Set("origins", strings.Join(origins, "|"))
	q.Set("destination", coord(dest)) // 单终点——实测批量只认这种

	// 外部调用打日志是生产惯例:能看清每次请求,排查配额/延迟全靠它。
	log.Printf("[amap] driving %d origins -> %s", len(origins), coord(dest))

	resp, err := c.http.Get(baseURL + "?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("amap request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("amap read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amap http %d: %s", resp.StatusCode, body)
	}

	// 高德响应:status="1" 成功;"0" 失败(配额超了/key 错了/参数非法),info 带原因。
	var out struct {
		Status  string `json:"status"`
		Info    string `json:"info"`
		Results []struct {
			Distance string `json:"distance"` // 单位:米
			Duration string `json:"duration"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("amap decode: %w", err)
	}
	if out.Status != "1" {
		return nil, fmt.Errorf("amap status=%s info=%s", out.Status, out.Info)
	}
	if len(out.Results) != len(origins) {
		return nil, fmt.Errorf("amap result count %d != requested %d", len(out.Results), len(origins))
	}

	row := make([]float64, len(out.Results))
	for i, r := range out.Results {
		m, err := strconv.ParseFloat(r.Distance, 64)
		if err != nil {
			return nil, fmt.Errorf("amap bad distance %q: %w", r.Distance, err)
		}
		row[i] = m / 1000 // 米 → 公里,和 haversine 的单位对齐
	}
	return row, nil
}

// RoutePolyline 返回 a→b 在 mode 下的真实路网轨迹,一串 [lng,lat] 坐标。
// 前端画线用:拿到轨迹点就能画出贴合道路的路线,而不是两点直线。
// 实测:
//   driving → v3/direction/driving,steps[].polyline 有轨迹
//   walking → v5 步行不返回轨迹坐标,要用 v3/direction/walking
//   transit → 公交由"步行段+公交段"组成,轨迹不连续,返回 nil 让前端画直线
func (c *Client) RoutePolyline(a, b model.Point, mode model.Mode) ([][2]float64, error) {
	var apiURL string
	switch mode {
	case model.ModeDriving:
		apiURL = "https://restapi.amap.com/v3/direction/driving"
	case model.ModeWalking:
		apiURL = "https://restapi.amap.com/v3/direction/walking"
	default:
		return nil, nil // transit:暂不提供真实轨迹
	}

	q := url.Values{}
	q.Set("key", c.key)
	q.Set("origin", coord(a))
	q.Set("destination", coord(b))
	log.Printf("[amap] route %s %s -> %s", mode, coord(a), coord(b))

	resp, err := c.http.Get(apiURL + "?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("amap route request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("amap route read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amap route http %d: %s", resp.StatusCode, body)
	}

	var out struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		Route  struct {
			Paths []struct {
				Steps []struct {
					Polyline string `json:"polyline"` // "lng,lat;lng,lat;..."
				} `json:"steps"`
			} `json:"paths"`
		} `json:"route"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("amap route decode: %w", err)
	}
	if out.Status != "1" {
		return nil, fmt.Errorf("amap route status=%s info=%s", out.Status, out.Info)
	}
	if len(out.Route.Paths) == 0 {
		return nil, fmt.Errorf("amap route: no paths")
	}

	// 一条完整路线由若干 step 组成,每步有一段 polyline,首尾相接就是全程轨迹。
	var pts [][2]float64
	for _, step := range out.Route.Paths[0].Steps {
		seg, err := parsePolyline(step.Polyline)
		if err != nil {
			return nil, err
		}
		pts = append(pts, seg...)
	}
	return pts, nil
}

// parsePolyline 解析高德轨迹字符串 "lng,lat;lng,lat;..." → [][2]float64。
func parsePolyline(s string) ([][2]float64, error) {
	var pts [][2]float64
	for _, seg := range strings.Split(s, ";") {
		parts := strings.Split(seg, ",")
		if len(parts) != 2 {
			continue
		}
		lng, err1 := strconv.ParseFloat(parts[0], 64)
		lat, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("amap bad polyline point %q", seg)
		}
		pts = append(pts, [2]float64{lng, lat})
	}
	return pts, nil
}

// cacheKey 缓存 key:出行方式 + 起点 -> 终点。
// 同一个点对,步行和驾车的距离不同,必须分开缓存——这就是 key 带 mode 的原因。
func cacheKey(mode model.Mode, a, b model.Point) string {
	return string(mode) + "|" + coord(a) + "->" + coord(b)
}

func (c *Client) cached(k string) (float64, bool) {
	if c.cache == nil {
		return 0, false
	}
	return c.cache.Get(k)
}

func (c *Client) store(k string, km float64) {
	if c.cache != nil {
		c.cache.Set(k, km)
	}
}

// SearchPlaces 按关键词搜地点(地理编码的"正向搜索"):"广州塔" → 候选列表。
// 走高德 v3/place/text(关键词搜索),和距离矩阵走不同的接口。
// city 可空:空 = 全国搜索;填了 = 只在该城市找(citylimit=true,结果更准)。
// 前端不直接调高德搜索,而是调我们的 /search——key 藏后端,还能统一加缓存/日志。
func (c *Client) SearchPlaces(keyword, city string, limit int) ([]model.Place, error) {
	q := url.Values{}
	q.Set("key", c.key)
	q.Set("keywords", keyword)
	q.Set("offset", strconv.Itoa(limit))
	q.Set("page", "1")
	if city != "" {
		// 注意:citylimit 只在 city 有值时才有意义,全国搜索就别传
		q.Set("city", city)
		q.Set("citylimit", "true")
	}

	resp, err := c.http.Get("https://restapi.amap.com/v3/place/text?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("amap search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("amap search read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amap search http %d: %s", resp.StatusCode, body)
	}

	var out struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		Pois   []struct {
			Name     string `json:"name"`
			Address  string `json:"address"`
			Location string `json:"location"` // 高德格式 "经度,纬度"(字符串)
		} `json:"pois"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("amap search decode: %w", err)
	}
	if out.Status != "1" {
		return nil, fmt.Errorf("amap search status=%s info=%s", out.Status, out.Info)
	}

	places := make([]model.Place, 0, len(out.Pois))
	for _, p := range out.Pois {
		// "lng,lat" 字符串拆开,注意经度在前,和之前距离接口同一个坑。
		parts := strings.Split(p.Location, ",")
		if len(parts) != 2 {
			continue
		}
		lng, err1 := strconv.ParseFloat(parts[0], 64)
		lat, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		places = append(places, model.Place{Name: p.Name, Address: p.Address, Lat: lat, Lng: lng})
	}
	return places, nil
}

// coord 把点拼成高德要求的 "经度,纬度"。注意顺序:经度在前,和
// model.Point 的字段顺序(Lat, Lng)相反,别写反了——这是调外部 API 的经典错误。
func coord(p model.Point) string {
	return strconv.FormatFloat(p.Lng, 'f', 6, 64) + "," +
		strconv.FormatFloat(p.Lat, 'f', 6, 64)
}
