package matrix

import (
	"log"
	"math"

	"awesomeProject/internal/geo"
	"awesomeProject/internal/model"
)

// Service 负责回答"任意两点间距离是多少"(距离矩阵)。
// 距离来源是可替换的(geo.Source):装配了来源就用真实路网距离,
// 来源失败或没装配时自动降级回 haversine 直线距离,调用方无感知。
// 这就是"面向接口/依赖倒置":api 和 solver 只关心能拿到矩阵,不关心距离从哪来。
type Service struct {
	src geo.Source // 可空:纯直线模式传 nil
}

func New() *Service { return &Service{} }

// NewWithSource 创建优先使用真实路网距离的服务。src 不能为 nil。
func NewWithSource(src geo.Source) *Service { return &Service{src: src} }

// DistanceMatrix 返回 n×n 矩阵,dists[i][j] = 点 i 到 j 的距离(公里,按 mode 出行)。
// 优先高德;任何一步出错就整体降级 haversine——矩阵要么全真实路网,
// 要么全直线,不会出现"一半真实一半直线"的混搭结果。
//
// 第二个返回值 degraded 表示"这张矩阵是不是降级来的",必须由上层一路带到
// HTTP 响应里。降级不能只写日志:日志对用户不可见,直线估算值必须显式告知。
func (s *Service) DistanceMatrix(points []model.Point, mode model.Mode) ([][]float64, bool) {
	// HasAPIKey 是"此刻"的判断:来源没装配时走直线(设计如此,不算降级),
	// 装配后同一进程的下一次请求即走真实路网。
	if s.src != nil && s.src.HasAPIKey() {
		dists, err := s.src.DistanceMatrix(points, mode)
		if err == nil {
			return dists, false
		}
		// 配了来源但调用失败
		log.Printf("[matrix] source %s failed (%v), falling back to haversine", mode, err)
		return haversineMatrix(points), true
	}
	// 没配 key:直线距离即预期行为,不产生降级警告。
	// 区分"设计如此"和"出故障",两者的告警语义不同。
	return haversineMatrix(points), false
}

// HaversineMatrix 全量直线距离矩阵(公里)。
// 导出给 planner 做"预矩阵":步行/公交的解序先用免费的直线距离跑完,
// 真实路网请求只花在最终顺序的相邻边上 —— 解序 0 API,见
// specs/2026-09-17-prematrix-sa.md。
func HaversineMatrix(points []model.Point) [][]float64 {
	return haversineMatrix(points)
}

// haversineMatrix 全量直线距离矩阵。预先计算
func haversineMatrix(points []model.Point) [][]float64 {
	n := len(points)
	dists := make([][]float64, n)
	for i := range dists {
		dists[i] = make([]float64, n)
		for j := range dists[i] {
			dists[i][j] = haversine(points[i].Lat, points[i].Lng, points[j].Lat, points[j].Lng)
		}
	}
	return dists
}

// Haversine 两点球面距离(公里),导出给 api 层复用。
// api 的"混合出行某段失败"降级也要用同一个公式:同一公式只能有一份实现,
// 复制会产生两个真相源,修改时必然漏掉一处。
func Haversine(a, b model.Point) float64 {
	return haversine(a.Lat, a.Lng, b.Lat, b.Lng)
}

// haversine 球面距离(公里)。高德失败时的降级方案。
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0 // 地球半径,公里
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}
