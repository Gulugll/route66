// Package geo 定义"外部地理数据来源"的统一形状。
//
// amap.Client 与 valhalla.Client 都实现 Source:上层(matrix / planner / api /
// worker / agent)只依赖接口,换距离来源零改动 —— 依赖倒置的收口点。
// 方法面刻意与 amap.Client 的既有公开方法一致,amap 不需要任何修改。
package geo

import "awesomeProject/internal/model"

// Source 一家"距离 + 轨迹 + 搜索"的地理数据提供方。
type Source interface {
	// DistanceMatrix n×n 距离矩阵(公里),dists[i][j] = 点 i 到点 j。
	// 不支持的方式/上游故障返回 error,降级策略由 matrix.Service 统一决定。
	DistanceMatrix(points []model.Point, mode model.Mode) ([][]float64, error)

	// Distance 两点间距离(公里),混合出行与步行/公交的逐段查询走这里。
	Distance(a, b model.Point, mode model.Mode) (float64, error)

	// RoutePolyline 两点间路网轨迹([lng,lat] 数组,经度在前)。
	// 公交无连续轨迹时返回 (nil, nil) —— 是业务事实,不是错误。
	RoutePolyline(a, b model.Point, mode model.Mode) ([][2]float64, error)

	// SearchPlaces 关键词搜索地点候选。不提供搜索的来源返回 error。
	SearchPlaces(keyword, city string, limit int) ([]model.Place, error)

	// HasAPIKey 此刻是否具备调用能力。key 型来源(高德)返回"key 是否已配置",
	// 自托管来源(Valhalla)装配即 true;请求级故障一律走 error + 降级。
	HasAPIKey() bool
}
