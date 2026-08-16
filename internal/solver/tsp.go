package solver

import (
	"math"
	"math/rand"
	"slices"
)

// NearestNeighbor 最近邻算法:从 start 出发,每一步去"最近的还没去过的点"。
// dists[i][j] 是 i 到 j 的距离矩阵(由 matrix 提供,解算器不关心距离从哪来)。
// 返回访问顺序的下标列表,如 [0 2 1 3]。
//
// 速度快,但贪心——只看眼前,可能画出交叉的远路。是 2-opt / 模拟退火的粗起点。
func NearestNeighbor(dists [][]float64, start int) []int {
	n := len(dists)
	if n == 0 {
		return nil
	}
	visited := make([]bool, n)
	order := make([]int, 0, n)
	order = append(order, start)
	visited[start] = true

	cur := start
	for len(order) < n {
		best, bestDist := -1, math.MaxFloat64
		for j := 0; j < n; j++ {
			if visited[j] {
				continue
			}
			if d := dists[cur][j]; d < bestDist {
				bestDist = d
				best = j
			}
		}
		order = append(order, best)
		visited[best] = true
		cur = best
	}
	return order
}

// TwoOpt 2-opt 局部搜索:反复"断开两条边、反转中间一段、重连"。
// 选两条边 (i,i+1) 和 (j,j+1),换成 (i,j) 和 (i+1,j+1) 等价于反转 order[i+1..j]。
// 只接受能缩短路径的换法,循环到没有改进为止 → 局部最优。
func TwoOpt(dists [][]float64, order []int) []int {
	if len(order) < 4 {
		return order
	}
	order = append([]int(nil), order...) // 拷贝一份,不改调用方的切片
	n := len(order)
	improved := true
	for improved {
		improved = false
		for i := 0; i < n-1; i++ {
			for j := i + 1; j < n-1; j++ {
				old := dists[order[i]][order[i+1]] + dists[order[j]][order[j+1]]
				alt := dists[order[i]][order[j]] + dists[order[i+1]][order[j+1]]
				if alt < old {
					slices.Reverse(order[i+1 : j+1])
					improved = true
				}
			}
		}
	}
	return order
}

// SimulatedAnnealing 模拟退火:从"最近邻 + 2-opt"的局部最优出发,
// 随机反转一段;变好就接受,变差也按 exp(-Δ/temp) 的概率接受,
// 从而跳出局部最优。温度从 1000 指数降到 0.1,越到后面越"冷静"。
// 返回全程见过的最优顺序。
func SimulatedAnnealing(dists [][]float64, start int) []int {
	if len(dists) < 2 {
		return []int{start}
	}
	order := TwoOpt(dists, NearestNeighbor(dists, start))
	if len(order) < 4 {
		return order
	}
	best := append([]int(nil), order...)
	bestLen := TourLength(dists, best)
	curLen := bestLen
	n := len(order)

	for temp := 1000.0; temp > 0.1; temp *= 0.995 {
		for k := 0; k < 200; k++ {
			// 随机选两条边 (i,i+1) 和 (j,j+1),j > i 且 j 不是最后一点(开放路径)
			i := rand.Intn(n - 2)
			j := i + 1 + rand.Intn(n-2-i)

			old := dists[order[i]][order[i+1]] + dists[order[j]][order[j+1]]
			alt := dists[order[i]][order[j]] + dists[order[i+1]][order[j+1]]
			delta := alt - old

			// 变好就收;变差则以 exp(-Δ/temp) 概率收(温度越高越敢收差解)
			if delta < 0 || rand.Float64() < math.Exp(-delta/temp) {
				slices.Reverse(order[i+1 : j+1])
				curLen += delta
				if curLen < bestLen {
					bestLen = curLen
					best = append([]int(nil), order...)
				}
			}
		}
	}
	return best
}

// TourLength 一条顺序的总距离:相邻两点距离之和(公里)。
func TourLength(dists [][]float64, order []int) float64 {
	total := 0.0
	for k := 0; k+1 < len(order); k++ {
		total += dists[order[k]][order[k+1]]
	}
	return total
}
