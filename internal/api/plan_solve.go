// plan_solve.go —— MCP 主链路的纯计算端点：前端取回路网矩阵，后端只负责解序。
//
// 设计动机（MCP 优先改造）：用户绑定自己的地图 MCP 后，距离矩阵由用户设备
// 直连厂商获取（key 不出浏览器），TSP 解序仍是后端 solver 包的单一真相源。
// 这个端点不碰任何外部服务、不需要任何 key —— 纯内存计算。
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
	"awesomeProject/internal/solver"
)

type solveRequest struct {
	Points []model.Point `json:"points"`
	Mode   string        `json:"mode"`
	Matrix [][]float64   `json:"matrix"`
}

func (s *Server) solveMatrix(c *gin.Context) {
	var req solveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mode, err := planner.ParseMode(req.Mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := planner.Validate(req.Points, mode, nil); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n := len(req.Points)
	if len(req.Matrix) != n {
		c.JSON(http.StatusBadRequest, gin.H{"error": "矩阵维度与点数不符"})
		return
	}
	for _, row := range req.Matrix {
		if len(row) != n {
			c.JSON(http.StatusBadRequest, gin.H{"error": "矩阵必须是方阵"})
			return
		}
	}

	order := solver.SimulatedAnnealing(req.Matrix, 0)
	totalKm := 0.0
	for i := 0; i < len(order)-1; i++ {
		totalKm += req.Matrix[order[i]][order[i+1]]
	}
	ordered := make([]model.Point, 0, n)
	names := make([]string, 0, n)
	for _, idx := range order {
		ordered = append(ordered, req.Points[idx])
		names = append(names, req.Points[idx].Name)
	}
	c.JSON(http.StatusOK, gin.H{
		"order_idx": order,
		"order":     names,
		"total_km":  totalKm,
	})
}
