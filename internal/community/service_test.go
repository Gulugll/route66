// service_test.go —— Service 层测试:校验规则与业务规则都在这层钉死。
// HTTP 契约(状态码)在 api 包的测试里,这里只管"该不该过、该报什么错"。
package community

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// goodSnapshot 一份合法快照:3 个点、方式白名单内、坐标合法。
var goodSnapshot = []byte(`{
	"version": 0,
	"mode": "driving",
	"points": [
		{"name": "天安门", "lat": 39.9087, "lng": 116.3975, "legMode": "driving"},
		{"name": "故宫",   "lat": 39.9170, "lng": 116.3972, "legMode": "walking"},
		{"name": "天坛",   "lat": 39.8822, "lng": 116.4100, "legMode": "driving"}
	]
}`)

func publishOK(t *testing.T, s *Service) *PostView {
	t.Helper()
	v, err := s.Publish(context.Background(), 1, "北京一日游", "经典路线分享", goodSnapshot, nil)
	if err != nil {
		t.Fatalf("合法快照发布失败: %v", err)
	}
	return v
}

func TestPublishHappy(t *testing.T) {
	s := NewService(NewMemStore())
	v := publishOK(t, s)

	// 起终点冗余列从快照提取 —— 列表页不解析 JSON 的前提
	if v.OriginName != "天安门" || v.DestName != "天坛" {
		t.Fatalf("起终点提取错误: %q → %q", v.OriginName, v.DestName)
	}
	// version 被规范化为当前版本
	var back Snapshot
	if err := json.Unmarshal(v.Snapshot, &back); err != nil {
		t.Fatalf("存回的快照应可解析: %v", err)
	}
	if back.Version != currentSnapshotVersion {
		t.Fatalf("version 应规范化为 %d, got %d", currentSnapshotVersion, back.Version)
	}
	if back.Points[1].LegMode != "walking" {
		t.Fatalf("多段方式应原样保留, got %q", back.Points[1].LegMode)
	}
}

func TestPublishValidation(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()
	tests := []struct {
		name    string
		title   string
		content string
		snap    []byte
		wantMsg string
	}{
		{"标题为空", "  ", "正文", goodSnapshot, "标题不能为空"},
		{"标题超长", strings.Repeat("题", 31), "正文", goodSnapshot, "标题最多"},
		{"正文为空", "标题", "", goodSnapshot, "正文不能为空"},
		{"正文超长", "标题", strings.Repeat("文", 2001), goodSnapshot, "正文最多"},
		{"缺快照", "标题", "正文", nil, "缺少路线快照"},
		{"快照非 JSON", "标题", "正文", []byte("{not json"), "路线快照格式错误"},
		{"快照未知字段", "标题", "正文", []byte(`{"mode":"driving","points":[],"typo_field":1}`), "路线快照格式错误"},
		{"mode 非法", "标题", "正文", []byte(`{"mode":"flying","points":[{"name":"a","lat":1,"lng":1,"legMode":"driving"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}`), "出行方式无效"},
		{"legMode 非法", "标题", "正文", []byte(`{"mode":"driving","points":[{"name":"a","lat":1,"lng":1,"legMode":"swimming"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}`), "出行方式无效"},
		{"点数不足", "标题", "正文", []byte(`{"mode":"driving","points":[{"name":"a","lat":1,"lng":1,"legMode":"driving"}]}`), "路线需要 2"},
		{"点数超限", "标题", "正文", buildSnapshotWithNPoints(51), "路线需要 2~"},
		{"纬度越界", "标题", "正文", []byte(`{"mode":"driving","points":[{"name":"a","lat":91,"lng":1,"legMode":"driving"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}`), "坐标越界"},
		{"经度越界", "标题", "正文", []byte(`{"mode":"driving","points":[{"name":"a","lat":1,"lng":181,"legMode":"driving"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}`), "坐标越界"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Publish(ctx, 1, tc.title, tc.content, tc.snap, nil)
			if err == nil {
				t.Fatalf("期望报错(%s), 却成功了", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("错误应含 %q, got: %v", tc.wantMsg, err)
			}
			// 校验失败绝不能落库 —— 兜底绝不存在的另一面:半成品不存在
			if !errors.Is(err, ErrStorageUnavailable) {
				return
			}
			t.Fatal("校验错误不应被包装成存储故障")
		})
	}
}

// buildSnapshotWithNPoints 构造 n 个合法点的快照(边界用例:51 个点超限)。
func buildSnapshotWithNPoints(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"mode":"driving","points":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"name":"p` + itoa(i) + `","lat":39.9,"lng":116.4,"legMode":"driving"}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for ; i > 0; i /= 10 {
		d = append([]byte{byte('0' + i%10)}, d...)
	}
	return string(d)
}

// TestSnapshotSizeLimit 64KB 上限:用合法结构但超长地名堆出体积。
func TestSnapshotSizeLimit(t *testing.T) {
	s := NewService(NewMemStore())
	big := `{"mode":"driving","points":[` +
		`{"name":"` + strings.Repeat("地", 40000) + `","lat":1,"lng":1,"legMode":"driving"},` +
		`{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}`
	_, err := s.Publish(context.Background(), 1, "标题", "正文", []byte(big), nil)
	if err == nil || !strings.Contains(err.Error(), "64KB") {
		t.Fatalf("期望 64KB 上限报错, got: %v", err)
	}
}

func TestListCursor(t *testing.T) {
	s := NewService(NewMemStore())
	for i := 0; i < 7; i++ {
		if _, err := s.Publish(context.Background(), 1,
			"帖子"+itoa(i), "正文", goodSnapshot, nil); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	// 第一页:默认 10 条上限,只有 7 条 → next=0(没有更多)
	posts, next, err := s.ListPosts(context.Background(), 0, 10, 0)
	if err != nil || len(posts) != 7 || next != 0 {
		t.Fatalf("第一页: n=%d next=%d err=%v", len(posts), next, err)
	}
	// 降序:最新在前
	if posts[0].ID != 7 || posts[6].ID != 1 {
		t.Fatalf("列表应按 id 降序, got %d..%d", posts[0].ID, posts[6].ID)
	}

	// 第二页:limit=3,从游标 7 往回拿 → 6,5,4 → 还有更多, next=4(本页最后一条)
	posts, next, err = s.ListPosts(context.Background(), 7, 3, 0)
	if err != nil || len(posts) != 3 || next != 4 {
		t.Fatalf("第二页: n=%d next=%d err=%v", len(posts), next, err)
	}

	// 第三页:从游标 4 拿 3 条 → 3,2,1 → 拿完, next=0
	posts, next, _ = s.ListPosts(context.Background(), 4, 3, 0)
	if len(posts) != 3 || posts[2].ID != 1 || next != 0 {
		t.Fatalf("第三页: n=%d last=%d next=%d", len(posts), posts[2].ID, next)
	}

	// limit 越界收敛到 10(防一次性拉全表)
	posts, _, _ = s.ListPosts(context.Background(), 0, 999, 0)
	if len(posts) > 10 {
		t.Fatalf("limit 应被收敛, got %d", len(posts))
	}
}

func TestDeleteAuthorOnly(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()
	v := publishOK(t, s)

	t.Run("非作者删除 403 语义", func(t *testing.T) {
		err := s.DeletePost(ctx, 999, v.ID)
		if !errors.Is(err, ErrNotAuthor) {
			t.Fatalf("期望 ErrNotAuthor, got: %v", err)
		}
	})
	t.Run("作者删除成功", func(t *testing.T) {
		if err := s.DeletePost(ctx, v.UserID, v.ID); err != nil {
			t.Fatalf("作者删除失败: %v", err)
		}
	})
	t.Run("删除后不可见", func(t *testing.T) {
		if _, err := s.Detail(ctx, v.ID, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("删除后详情应 ErrNotFound, got: %v", err)
		}
		posts, _, _ := s.ListPosts(ctx, 0, 10, 0)
		if len(posts) != 0 {
			t.Fatalf("删除后列表不应再有它")
		}
	})
	t.Run("重复删除等同不存在", func(t *testing.T) {
		if err := s.DeletePost(ctx, v.UserID, v.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("重复删除应 ErrNotFound, got: %v", err)
		}
	})
}

// ── M3 互动 ──

// TestLikeUnlike 钉住幂等语义与计数一致性:
// 计数必须与 post_likes 行数严格相等(同一事务保证),任何一边漂移都是事故。
func TestLikeUnlike(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()
	v := publishOK(t, s)

	// A 赞:created,计数 1
	n, err := s.Like(ctx, 100, v.ID)
	if err != nil || n != 1 {
		t.Fatalf("首赞: count=%d err=%v", n, err)
	}
	// A 重复赞:幂等,计数不变
	if n, _ := s.Like(ctx, 100, v.ID); n != 1 {
		t.Fatalf("重复赞计数应不变, got %d", n)
	}
	// B 赞:计数 2
	if n, _ := s.Like(ctx, 200, v.ID); n != 2 {
		t.Fatalf("第二人赞后计数应 2, got %d", n)
	}

	// A 取赞:计数 1
	if n, _ := s.Unlike(ctx, 100, v.ID); n != 1 {
		t.Fatalf("取赞后计数应 1, got %d", n)
	}
	// A 重复取赞:幂等,计数不变(绝不减成负数)
	if n, _ := s.Unlike(ctx, 100, v.ID); n != 1 {
		t.Fatalf("重复取赞计数应不变, got %d", n)
	}
	// 没赞过的人直接取赞:同样幂等
	if n, _ := s.Unlike(ctx, 300, v.ID); n != 1 {
		t.Fatalf("无赞取赞计数应不变, got %d", n)
	}

	// 帖子不存在 → ErrNotFound(不是 400 的校验错)
	if _, err := s.Like(ctx, 100, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("给不存在的帖子点赞应 ErrNotFound, got: %v", err)
	}
}

// TestAddComment 评论校验与计数。
func TestAddComment(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()
	v := publishOK(t, s)

	if _, err := s.AddComment(ctx, 100, v.ID, "路线不错!"); err != nil {
		t.Fatalf("发评论失败: %v", err)
	}
	// 计数随评论 +1(同一事务)
	d, _ := s.Detail(ctx, v.ID, 0)
	if d.CommentsCount != 1 {
		t.Fatalf("评论后 comments_count 应 1, got %d", d.CommentsCount)
	}
	// 空内容 / 超长
	if _, err := s.AddComment(ctx, 100, v.ID, "   "); err == nil {
		t.Fatal("空评论应报错")
	}
	if _, err := s.AddComment(ctx, 100, v.ID, strings.Repeat("评", 501)); err == nil {
		t.Fatal("超长评论应报错")
	}
	// 给不存在的帖子评论
	if _, err := s.AddComment(ctx, 100, 99999, "hi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("应 ErrNotFound, got: %v", err)
	}
	// 列表升序
	if _, err := s.AddComment(ctx, 200, v.ID, "第二条"); err != nil {
		t.Fatal(err)
	}
	cs, err := s.ListComments(ctx, v.ID)
	if err != nil || len(cs) != 2 || cs[0].Content != "路线不错!" {
		t.Fatalf("评论列表应升序且全量: n=%d err=%v", len(cs), err)
	}
}

// TestIsLikedBackfill is_liked 批量回填:同一批帖子,不同 viewer 看到不同结果。
func TestIsLikedBackfill(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()
	p1 := publishOK(t, s)
	_, err := s.Publish(ctx, 1, "第二篇", "正文", goodSnapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 用户 100 只赞 p1
	if _, err := s.Like(ctx, 100, p1.ID); err != nil {
		t.Fatal(err)
	}

	listA, _, _ := s.ListPosts(ctx, 0, 10, 100)
	for _, p := range listA {
		want := p.ID == p1.ID
		if p.IsLiked != want {
			t.Fatalf("user100 视角: post %d is_liked 应为 %v", p.ID, want)
		}
	}
	listB, _, _ := s.ListPosts(ctx, 0, 10, 200)
	for _, p := range listB {
		if p.IsLiked {
			t.Fatalf("user200 没赞过任何帖, is_liked 应全 false")
		}
	}
	// 游客(viewerID=0):全 false
	listG, _, _ := s.ListPosts(ctx, 0, 10, 0)
	for _, p := range listG {
		if p.IsLiked {
			t.Fatal("游客 is_liked 应全 false")
		}
	}
	// 详情同样回填
	d, _ := s.Detail(ctx, p1.ID, 100)
	if !d.IsLiked {
		t.Fatal("详情视角的 is_liked 未回填")
	}
}

// ── 图片(M4 追加) ──

// TestPublishWithImages 图片 URL 列表:原样存储、上限 9、只收自家上传路径。
func TestPublishWithImages(t *testing.T) {
	s := NewService(NewMemStore())
	ctx := context.Background()

	t.Run("带图发布原样存储", func(t *testing.T) {
		urls := []string{"/uploads/posts/1/a.png", "/uploads/posts/1/b.jpg"}
		v, err := s.Publish(ctx, 1, "图文帖", "正文", goodSnapshot, urls)
		if err != nil {
			t.Fatalf("带图发布失败: %v", err)
		}
		if len(v.ImageURLs) != 2 || v.ImageURLs[0] != urls[0] {
			t.Fatalf("image_urls 未存储: %v", v.ImageURLs)
		}
	})

	t.Run("超过 9 张报错", func(t *testing.T) {
		urls := make([]string, 10)
		for i := range urls {
			urls[i] = fmt.Sprintf("/uploads/posts/1/%d.png", i)
		}
		if _, err := s.Publish(ctx, 1, "标题", "正文", goodSnapshot, urls); err == nil {
			t.Fatal("10 张图应报错")
		}
	})

	t.Run("拒绝外链 URL", func(t *testing.T) {
		// 帖子里的图必须是自己存的 —— 收外链等于给 XSS/盗链开洞
		if _, err := s.Publish(ctx, 1, "标题", "正文", goodSnapshot,
			[]string{"https://evil.example.com/x.png"}); err == nil {
			t.Fatal("外链 URL 应报错")
		}
	})

	t.Run("空列表合法(纯路线帖)", func(t *testing.T) {
		v, err := s.Publish(ctx, 1, "无图帖", "正文", goodSnapshot, nil)
		if err != nil {
			t.Fatalf("nil 图片列表应合法: %v", err)
		}
		if len(v.ImageURLs) != 0 {
			t.Fatalf("nil 应存为空, got %v", v.ImageURLs)
		}
	})
}
