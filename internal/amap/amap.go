// Package amap 封装高德地图 Web 服务 API,回答两类问题:
//  1. "任意两点之间,按某种出行方式的真实距离是多少(公里)"
//  2. "某个关键词有哪些地点候选"(搜索)
//
// 出行方式对应不同的高德接口(实测确认):
//
//	driving → v3/distance(type=1 驾车,支持批量,逐列请求)
//	walking → v5/direction/walking(单对,逐对请求)
//	transit → v3/direction/transit/integrated(单对,逐对请求)
//
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
	"sync"
	"time"

	"awesomeProject/internal/cache"
	"awesomeProject/internal/model"
)

// 高德 Web 服务的根地址 + 本项目用到的各个接口路径。
//
// 以前这里是一个包级变量 baseURL,只有 v3/distance 能替换。两个问题:
//  1. 包级可变状态 = 全局状态,测试之间会互相干扰(谁改了没改回来就炸),
//     而且一旦有人加 t.Parallel() 就直接 race
//  2. 只能 fake 一个接口,想测"步行失败怎么办"就 fake 不了
//
// 改成"根地址做成 Client 的字段 + 路径常量化":每个实例各自持有一份,
// 测试里想指哪儿指哪儿,互不影响;顺带一眼看清这个项目到底用了高德哪几个接口。
const defaultRESTBase = "https://restapi.amap.com"

const (
	pathDistance   = "/v3/distance"                     // 驾车距离,支持批量(多起点→单终点)
	pathDrivingDir = "/v3/direction/driving"            // 驾车明细,带轨迹 polyline
	pathWalkingV5  = "/v5/direction/walking"            // 步行距离,数值准但不返回轨迹
	pathWalkingV3  = "/v3/direction/walking"            // 步行路线,有轨迹(所以画线用它)
	pathTransitDir = "/v3/direction/transit/integrated" // 公交换乘方案
	pathPlaceText  = "/v3/place/text"                   // 关键词搜地点
)

// Client 高德 Web 服务客户端。
// cache 字段可空:传 nil 表示不缓存(比如测试或想关掉缓存时)。
// key 通常静态传入;要"运行时换 key"(管理端热生效)就注入 keyFn,
// 每次请求现取 —— 两者都给时 keyFn 优先。
type Client struct {
	key      string
	keyFn    func() string // 动态 key 来源(settings.Provider),可空
	restBase string        // 高德 REST 根地址。测试里换成 httptest 假服务器
	http     *http.Client
	cache    cache.Cache
}

// NewClient 创建一个高德客户端。key 是 Web 服务 key(不是 JS key)。
// c 可为 nil。超时 5 秒:外部 API 必须设超时,否则一个慢请求能把
// 整个 /plan 卡死——这是"外部依赖要有边界"的基本功。
func NewClient(key string, c cache.Cache) *Client {
	return NewClientWithBase(key, defaultRESTBase, c)
}

// NewClientWithBase 和 NewClient 一样,但可以指定高德 REST 根地址。
//
// 为什么单独开一个构造器,而不是把 restBase 字段导出让外面直接赋值:
// 字段能被随意改,就没人知道"什么时候、在哪里被改过";
// 而一个构造器把"我要连哪儿"这件事显式写进调用点,读代码的人一眼看得见。
// 它的使用者只有一个——上层的集成测试(把请求指向本地假服务器);
// 生产代码永远走 NewClient,根地址固定是官方域名。
func NewClientWithBase(key, restBase string, c cache.Cache) *Client {
	return &Client{
		key:      key,
		restBase: restBase,
		http:     &http.Client{Timeout: 5 * time.Second},
		cache:    c,
	}
}

// url 拼出某个接口的完整地址。所有请求都走它,换地址只改一处。
func (c *Client) url(path string) string { return c.restBase + path }

// WithKeyFn 注入动态 key 来源。返回自身,方便链式装配。
// keyFn 会在每次发请求时被调用 —— 实现方自己负责缓存(Provider 有),
// 这里绝不缓存快照,否则"热生效"就成了一句空话。
func (c *Client) WithKeyFn(fn func() string) *Client {
	c.keyFn = fn
	return c
}

// apiKey 当前请求该用的 key。这是所有请求取 key 的**唯一**出口 ——
// 任何地方直接摸 c.key 都会让动态 key 失效,review 时盯紧这一条。
func (c *Client) apiKey() string {
	if c.keyFn != nil {
		return c.keyFn()
	}
	return c.key
}

// HasAPIKey 当前是否有可用的 key。
// 注意返回值是"此刻"的:管理端配了 key,下一个请求它就变 true ——
// 调用方(路由 handler)据此决定 503 还是干活,不要在启动时缓存这个结果。
func (c *Client) HasAPIKey() bool { return c.apiKey() != "" }

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

// maxColumnConcurrent 驾车矩阵"同时在途"的列请求数上限(信号量容量)。
//
// 为什么是 4,而不是"越多越好":并发度上限由 key 的 QPS 配额决定,不由核数决定。
// 高德对超配额请求返回 CUQPS_HAS_EXCEEDED_THE_LIMIT——开 50 个 goroutine
// 只会同时收到 50 个报错,并加速耗尽配额。
// 4 路并发 × 单请求约 300ms ≈ 峰值 13 QPS,对常见个人认证配额是安全的;
// 如果你的 key 在控制台「流量分析-配额管理」里的 QPS 更低,把它调小即可。
const maxColumnConcurrent = 4

// drivingMatrix 驾车矩阵:逐列批量(v3/distance type=1),列与列并发请求。
//
// ── 为什么这批工作适合并发 ──
// n 列彼此独立:第 j 列要的是"所有点到点 j 的距离",和第 k 列毫无交集。
// 每列 90% 的时间都在等网络回包——等的时候 CPU 闲着,完全可以去发下一列。
// 这是并发(I/O 重叠)最理想的形状:等待互相填满,而不是靠多核硬算。
//
// ── 但"能并发"不等于"无限制 go 出去",三个问题必须逐个回答 ──
//
//  1. 最多同时发几个? → 信号量限流,见 maxColumnConcurrent 的注释。
//
//  2. 谁写矩阵?是否存在竞争? → 每个 goroutine 只写**自己那一列** dists[i][j]
//     (j 固定,i 遍历)。不同 goroutine 碰的内存位置零交集,元素级并发写
//     在 Go 里是安全的,连锁都不用加。
//     两个前提,缺一不可:
//       a) 矩阵在外面已经 make 好,goroutine 里只做下标赋值、绝不 append——
//          append 可能触发扩容、整体搬迁底层数组,那才是真竞争;
//       b) 缓存写入(c.store)走 cache.Cache,Memory 实现自带 sync.Mutex,
//          不然并发写 map 会直接 fatal(不是能 recover 的 panic)。
//
//  3. 某一列失败了怎么办? → 矩阵不完整,必须整体报错,让上层 matrix
//     降级到 haversine——绝不能返回"半张真实半张直线"的混搭矩阵。
//     多列可能同时失败,用互斥锁只记**第一个**错误:错误信息要有"第一个赢家",
//     后来的直接丢弃,否则报错内容互相覆盖,排查时看到的永远不确定是哪个。
func (c *Client) drivingMatrix(points []model.Point) ([][]float64, error) {
	n := len(points)
	dists := make([][]float64, n)
	for i := range dists {
		dists[i] = make([]float64, n)
	}

	// 信号量 = 带缓冲的 channel,容量就是"同时最多几路"。
	// 发送成功 = 占到一个名额;接收 = 归还名额。
	// 为什么用 channel 而不是"计数器 + 锁":channel 的阻塞语义天然形成
	// "排队等名额"——池子满了发送方就停,不用自己写"满了怎么办"的判断。
	// 这就是 Go 说的"不要通过共享内存来通信,要通过通信来共享内存"。
	sem := make(chan struct{}, maxColumnConcurrent)

	var wg sync.WaitGroup // 计数器:还差几个 goroutine 没收工
	var errMu sync.Mutex  // 只保护 firstErr 这一个变量——锁的粒度越小越好
	var firstErr error

	for j := 0; j < n; j++ {
		// 缓存查询留在主 goroutine 里做:命中的直接填进矩阵,
		// 不进并发池、不发请求、不占信号量名额。
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
			continue // 这列全命中缓存,连 goroutine 都不用开
		}

		// 先占名额,再开 goroutine:池子满了这里会阻塞——这正是要的
		// "背压"效果:主 goroutine 停下来等,而不是让无限多的请求堆积。
		// 注意占名额在主 goroutine、还名额在子 goroutine,一进一出刚好配对。
		sem <- struct{}{}
		wg.Add(1)
		go func(j int, origins []string, idxs []int) {
			// 两个 defer 逆序执行:先还名额,再通知 WaitGroup——
			// 顺序其实无所谓,但必须用 defer,保证 return / panic 都逃不掉,
			// 否则一个 panic 就能让 wg.Wait() 永远等下去(goroutine 泄漏的常见来源)。
			defer wg.Done()
			defer func() { <-sem }()

			col, err := c.fetchColumn(origins, points[j])
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = wrapColumnErr(j, err)
				}
				errMu.Unlock()
				return // 这列废了,矩阵注定不完整,别再填数据
			}
			for k, i := range idxs {
				dists[i][j] = col[k] // 只写第 j 列,见函数注释第 2 点
				c.store(cacheKey(model.ModeDriving, points[i], points[j]), col[k]) // 写缓存,下次直接命中
			}
		}(j, origins, idxs)
		// 为什么把 j/origins/idxs 显式传参而不是闭包直接捕获?
		// Go 1.22 起循环变量每轮都是新实例,捕获也安全;显式传参把
		// "这个 goroutine 用的是这一轮的值"写在签名里,不依赖对
		// Go 版本语义差异的记忆——并发代码里的隐式知识越少越好。
	}

	// 阻塞到所有列收工。此刻 dists 要么完整,要么 firstErr 非空。
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return dists, nil
}

// wrapColumnErr 把某一列的失败包装成带上下文的错误。
// 对限流类错误(CUQPS_HAS_EXCEEDED_THE_LIMIT,并发改造后最可能撞上的)
// 附加可操作的提示:错误信息要指明调整哪个常量,而不是只给一个英文码。
func wrapColumnErr(col int, err error) error {
	if strings.Contains(err.Error(), "CUQPS") {
		return fmt.Errorf("第 %d 列: %w(高德限流:并发超过了 key 的 QPS 配额,把 amap.go 的 maxColumnConcurrent 调小)", col, err)
	}
	return fmt.Errorf("第 %d 列: %w", col, err)
}

// pairwiseMatrix 逐对矩阵(walking/transit):没有批量接口,只能一对一发请求。
// 注意两点:
//  1. 高德个人 key 的 QPS 限制很严(实测步行约 3/s),连发会返回
//     CUQPS_HAS_EXCEEDED_THE_LIMIT,请求之间必须节流
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
			// 节流:仅在实际发了请求后等待,缓存命中不消耗配额、不等待。
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
		apiURL = c.url(pathDrivingDir)
	case model.ModeWalking:
		apiURL = c.url(pathWalkingV5)
	case model.ModeTransit:
		apiURL = c.url(pathTransitDir)
	default:
		return 0, fmt.Errorf("unsupported mode %q", mode)
	}

	q := url.Values{}
	q.Set("key", c.apiKey())
	q.Set("origin", coord(a)) // v5/v3 单对接口用 origin/destination
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
	q.Set("key", c.apiKey())
	q.Set("type", "1") // 1 = 驾车导航距离,按真实路网
	q.Set("origins", strings.Join(origins, "|"))
	q.Set("destination", coord(dest)) // 单终点——实测批量只认这种

	// 外部调用打日志是生产惯例:能看清每次请求,排查配额/延迟全靠它。
	log.Printf("[amap] driving %d origins -> %s", len(origins), coord(dest))

	resp, err := c.http.Get(c.url(pathDistance) + "?" + q.Encode())
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
//
//	driving → v3/direction/driving,steps[].polyline 有轨迹
//	walking → v5 步行不返回轨迹坐标,要用 v3/direction/walking
//	transit → 公交由"步行段+公交段"组成,轨迹不连续,返回 nil 让前端画直线
func (c *Client) RoutePolyline(a, b model.Point, mode model.Mode) ([][2]float64, error) {
	var apiURL string
	// (nil, nil) 和"报错"是两件不同的事,别混:
	//   - transit:(nil, nil) 表示"这个出行方式本来就没有连续轨迹",是正常的业务事实
	//   - 未知 mode:报错。以前这里也吞成 (nil, nil),结果 /route?mode=cycling
	//     会返回 200 + 空数组,前端默默画了条直线——把"我拼错了参数"伪装成"正常返回空"
	switch mode {
	case model.ModeDriving:
		apiURL = c.url(pathDrivingDir)
	case model.ModeWalking:
		apiURL = c.url(pathWalkingV3)
	case model.ModeTransit:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported mode %q for polyline", mode)
	}

	q := url.Values{}
	q.Set("key", c.apiKey())
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
	q.Set("key", c.apiKey())
	q.Set("keywords", keyword)
	q.Set("offset", strconv.Itoa(limit))
	q.Set("page", "1")
	if city != "" {
		// 注意:citylimit 只在 city 有值时才有意义,全国搜索就别传
		q.Set("city", city)
		q.Set("citylimit", "true")
	}

	resp, err := c.http.Get(c.url(pathPlaceText) + "?" + q.Encode())
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
		lng, lat, err := parseCoord(p.Location)
		if err != nil {
			continue // 跳过无效数据
		}
		places = append(places, model.Place{Name: p.Name, Address: p.Address, Lat: lat, Lng: lng})
	}
	return places, nil
}

// parseCoord 解析 "lng,lat" 字符串成坐标,返回 (lng, lat, error)。
// 统一处理坐标字符串解析逻辑,避免在多处重复。
// 注意:高德格式是"经度,纬度"(lng 在前),和 model.Point 字段顺序相反。
func parseCoord(s string) (lng, lat float64, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("需要 lng,lat 格式")
	}
	lng, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("经度不是数字: %v", err)
	}
	lat, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("纬度不是数字: %v", err)
	}
	return lng, lat, nil
}

// coord 把点拼成高德要求的 "经度,纬度"。注意顺序:经度在前,和
// model.Point 的字段顺序(Lat, Lng)相反,别写反了——这是调外部 API 的经典错误。
func coord(p model.Point) string {
	return strconv.FormatFloat(p.Lng, 'f', 6, 64) + "," +
		strconv.FormatFloat(p.Lat, 'f', 6, 64)
}
