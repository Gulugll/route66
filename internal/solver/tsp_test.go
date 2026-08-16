package solver

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

// TestNearestNeighborOrder:从 0 出发,1 比 2 近,所以必须先去 1 再去 2。
// 如果算法哪天改坏了,这个测试会先报错。
func TestNearestNeighborOrder(t *testing.T) {
	dists := [][]float64{
		{0, 1, 10},
		{1, 0, 9},
		{10, 9, 0},
	}
	got := NearestNeighbor(dists, 0)
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestSolverImprovementChain 验证"逐步变好"这条核心承诺:
// 最近邻 → 2-opt → 模拟退火,总距离只会降不会升。
// 2-opt 只接受更短,SA 记录全程最优(从 2-opt 起步),所以这个断言是确定性的。
// 用固定种子的随机平面点:这种图最近邻容易画出交叉线,2-opt 能收回来,看得见改善。
func TestSolverImprovementChain(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	pts := make([][2]float64, 12)
	for i := range pts {
		pts[i] = [2]float64{rng.Float64() * 100, rng.Float64() * 100}
	}
	dists := make([][]float64, len(pts))
	for i := range dists {
		dists[i] = make([]float64, len(pts))
		for j := range dists[i] {
			dx, dy := pts[i][0]-pts[j][0], pts[i][1]-pts[j][1]
			dists[i][j] = math.Hypot(dx, dy)
		}
	}

	nn := NearestNeighbor(dists, 0)
	nnLen := TourLength(dists, nn)
	twoOpt := TwoOpt(dists, nn)
	twoOptLen := TourLength(dists, twoOpt)
	sa := SimulatedAnnealing(dists, 0)
	saLen := TourLength(dists, sa)

	if !(saLen <= twoOptLen && twoOptLen <= nnLen) {
		t.Fatalf("应逐步变好: NN=%.2f, 2opt=%.2f, SA=%.2f", nnLen, twoOptLen, saLen)
	}
	t.Logf("最近邻 %.2f → +2-opt %.2f → +模拟退火 %.2f (km)", nnLen, twoOptLen, saLen)
}
