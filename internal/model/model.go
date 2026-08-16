package model

// Point 一个地点:名字 + 坐标。名字用于返回结果时让人看懂,坐标用于算距离。
type Point struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}
