package model

// Mode 出行方式。不同的方式走高德不同的接口,距离/耗时都不同:
//   driving → v3/distance(type=1 驾车,支持批量)
//   walking → v5/direction/walking(逐对)
//   transit → v3/direction/transit/integrated(逐对)
// 注意:cycling(骑行)对个人开发者 key 返回 RESOURCE_UNAVAILABLE,暂不支持。
type Mode string

const (
	ModeDriving Mode = "driving"
	ModeWalking Mode = "walking"
	ModeTransit Mode = "transit"
)

// Point 一个地点:名字 + 坐标。名字用于返回结果时让人看懂,坐标用于算距离。
type Point struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// Place 搜索返回的地点候选(前端选一个转成 Point 添加)。
type Place struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}
