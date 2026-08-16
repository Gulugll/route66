package matrix

import (
	"math"

	"awesomeProject/internal/model"
)

// Service 负责回答"任意两点间距离是多少"(距离矩阵)。
// 当前:全部用 haversine 直线距离,先把流程跑通。
// 下一阶段:优先高德矩阵 API(真实路网距离)+ Redis 缓存,失败时降级回 haversine。
type Service struct{}

func New() *Service { return &Service{} }

// DistanceMatrix 返回 n×n 矩阵,dists[i][j] = 点 i 到 j 的距离(公里)。
// api 和 solver 都不关心距离从哪来,只关心能拿到矩阵——这就是低耦合。
func (s *Service) DistanceMatrix(points []model.Point) [][]float64 {
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

// haversine 球面距离(公里)。高德失败时的降级方案,当前阶段也是唯一方案。
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0 // 地球半径,公里
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}
