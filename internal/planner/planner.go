// Package planner 把"一次规划"的业务规则收口在一个包里:参数校验(Validate) + 解算(Compute)。
//
// 为什么要抽出来:同步接口(POST /plan)和 Phase 2 的异步 worker 必须跑同一条解算链——
// 复制一份就是两个真相源,哪天改了算法,总有一处被忘掉(和 Haversine 当年复制两份是同一个教训)。
// api 层只负责 HTTP(bind / 状态码 / 路由),业务规则全在这里,谁调用都得到一样的结果。
package planner

import (
	"fmt"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/solver"
)

// MaxPoints 驾车(批量接口)的上限。驾车走 v3/distance,一次请求能算
// "所有点 → 某一个点"整列,所以 n 个点只要 n 次请求,50 点也就 50 次,扛得住。
const MaxPoints = 50

// MaxPointsPairwise 逐对接口(步行/公交/混合出行)的上限。
//
// 步行/公交没有批量接口,只能一对一发请求,每发一次还要歇 350ms 尊重 QPS 配额。
// 2026-09-17 起"解序"改用免费的直线预矩阵,不再为解序建全量矩阵 ——
// 真实路网请求只花在最终顺序的 n-1 条边上:
//
//	n=5  → 4 次 → 约 1.5 s
//	n=10 → 9 次 → 约 3.5 s
//	n=50 → 49 次 → 约 18 s  ← 仍是逐对,线性变慢,还叠加前端逐段画线的 n-1 次
//
// 请求量已从平方级降到线性,上限理论可以放宽;但"边界上的余量"改起来
// 牵动前端/文档/测试一整串,先不动数值,只把账算对(决策点 D4,同上 specs)。
const MaxPointsPairwise = 10

// PlanRequest 一次规划请求。三个地方共用同一个结构:
// api 绑定 JSON、Stream 里的任务载荷、tasks 表的 req_json —— 字段必须一字不差。
type PlanRequest struct {
	Origin       model.Point   `json:"origin"`
	Destinations []model.Point `json:"destinations"`
	Mode         string        `json:"mode"`     // driving|walking|transit,默认 driving
	Manual       bool          `json:"manual"`   // true = 按列表顺序,不跑 TSP
	Segments     []string      `json:"segments"` // 可选:每段的方式,如 ["walking","transit"]
}

// Result 一次规划的产出。JSON 字段名是前后端约定好的 wire contract,别改。
type Result struct {
	Order      []string `json:"order"`       // 访问顺序,按名字返回(给人看的)
	OrderIdx   []int    `json:"order_idx"`   // 给机器用的下标 —— 名字不是标识符
	TotalKm    float64  `json:"total_km"`    // 沿该顺序走完全程的总距离
	Warnings   []string `json:"warnings"`    // 警告信息:如某段降级到直线距离
	Degraded   []string `json:"degraded"`    // 降级的路段(如 "故宫→天坛")
	IsDegraded bool     `json:"is_degraded"` // 是否有任何降级
}

// ParseMode 把请求里的出行方式字符串转成 model.Mode。
// 空字符串 = 调用方没传 → 默认驾车;其他非法值一律报错,绝不"猜一个"。
// (从 api 层搬过来:白名单校验必须只有一个真相源,/plan 和 /route 共用。)
func ParseMode(s string) (model.Mode, error) {
	if s == "" {
		return model.ModeDriving, nil
	}
	if s != string(model.ModeDriving) &&
		s != string(model.ModeWalking) &&
		s != string(model.ModeTransit) {
		return "", fmt.Errorf("出行方式无效: %q(支持 driving/walking/transit)", s)
	}
	return model.Mode(s), nil
}

// isValidCoord 校验坐标是否在有效范围内(经度 -180~180,纬度 -90~90)
func isValidCoord(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

// Validate 做除 ParseMode 之外的全部参数校验。
// 错误文案与 HTTP 时代的 400 响应完全一致(有测试钉住,别随手改)。
// 为什么 worker 也要再校验一遍:任务从 Stream 里读回来时同样不可信,
// 边界上的校验从来只嫌少不嫌多 —— 而且它就是同一个函数,零成本。
func Validate(points []model.Point, mode model.Mode, segments []string) error {
	// origin + destinations:至少要有一个目的地
	if len(points) < 2 {
		return fmt.Errorf("至少需要一个目的地")
	}

	// 点数上限按"这次实际会走哪条路径"来定:
	// 混合出行和步行/公交只能逐对发请求,上限收紧;只有单一驾车吃批量接口红利。
	limit, pathDesc := MaxPoints, "驾车批量接口"
	if len(segments) > 0 || mode != model.ModeDriving {
		limit, pathDesc = MaxPointsPairwise, "逐对请求接口"
	}
	if len(points) > limit {
		return fmt.Errorf(
			"点数超过限制:最多 %d 个,当前 %d 个(%s,点数每多一个,请求量增长很快)",
			limit, len(points), pathDesc)
	}

	// 坐标有效性
	for _, p := range points {
		if !isValidCoord(p.Lat, p.Lng) {
			return fmt.Errorf("坐标无效: %s (lat=%.6f, lng=%.6f)", p.Name, p.Lat, p.Lng)
		}
	}

	// segments 长度必须是"点数-1"(每两个相邻点之间一段),方式沿用同一套白名单
	if len(segments) > 0 {
		if len(segments) != len(points)-1 {
			return fmt.Errorf("segments 长度错误: 应为 %d(点数-1)，实际 %d", len(points)-1, len(segments))
		}
		for i, seg := range segments {
			if _, err := ParseMode(seg); err != nil {
				return fmt.Errorf("第 %d 段出行方式无效: %s (支持: driving/walking/transit)", i, seg)
			}
		}
	}
	return nil
}

// Compute 跑一次完整解算,产出 Result。同步 handler 和异步 worker 都走这里。
//
// 参数 m 必须非 nil(装配层保证);am 可为 nil —— 此时混合出行不可用,
// 单一方式全部走 haversine(这是"设计如此",不是降级,详见 DistanceMatrix)。
// 单一方式的解算策略按模式分两条路(2026-09-17 起):
//   - 驾车:真实路网全矩阵(批量 n 次) → SA/manual 顺序 → 求和
//   - 步行/公交:直线预矩阵(0 API)解序 → 只对最终顺序的 n-1 条边查真路
func Compute(points []model.Point, mode model.Mode, manual bool, segments []string,
	m *matrix.Service, am *amap.Client) (Result, error) {

	var result Result
	var warnings []string
	var degraded []string
	n := len(points)

	if len(segments) > 0 {
		// —— 混合出行:每段各自的方式 ——
		// 每段方式不同 → 没有统一的距离矩阵 → 无法做 TSP,
		// 顺序就是用户列表顺序,逐段算距离求和。
		if am == nil || !am.HasAPIKey() {
			return Result{}, fmt.Errorf("未配置 AMAP_KEY,混合出行不可用")
		}
		for i := 0; i < n-1; i++ {
			km, err := am.Distance(points[i], points[i+1], model.Mode(segments[i]))
			if err != nil {
				// 部分失败容错:降级到直线距离,记日志 + 记降级路段,
				// 警告一路带到结果里(错得悄无声息是最坏的失败方式)。
				segmentName := points[i].Name + "→" + points[i+1].Name
				km = matrix.Haversine(points[i], points[i+1])
				warnings = append(warnings, fmt.Sprintf("路段 %s 的高德路线获取失败，已降级为直线距离", segmentName))
				degraded = append(degraded, segmentName)
			}
			result.TotalKm += km
		}
		result.OrderIdx = seqOrder(n)
	} else {
		// —— 单一方式 ——
		// 驾车和步行/公交走两条不同的路,分界线是"建真实矩阵要花多少请求":
		if mode == model.ModeDriving {
			// 驾车:批量接口建真实矩阵只要 n 次请求(且可并发),解序质量也更高,
			// 没必要换直线预矩阵 —— 维持原路径(决策点 D1,specs/2026-09-17-prematrix-sa.md)。
			// 第二个返回值 degraded 表示"这张矩阵是不是降级来的"。
			// 整张矩阵都是直线估算时没有"哪一段"可言,只给一条覆盖全线的警告。
			dists, degradedByMatrix := m.DistanceMatrix(points, mode)
			if degradedByMatrix {
				warnings = append(warnings, "高德路网距离获取失败,本次总距离为直线估算,仅供参考")
			}
			order := seqOrder(n)
			if !manual {
				// 模拟退火内部:最近邻粗解 → 2-opt 局部最优 → 跳出坑找更好的
				order = solver.SimulatedAnnealing(dists, 0)
			}
			result.TotalKm = solver.TourLength(dists, order)
			result.OrderIdx = order
		} else {
			// 步行/公交:没有批量接口,建全量矩阵要 n×(n-1) 次请求。
			// 改成"直线预矩阵解序(0 API) → 只对最终顺序的 n-1 条边查真路":
			// 解序质量略降(直线远近≠实走远近),换掉约 90% 的请求量,划算。
			dists := matrix.HaversineMatrix(points)
			order := seqOrder(n)
			if !manual {
				order = solver.SimulatedAnnealing(dists, 0)
			}
			result.OrderIdx = order
			if am == nil || !am.HasAPIKey() {
				// 没配 key:直线就是"本来该用的算法",设计如此,不算降级不报警告
				result.TotalKm = solver.TourLength(dists, order)
			} else {
				for k := 0; k+1 < n; k++ {
					a, b := points[order[k]], points[order[k+1]]
					km, err := am.Distance(a, b, mode)
					if err != nil {
						// 部分失败容错:与混合出行分支同一套降级范式 ——
						// 该段退直线、记路段名、警告带到结果,绝不悄悄错。
						segName := a.Name + "→" + b.Name
						km = dists[order[k]][order[k+1]]
						warnings = append(warnings, fmt.Sprintf("路段 %s 的高德路线获取失败，已降级为直线距离", segName))
						degraded = append(degraded, segName)
					}
					result.TotalKm += km
				}
			}
		}
	}

	// is_degraded 的单一真相源:有警告 = 结果里含估算值。
	// 任何一条降级路径(整张矩阵降级 / 某段降级)都盖得住。
	result.Order = make([]string, len(result.OrderIdx))
	for i, idx := range result.OrderIdx {
		result.Order[i] = points[idx].Name
	}
	result.Warnings = warnings
	result.Degraded = degraded
	result.IsDegraded = len(warnings) > 0
	return result, nil
}

// seqOrder 生成 [0,1,...,n-1]:用户列表顺序(混合出行 / manual 模式的顺序)
func seqOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}
