package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/solver"
)

// Server 持有 HTTP 层需要的依赖。所有业务逻辑都藏在别的包里,这里只做编排。
type Server struct {
	matrix *matrix.Service
}

// NewRouter 组装路由。gin.Engine 就是"标准库 ServeMux + 中间件"的增强版,
// gin.Default() 自带日志和 panic 恢复两个中间件。
func NewRouter(m *matrix.Service) *gin.Engine {
	s := &Server{matrix: m}

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.POST("/plan", s.plan)
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

	points := append([]model.Point{req.Origin}, req.Destinations...)
	dists := s.matrix.DistanceMatrix(points)
	// 模拟退火内部:最近邻粗解 → 2-opt 局部最优 → 跳出坑找更好的
	order := solver.SimulatedAnnealing(dists, 0)

	resp := planResp{
		TotalKm: solver.TourLength(dists, order),
	}
	for _, i := range order {
		resp.Order = append(resp.Order, points[i].Name)
	}
	c.JSON(http.StatusOK, resp)
}
