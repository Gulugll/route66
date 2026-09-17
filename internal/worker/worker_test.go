package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
)

// fakeRepo 是 TaskRepo 的内存实现 —— Repository 模式的直接兑现:
// 不用真 MySQL 就能测 worker 的完整消费循环(领任务 → 解算 → 写回 → ACK)。
// MySQL 实现本身只有五条 SQL,真正的行为测试在这里做。
//
// mu 不是可有可无:worker 在自己的 goroutine 里调 MarkDone/MarkFailed,
// 主测试 goroutine 在轮询 Get —— 两边并发碰同一个 map,
// 没有 -race 的日子它安静如鸡,-race 一开当场抓获(2026-09-17 实测)。
// 真 MySQL 实现靠连接池+行锁保证并发安全,fake 也得对等。
type fakeRepo struct {
	mu    sync.Mutex
	tasks map[int64]*repo.Task
	next  int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{tasks: map[int64]*repo.Task{}}
}

func (f *fakeRepo) Create(ctx context.Context, reqJSON []byte) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.tasks[f.next] = &repo.Task{ID: f.next, ReqJSON: reqJSON, Status: repo.StatusQueued}
	return f.next, nil
}

func (f *fakeRepo) Get(ctx context.Context, id int64) (repo.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return repo.Task{}, repo.ErrNotFound
	}
	return *t, nil
}

func (f *fakeRepo) MarkDone(ctx context.Context, id int64, resultJSON []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id].Status = repo.StatusDone
	f.tasks[id].ResultJSON = resultJSON
	return nil
}

func (f *fakeRepo) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id].Status = repo.StatusFailed
	f.tasks[id].Error = errMsg
	return nil
}

// fakeAmap 起一个只回 v3/distance 的假高德(每对 10000 米,和 api 包的测试同款)
func fakeAmap(t *testing.T) *amap.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v3/distance" {
			t.Errorf("假高德收到未预期的路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := len(strings.Split(r.URL.Query().Get("origins"), "|"))
		items := make([]string, n)
		for i := range items {
			items[i] = `{"distance":"10000","duration":"600"}`
		}
		fmt.Fprintf(w, `{"status":"1","info":"OK","results":[%s]}`, strings.Join(items, ","))
	}))
	t.Cleanup(srv.Close)
	return amap.NewClientWithBase("test-key", srv.URL, nil)
}

// redisReachable 探测本机 6379。Redis 没起时测试跳过而不是失败 ——
// 依赖不可用不是代码错误(和"没配 key 不算降级"是同一个哲学)。
func redisReachable() bool {
	conn, err := net.DialTimeout("tcp", "localhost:6379", 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestWorkerConsumesTaskEndToEnd 验证完整消费循环:
// 建任务 → 入队 → worker 领走 → 解算 → 状态变 done → 结果正确。
// 用真 Redis(本机 6379):XADD/XREADGROUP/ACK 的语义没法用 fake 模拟出来。
func TestWorkerConsumesTaskEndToEnd(t *testing.T) {
	if !redisReachable() {
		t.Skip("本机 6379 没有可用的 Redis,跳过(启动方法见 README)")
	}
	gin.SetMode(gin.TestMode)

	// 每次运行用独立的 stream/组:测试之间、测试和真实服务之间互不吃消息
	stream := fmt.Sprintf("plan_tasks_test_%d", time.Now().UnixNano())
	q := queue.New("localhost:6379", stream, "workers", "")
	t.Cleanup(func() { _ = q.Close() })
	r := newFakeRepo()
	am := fakeAmap(t)
	m := matrix.NewWithAmap(am)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, q, r, m, am) }()

	// 建任务并入队:两点驾车 manual=true,顺序固定 [0,1],每段 10km
	req := planner.PlanRequest{
		Origin:       model.Point{Name: "天安门", Lat: 39.9087, Lng: 116.3975},
		Destinations: []model.Point{{Name: "故宫", Lat: 39.9163, Lng: 116.3972}},
		Manual:       true,
		Mode:         "driving",
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	id, err := r.Create(ctx, reqJSON)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := q.Add(ctx, id); err != nil {
		t.Fatalf("add to stream: %v(Redis 可达但入队失败,检查 stream 权限)", err)
	}

	// 轮询等 worker 写回结果(最多 5 秒):worker 是异步的,测试只能等
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, err := r.Get(ctx, id)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if task.Status == repo.StatusFailed {
			t.Fatalf("任务失败了: %s", task.Error)
		}
		if task.Status == repo.StatusDone {
			var result planner.Result
			if err := json.Unmarshal(task.ResultJSON, &result); err != nil {
				t.Fatalf("result 不是合法 JSON: %v", err)
			}
			if want := []int{0, 1}; !equalInts(result.OrderIdx, want) {
				t.Errorf("order_idx = %v, want %v", result.OrderIdx, want)
			}
			if result.TotalKm != 10 {
				t.Errorf("total_km = %v, want 10(一段 × 10000 米)", result.TotalKm)
			}
			if result.IsDegraded {
				t.Errorf("不该降级: %v", result.Warnings)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("5 秒内任务没被消费, still status=%s", task.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
