package matrix

import (
	"log"
	"math"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/model"
)

// Service 负责回答"任意两点间距离是多少"(距离矩阵)。
// 距离来源是可替换的:有高德客户端就优先用真实路网距离(带缓存),
// 高德失败或没配 key 时自动降级回 haversine 直线距离,调用方无感知。
// 这就是"面向接口/依赖倒置":api 和 solver 只关心能拿到矩阵,不关心距离从哪来。
type Service struct {
	amap *amap.Client // 可空:没 key 或想纯直线时传 nil
}

func New() *Service { return &Service{} }

// NewWithAmap 创建优先使用高德路网距离的服务。c 不能为 nil。
func NewWithAmap(c *amap.Client) *Service { return &Service{amap: c} }

// DistanceMatrix 返回 n×n 矩阵,dists[i][j] = 点 i 到 j 的距离(公里,按 mode 出行)。
// 优先高德;任何一步出错就整体降级 haversine——矩阵要么全真实路网,
// 要么全直线,不会出现"一半真实一半直线"的混搭结果。
func (s *Service) DistanceMatrix(points []model.Point, mode model.Mode) [][]float64 {
	if s.amap != nil {
		if dists, err := s.amap.DistanceMatrix(points, mode); err == nil {
			return dists
		} else {
			// 降级要可见:打日志,别静默。线上排查全靠这种日志。
			log.Printf("[matrix] amap %s failed (%v), falling back to haversine", mode, err)
		}
	}
	return haversineMatrix(points)
}

// haversineMatrix 全量直线距离矩阵。
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
