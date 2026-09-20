package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/auth"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
	"awesomeProject/internal/settings"
)

// webDirName 前端静态文件目录(相对进程工作目录)
const webDirName = "web"

// Server 持有 HTTP 层需要的依赖。所有业务逻辑都藏在别的包里,这里只做编排。
// repo / queue 是 Phase 2 的异步任务依赖,可为 nil —— 没配 MYSQL_DSN 时
// 异步路径不注册,同步的 /plan 照常工作(和 amap 可空是同一个思路)。
// auth / settings 是登录与 key 中心化的依赖(Phase 3),可为 nil ——
// nil 时 /auth/* 与 /config/public 不注册,游客/静态 key 模式照常。
type Server struct {
	matrix   *matrix.Service
	amap     *amap.Client       // 不再为 nil:动态 key 后客户端常驻,用 HasAPIKey 判断可用性
	repo     repo.TaskRepo      // 可空:nil = 异步 /plans 不可用
	queue    *queue.Queue       // 可空:同上
	auth     *auth.Service      // 可空:nil = 不注册 /auth/*
	settings *settings.Provider // 可空:nil = 不注册 /config/public
	// envFallbacks 配置名 → env 兜底值。管理端回显"来源"时用:
	// handler 不直接读环境变量(集中到 config.Load 是既有约定)
	envFallbacks map[string]string
}

// Option 装配选项(FUNCTIONAL OPTIONS 模式):新增可选依赖时,
// 老调用方(NewRouter 的既有测试)一行不用改。
type Option func(*Server)

// WithAuth 挂认证服务:注册 /auth/* 路由 + 全局 OptionalAuth。
func WithAuth(a *auth.Service) Option { return func(s *Server) { s.auth = a } }

// WithSettings 挂配置中心:注册 /config/public(JS key 下发)。
func WithSettings(p *settings.Provider) Option { return func(s *Server) { s.settings = p } }

// WithEnvFallbacks 提供配置名 → env 兜底值的映射(管理端回显来源用)。
func WithEnvFallbacks(f map[string]string) Option {
	return func(s *Server) { s.envFallbacks = f }
}

// amapReady 服务此刻能不能调高德。动态 key 后"有没有 key"是运行时状态,
// 每个请求现查,绝不缓存到启动时 —— 那会把热生效变成"重启生效"。
func (s *Server) amapReady() bool { return s.amap != nil && s.amap.HasAPIKey() }

// protected 返回业务接口的注册面:auth 已装配时带 RequireAuth(API 级登录墙),
// 未装配时退化为无保护 —— 降级语义的唯一开关点。
// 为什么用 Group("/"):gin 的组会继承引擎上的 OptionalAuth(先认人再拦截),顺序正好。
func (s *Server) protected(router *gin.Engine) gin.IRouter {
	if s.auth != nil {
		return router.Group("/", s.auth.RequireAuth())
	}
	return router.Group("/")
}

// NewRouter 组装路由。gin.Engine 就是"标准库 ServeMux + 中间件"的增强版,
// gin.Default() 自带日志和 panic 恢复两个中间件。
//
// repo / queue 传 nil 表示"本进程没装配异步任务链路",此时不注册 /plans。
// opts 是可选拼装(认证/配置中心),不传 = 老行为。
func NewRouter(m *matrix.Service, am *amap.Client, r repo.TaskRepo, q *queue.Queue,
	opts ...Option) *gin.Engine {
	s := &Server{matrix: m, amap: am, repo: r, queue: q}
	for _, opt := range opts {
		opt(s)
	}

	router := gin.Default()
	// 认证中间件放最外层:它只是"尽力认人",游客照常通过 ——
	// 真正拦截的是 protected() 组上的 RequireAuth
	if s.auth != nil {
		router.Use(s.auth.OptionalAuth())
	}
	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	// p = 业务接口的注册面。auth 已装配时它带着 RequireAuth ——
	// 登录墙从"页面层"下沉到"API 层":curl 绕过前端也打不进业务接口。
	// auth 未装配(没配 PG_DSN)时退化为无保护 —— 服务不能因为没配库就全锁死,
	// 降级语义的开关收在这一个函数里,别处不出现第二个 if。
	p := s.protected(router)
	p.POST("/plan", s.plan)
	p.GET("/search", s.search)
	p.GET("/route", s.route)
	// 异步任务接口只在装配了存储 + Stream 时注册:
	// 未部署的功能不注册路由,404 比 503 更能准确表达"功能不存在"。
	if s.repo != nil && s.queue != nil {
		p.POST("/plans", s.createPlan)
		p.GET("/plans/:id", s.getPlan)
	}
	// 登录/注册/当前用户,全部公开:
	// register/login 是登录墙本体,锁了就没人能进来;
	// me 未登录返回 401 是正常语义(前端靠它探测登录态);
	// logout 幂等,未登录调用无害。
	if s.auth != nil {
		router.POST("/auth/register", s.register)
		router.POST("/auth/login", s.login)
		router.POST("/auth/logout", s.logout)
		router.GET("/auth/me", s.me)
	}
	// JS key 下发:浏览器渲染地图用(JS key 天生公开,保护它不减少任何暴露面),
	// 登录页自身也不需要它。settings 未装配 = 静态 key 模式,前端自己配
	if s.settings != nil {
		router.GET("/config/public", s.publicConfig)
	}
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
	router.NoRoute(gin.WrapH(http.FileServer(http.Dir(webDirName))))
	return router
}

// plan 同步规划:一次 HTTP 请求里做完 校验 → 解算 → 返回。
// 业务规则(参数校验 + 解算)全在 planner 包,这里只做 HTTP 编排;
// 异步 worker 跑的是同一个 planner.Compute,两条路径永远算出同样的东西。
func (s *Server) plan(c *gin.Context) {
	var req planner.PlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	points := append([]model.Point{req.Origin}, req.Destinations...)

	// 校验顺序:先参数(400),再看依赖(503)——和 /route 同一个原则。
	mode, err := planner.ParseMode(req.Mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := planner.Validate(points, mode, req.Segments); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 混合出行没有 haversine 退路(每段方式不同,直线估算没法做),没 key 直接 503。
	if !s.amapReady() && len(req.Segments) > 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,混合出行不可用"})
		return
	}

	result, err := planner.Compute(points, mode, req.Manual, req.Segments, s.matrix, s.amap)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// createPlan 异步规划的"下单":校验 → 落库(queued) → 任务进 Stream → 立刻返回 id。
// 整个 handler 毫秒级完成 —— 14 分钟的解算由后台 worker 做(见 internal/worker)。
func (s *Server) createPlan(c *gin.Context) {
	var req planner.PlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	points := append([]model.Point{req.Origin}, req.Destinations...)

	// 提交时就做完整校验:参数错了让用户立刻改,而不是几分钟后 worker 报 failed。
	// (worker 收到任务后还会再校验一遍 —— 任务载荷和 HTTP 请求一样不可信。)
	mode, err := planner.ParseMode(req.Mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := planner.Validate(points, mode, req.Segments); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !s.amapReady() && len(req.Segments) > 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,混合出行不可用"})
		return
	}

	// req_json 存原始请求:worker 挂了重做、历史回放,都要靠它
	reqJSON, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "请求序列化失败: " + err.Error()})
		return
	}
	id, err := s.repo.Create(c.Request.Context(), reqJSON)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务落库失败: " + err.Error()})
		return
	}
	// Stream 载荷只需要 task_id:任务详情在库里,worker 拿 id 再查 ——
	// 消息里塞大 JSON 是常见反模式(队列应该轻,DB 才是事实源)。
	if err := s.queue.Add(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务入队失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plan_id": id, "status": "queued"})
}

// getPlan 查任务状态/结果:前端轮询这个接口。
// status=queued → 继续等;done → result 里有完整结果;failed → error 说明原因。
func (s *Server) getPlan(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan_id 必须是数字"})
		return
	}
	task, err := s.repo.Get(c.Request.Context(), id)
	if err != nil {
		if err == repo.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务失败: " + err.Error()})
		return
	}
	resp := gin.H{
		"plan_id":    task.ID,
		"status":     task.Status,
		"created_at": task.CreatedAt,
		"updated_at": task.UpdatedAt,
	}
	// done 才带 result,failed 才带 error —— 字段缺席比 null 更不容易被误用
	if task.Status == "done" {
		resp["result"] = json.RawMessage(task.ResultJSON)
	}
	if task.Status == "failed" {
		resp["error"] = task.Error
	}
	c.JSON(http.StatusOK, resp)
}

// search 是"地名 → 候选地点"的代理接口:前端传关键词,我们调高德搜索,
// key 藏在后端(前端不碰第三方凭据),以后还能给搜索加缓存/限流。
func (s *Server) search(c *gin.Context) {
	if !s.amapReady() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置 AMAP_KEY,搜索不可用"})
		return
	}
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 q 参数"})
		return
	}
	// city 可空:不传为全国搜索,传了(如"杭州")则限定该城市。
	places, err := s.amap.SearchPlaces(q, c.Query("city"), 6)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "高德搜索失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"places": places})
}

// route 是"两点之间的真实路网轨迹"代理接口:前端画路线用。
// 与 /search 相同:前端不直接调高德,轨迹也由后端代理。
//
// 校验顺序:先校验参数(400),再看依赖是否可用(503)。
// 参数错误是调用方的问题,与依赖可用性无关;若依赖不可用先返回 503,
// 调用方会误以为修改参数能解决问题,两类故障混在一起会延长排查。
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
	// 和 /plan 用同一个 ParseMode:同一个参数,两个接口必须一套标准。
	mode, err := planner.ParseMode(c.Query("mode"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !s.amapReady() {
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
