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
//
// 两个关键细节(2026-09-17 修,详见 TestAsymmetricThreePointTrap):
//  1. j 允许取到 n-1:反转段可以贴到路径末端。否则 n=3 时邻域里
//     只有"反转单个元素"= 恒等变换,顺序根本动不了(SA 退化成纯最近邻)。
//  2. 好坏判据用**精确的 TourLength 差**,不套"边界两条边"的简化公式:
//     那个公式默认反转段内部边不变,对对称矩阵成立,但驾车真实路网
//     **不对称**(单行道/禁转),最优收益恰恰可能藏在内部边的方向翻转里。
//     代价是每次评估 O(n),n≤50 的教学规模毫秒级,换正确性值得。
func TwoOpt(dists [][]float64, order []int) []int {
	if len(order) < 2 {
		return order
	}
	order = append([]int(nil), order...) // 拷贝一份,不改调用方的切片
	n := len(order)
	curLen := TourLength(dists, order)
	improved := true
	for improved {
		improved = false
		for i := 0; i < n-1; i++ {
			for j := i + 1; j < n; j++ {
				cand := append([]int(nil), order...)
				slices.Reverse(cand[i+1 : j+1])
				if newLen := TourLength(dists, cand); newLen < curLen-1e-9 {
					order, curLen = cand, newLen
					improved = true
				}
			}
		}
	}
	return order
}

// SimulatedAnnealing 模拟退火:从"最近邻 + 2-opt"的局部最优出发,
// 随机反转一段;变好就接受,变差也按 exp(-Δ/temp) 的概率接受,
// 从而跳出局部最优。温度从 1000 指数降到 0.1,接受劣解的概率随之降低。
// 返回全程见过的最优顺序。
//
// 邻域与判据同 TwoOpt:j 可到路径末端、精确 TourLength 差
// (旧版"边界两条边"公式在不对称矩阵上会把最优移动误判成变差)。
func SimulatedAnnealing(dists [][]float64, start int) []int {
	if len(dists) < 2 {
		return []int{start}
	}
	order := TwoOpt(dists, NearestNeighbor(dists, start))
	if len(order) < 4 {
		// 3 点路径:新邻域(j 可到末端)足以覆盖全部两种顺序,
		// 2-opt 已经找到局部最优,SA 的随机游走没有额外空间
		return order
	}
	best := append([]int(nil), order...)
	bestLen := TourLength(dists, best)
	curLen := bestLen
	n := len(order)

	for temp := 1000.0; temp > 0.1; temp *= 0.995 {
		for k := 0; k < 200; k++ {
			// 随机选两条边 (i,i+1) 和 (j,j+1):j > i 且允许 j = n-1
			// (反转段贴到末端 —— 否则 3 点路径的邻域是空的,见 TwoOpt 注释)
			i := rand.Intn(n - 1)
			j := i + 1 + rand.Intn(n-1-i)

			cand := append([]int(nil), order...)
			slices.Reverse(cand[i+1 : j+1])
			// 精确计算 TourLength 差值,不使用简化公式(不对称矩阵下的已知问题,同 TwoOpt)
			delta := TourLength(dists, cand) - curLen

			// 优于当前解直接接受;更差时以 exp(-Δ/temp) 的概率接受(温度越高接受概率越大)
			if delta < 0 || rand.Float64() < math.Exp(-delta/temp) {
				order, curLen = cand, curLen+delta
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
