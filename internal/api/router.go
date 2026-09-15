package api

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/solver"
)

// 常量定义
const (
	// MaxPoints 驾车(批量接口)的上限。驾车走 v3/distance,一次请求能算
	// "所有点 → 某一个点"整列,所以 n 个点只要 n 次请求,50 点也就 50 次,扛得住。
	MaxPoints = 50

	// MaxPointsPairwise 逐对接口(步行/公交/混合出行)的上限。
	//
	// 为什么必须比 MaxPoints 小得多:这些路径没有批量接口,只能一对一发请求,
	// 每发一次还要歇 350ms 尊重 QPS 配额。算笔账(n 个点):
	//	 n=5  → 5×4=20 次 → 约  7 s
	//	 n=10 → 10×9=90 次 → 约 32 s    ← 教学规模的上限就在这
	//	 n=50 → 50×49=2450 次 → 约 14 min ← 浏览器早断了,网关也会掐
	// 限制必须按"最坏路径"算,不能按最好路径算:同一个 MaxPoints 用到
	// 逐对接口上,就是从"能用"变成"把服务卡死"。
	// （这正是 Phase 2 要把规划改成异步任务队列的直接动机。）
	// 混合出行时每段一次请求,同样走逐对路径,所以也受这个限制。
	MaxPointsPairwise = 10

	// webDirName 前端静态文件目录(相对进程工作目录)。
	webDirName = "web"
)

// isValidCoord 校验坐标是否在有效范围内(经度 -180~180,纬度 -90~90)
func isValidCoord(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

// isValidMode 校验出行方式是否合法
func isValidMode(mode string) bool {
	return mode == string(model.ModeDriving) ||
		mode == string(model.ModeWalking) ||
		mode == string(model.ModeTransit)
}

// parseMode 把请求里的出行方式字符串转成 model.Mode。
// 空字符串 = 调用方没传 → 默认驾车;其他非法值一律报错,绝不"猜一个"。
//
// 为什么单独抽出来:出行方式是"白名单参数",必须在校验层收口。
// 以前 /plan 只校验了 segments 里的方式,却漏了顶层的 mode —— 传个
// mode=cycling 进来,会被一路带到 amap 层报错、再被 matrix 层吞掉降级成
// 直线,最后返回 200。调用方拿到一个"看起来正常"的距离,完全不知道
// 自己拼错了参数。宁可在边界上 400,也不要返回一个悄悄算错的 200。
func parseMode(s string) (model.Mode, error) {
	if s == "" {
		return model.ModeDriving, nil
	}
	if !isValidMode(s) {
		return "", fmt.Errorf("出行方式无效: %q(支持 driving/walking/transit)", s)
	}
	return model.Mode(s), nil
}

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
	//
	// "web" 是相对路径,相对的是**进程的工作目录**,不是源码目录——
	// 从别的目录启动二进制,页面就会 404 而 API 全部正常,很难一眼看出来。
	// 所以启动时把解析后的绝对路径打出来,让这个问题在第一行日志就暴露。
	webDir, err := filepath.Abs(webDirName)
	if err != nil {
		webDir = webDirName
	}
	log.Printf("serving frontend from %s", webDir)
	r.NoRoute(gin.WrapH(http.FileServer(http.Dir(webDirName))))
	return r
}

type planReq struct {
	Origin       model.Point   `json:"origin"`
	Destinations []model.Point `json:"destinations"`
	Mode         string        `json:"mode"`     // driving|walking|transit,默认 driving
	Manual       bool          `json:"manual"`   // true = 按列表顺序,不跑 TSP
	Segments     []string      `json:"segments"` // 可选:每段的方式,如 ["walking","transit"]
}

type planResp struct {
	Order []string `json:"order"` // 访问顺序,按名字返回(给人看的)
	// OrderIdx 访问顺序的下标,指向 [origin] + destinations 拼成的 points 数组。
	//
	// 为什么必须额外给一份下标:名字不是标识符。用户搜两次"广州塔"就会有两个
	// 同名点,前端拿名字回查坐标只能查到第一个,画出来的路线就错了。
	// order 负责"给人看",order_idx 负责"机器用"——两者的职责别混。
	OrderIdx   []int    `json:"order_idx"`
	TotalKm    float64  `json:"total_km"`    // 沿该顺序走完全程的总距离
	Warnings   []string `json:"warnings"`    // 警告信息:如某段降级到直线距离
	Degraded   []string `json:"degraded"`    // 降级的路段(如 "广州塔→白云山")
	IsDegraded bool     `json:"is_degraded"` // 是否有任何降级
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

	points := append([]model.Point{req.Origin}, req.Destinations...)

	// —— 参数校验 1:出行方式白名单 ——
	// 放在最前面:它既是业务参数,也决定下面该用哪一档点数上限。
	mode, err := parseMode(req.Mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// —— 参数校验 2:点数限制(按"这次实际会走哪条路径"来定上限) ——
	// 混合出行和步行/公交都只能逐对发请求,上限必须收紧;
	// 只有"单一驾车"能吃到 v3/distance 批量接口的红利,才放开到 50。
	limit, pathDesc := MaxPoints, "驾车批量接口"
	if len(req.Segments) > 0 || mode != model.ModeDriving {
		limit, pathDesc = MaxPointsPairwise, "逐对请求接口"
	}
	if len(points) > limit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(
			"点数超过限制:最多 %d 个,当前 %d 个(%s,点数每多一个,请求量增长很快)",
			limit, len(points), pathDesc)})
		return
	}

	// —— 参数校验 3:坐标有效性 ——
	for _, p := range points {
		if !isValidCoord(p.Lat, p.Lng) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("坐标无效: %s (lat=%.6f, lng=%.6f)", p.Name, p.Lat, p.Lng)})
			return
		}
	}

	// —— 参数校验 4:segments 合法性 ——
	// 长度必须是"点数-1"(每两个相邻点之间一段),方式沿用同一套白名单。
	if len(req.Segments) > 0 {
		if len(req.Segments) != len(points)-1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("segments 长度错误: 应为 %d(点数-1)，实际 %d", len(points)-1, len(req.Segments))})
			return
		}
		for i, seg := range req.Segments {
			if _, err := parseMode(seg); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第 %d 段出行方式无效: %s (支持: driving/walking/transit)", i, seg)})
				return
			}
		}
	}

	order := make([]int, len(points))
	for i := range order {
		order[i] = i
	}

	var total float64
	var warnings []string
	var degraded []string

	if len(req.Segments) > 0 {
		// —— 混合出行:每段各自的方式 ——
		// 每段方式不同 → 没有统一的距离矩阵 → 无法做 TSP,
		// 顺序就是用户列表顺序,逐段算距离求和。
		if s.amap == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,混合出行不可用"})
			return
		}
		for i := 0; i < len(points)-1; i++ {
			km, err := s.amap.Distance(points[i], points[i+1], model.Mode(req.Segments[i]))
			if err != nil {
				// —— 部分失败容错:降级到直线距离 ——
				// 记录日志+降级路段,返回给前端显示警告
				segmentName := points[i].Name + "→" + points[i+1].Name
				log.Printf("[plan] 路段 %s 失败(%v), 降级到直线距离", segmentName, err)

				// 用 haversine 计算直线距离作为降级。
				// 复用 matrix 导出的实现,不再在 api 里复制一份公式。
				km = matrix.Haversine(points[i], points[i+1])

				warnings = append(warnings, fmt.Sprintf("路段 %s 的高德路线获取失败，已降级为直线距离", segmentName))
				degraded = append(degraded, segmentName)
			}
			total += km
		}
	} else {
		// —— 单一方式:全矩阵 + (可选)TSP ——
		// 注意这里接两个返回值:第二个是"这张矩阵是不是降级来的"。
		dists, degradedByMatrix := s.matrix.DistanceMatrix(points, mode)
		if degradedByMatrix {
			// 整张矩阵都是直线估算,没有"哪一段"可言,所以不进 degraded 路段列表,
			// 只给一条覆盖全线的警告。降级必须一路带到 HTTP 响应里——只写日志的话,
			// 用户在浏览器上看到的是一个毫无异常标记的公里数。
			log.Printf("[plan] 距离矩阵整体降级为直线, 已回传警告给调用方")
			warnings = append(warnings, "高德路网距离获取失败,本次总距离为直线估算,仅供参考")
		}
		if !req.Manual {
			// 模拟退火内部:最近邻粗解 → 2-opt 局部最优 → 跳出坑找更好的
			order = solver.SimulatedAnnealing(dists, 0)
		}
		total = solver.TourLength(dists, order)
	}

	resp := planResp{
		OrderIdx: order,
		TotalKm:  total,
		Warnings: warnings,
		Degraded: degraded,
		// 单一真相源:有警告 = 结果里含估算值。
		// 以前这里判断的是 len(degraded) > 0,而矩阵整体降级时 degraded 是空的,
		// 于是 is_degraded=false —— 前端把直线距离当真实路网展示给用户。
		// 换成 warnings 之后,任何一条降级路径都盖得住。
		IsDegraded: len(warnings) > 0,
	}
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
//
// 校验顺序有讲究:先校验参数,再看依赖是否可用。
// 参数错了是**调用方**的问题,不管服务端当下能不能干活,都该直说
// "你的参数有问题"(400);反过来先报 503,调用方会以为改成合法参数就行了,
// 实际上服务端确实也没配 key —— 两种错误混在一起,排查就得多绕一圈。
func (s *Server) route(c *gin.Context) {
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
	// 和 /plan 用同一个 parseMode:同一个参数,两个接口必须一套标准。
	// 以前这里没校验,mode=cycling 会被 RoutePolyline 静默吞成空数组返回 200。
	mode, err := parseMode(c.Query("mode"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if s.amap == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,路线不可用"})
		return
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
