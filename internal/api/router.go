package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/solver"
)

// Server 持有 HTTP 层需要的依赖。所有业务逻辑都藏在别的包里,这里只做编排。
type Server struct {
	matrix *matrix.Service
	amap   *amap.Client // 可空:没配 key 时 /search 不可用
}

// NewRouter 组装路由。gin.Engine 就是"标准库 ServeMux + 中间件"的增强版,
// gin.Default() 自带日志和 panic 恢复两个中间件。
func NewRouter(m *matrix.Service, am *amap.Client) *gin.Engine {
	s := &Server{matrix: m, amap: am}

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.POST("/plan", s.plan)
	r.GET("/search", s.search)
	r.GET("/route", s.route)
	// 托管前端页面:没命中上面 API 路由的请求,交给 web/ 目录的静态文件服务器。
	// 用 NoRoute 而不是 Static("/") —— 因为 httprouter 不允许根路径 catch-all
	// 和 /healthz 这种具体路由共存(会 panic),NoRoute 是社区标准兜底做法。
	// 页面和 API 同源(都在 :7800),前端用相对路径 /plan 调用,不需要 CORS。
	r.NoRoute(gin.WrapH(http.FileServer(http.Dir("web"))))
	return r
}

type planReq struct {
	Origin       model.Point   `json:"origin"`
	Destinations []model.Point `json:"destinations"`
	Mode         string        `json:"mode"`    // driving|walking|transit,默认 driving
	Manual       bool          `json:"manual"`  // true = 按列表顺序,不跑 TSP
	Segments     []string      `json:"segments"` // 可选:每段的方式,如 ["walking","transit"]
}

type planResp struct {
	Order   []string `json:"order"`    // 访问顺序,按名字返回
	TotalKm float64  `json:"total_km"` // 沿该顺序走完全程的总距离
}

// plan 是整条业务链路的编排:坐标 → 距离矩阵 → 最优顺序 → 结果。
// 它不知道距离是怎么算的(高德?直线?),也不知道 TSP 用了什么算法。
func (s *Server) plan(c *gin.Context) {
	var req planReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Destinations) < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少需要一个目的地"})
		return
	}

	// 出行方式:前端没传就默认驾车(最常用的)。
	mode := model.Mode(req.Mode)
	if mode == "" {
		mode = model.ModeDriving
	}

	points := append([]model.Point{req.Origin}, req.Destinations...)
	order := make([]int, len(points))
	for i := range order {
		order[i] = i
	}

	var total float64
	if len(req.Segments) > 0 {
		// —— 混合出行:每段各自的方式 ——
		// 每段方式不同 → 没有统一的距离矩阵 → 无法做 TSP,
		// 顺序就是用户列表顺序,逐段算距离求和。
		if s.amap == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,混合出行不可用"})
			return
		}
		if len(req.Segments) != len(points)-1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "segments 长度必须等于地点数-1"})
			return
		}
		for i := 0; i < len(points)-1; i++ {
			km, err := s.amap.Distance(points[i], points[i+1], model.Mode(req.Segments[i]))
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "路段 " + points[i].Name + "→" + points[i+1].Name + " 失败: " + err.Error()})
				return
			}
			total += km
		}
	} else {
		// —— 单一方式:全矩阵 + (可选)TSP ——
		dists := s.matrix.DistanceMatrix(points, mode)
		if !req.Manual {
			// 模拟退火内部:最近邻粗解 → 2-opt 局部最优 → 跳出坑找更好的
			order = solver.SimulatedAnnealing(dists, 0)
		}
		total = solver.TourLength(dists, order)
	}

	resp := planResp{TotalKm: total}
	for _, i := range order {
		resp.Order = append(resp.Order, points[i].Name)
	}
	c.JSON(http.StatusOK, resp)
}

// search 是"地名 → 候选地点"的代理接口:前端传关键词,我们调高德搜索,
// key 藏在后端(前端不碰第三方凭据),以后还能给搜索加缓存/限流。
func (s *Server) search(c *gin.Context) {
	if s.amap == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,搜索不可用"})
		return
	}
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 q 参数"})
		return
	}
	// city 可空:前端不填就是全国搜索,填了(如"杭州")就限定该城市。
	// 这是"参数化"——把硬编码变成调用方决定,接口更通用。
	places, err := s.amap.SearchPlaces(q, c.Query("city"), 6)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "高德搜索失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"places": places})
}

// route 是"两点之间的真实路网轨迹"代理接口:前端画路线用。
// 和 /search 一个套路——前端不直接调高德,轨迹也走后端拿。
func (s *Server) route(c *gin.Context) {
	if s.amap == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,路线不可用"})
		return
	}
	a, err := parsePoint(c.Query("origin"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "origin 参数无效: " + err.Error()})
		return
	}
	b, err := parsePoint(c.Query("dest"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dest 参数无效: " + err.Error()})
		return
	}
	mode := model.Mode(c.Query("mode"))
	if mode == "" {
		mode = model.ModeDriving
	}
	pts, err := s.amap.RoutePolyline(a, b, mode)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "高德路线失败: " + err.Error()})
		return
	}
	if pts == nil {
		pts = [][2]float64{} // 空数组,前端降级画直线
	}
	c.JSON(http.StatusOK, gin.H{"polyline": pts})
}

// parsePoint 解析 "lng,lat" 字符串成坐标(和高德格式一致,经度在前)。
func parsePoint(s string) (model.Point, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return model.Point{}, fmt.Errorf("需要 lng,lat 格式")
	}
	lng, err1 := strconv.ParseFloat(parts[0], 64)
	lat, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil {
		return model.Point{}, fmt.Errorf("坐标不是数字")
	}
	return model.Point{Lng: lng, Lat: lat}, nil
}
