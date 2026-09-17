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

// TestAsymmetricThreePointTrap:用 2026-09-17 smoke 当天的高德实测距离
// (广州塔/白云山/长隆)钉住两个真实缺陷 —— 它们叠加起来让解算器
// 在 3 点开放路径上"看见最优解却走不过去":
//
//  1. 旧邻域只允许"反转中间段"(j < n-1),n=3 时只能反转单个元素 = 恒等变换,
//     SA 退化成纯最近邻 —— 顺序根本动不了;
//  2. 旧判据只算边界两条边,默认反转段内部边不变 —— 对称矩阵成立,
//     但驾车真实路网不对称:最优移动的收益恰恰藏在内部边 43.8→28.7 的翻转里,
//     旧公式只看到边界 +0.9,把净赚 14.2km 的一步当成了变差而拒绝。
//
// NN = [0,1,2] = 15.445+43.795 = 59.24(smoke 当天服务真实输出);
// 最优 = [0,2,1] = 16.258+28.651 = 44.909。2-opt/SA 必须够得着它。
func TestAsymmetricThreePointTrap(t *testing.T) {
	dists := [][]float64{
		{0, 15.445, 16.258},
		{15.445, 0, 43.795},
		{16.258, 28.651, 0},
	}
	const want = 44.909

	twoOpt := TwoOpt(dists, NearestNeighbor(dists, 0))
	if got := TourLength(dists, twoOpt); got > want+1e-9 {
		t.Errorf("2-opt = %.3f, 应找到 %.3f 的顺序 [0,2,1]", got, want)
	}
	sa := SimulatedAnnealing(dists, 0)
	if got := TourLength(dists, sa); got > want+1e-9 {
		t.Errorf("SA = %.3f, 应找到 %.3f 的顺序 [0,2,1]", got, want)
	}
	if sa[0] != 0 || twoOpt[0] != 0 {
		t.Errorf("起点不变量被破坏: 2opt=%v sa=%v", twoOpt, sa)
	}
}
