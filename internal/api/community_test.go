// community_test.go —— 社区路由的 HTTP 契约测试。
// 与 auth_protect_test 同一纪律:路径写字面量、重写解码结构、内存 fake 不碰真库。
// Service 层已详测校验规则;这里钉的是"错误类型 → 状态码"的翻译与路由保护。
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/community"
	"awesomeProject/internal/matrix"
)

// newCommunityTestRouter 两个用户的社区路由:cookieA/cookieB + 服务里已注册的用户。
// 快照用例与 community 包的 goodSnapshot 同构 —— 改快照结构时两边都要动,故意的:
// 契约测试就该和真实请求体长得一模一样,而不是共享一个"生成器"偷偷解耦。
const goodSnapshotJSON = `{"mode":"driving","points":[
	{"name":"天安门","lat":39.9087,"lng":116.3975,"legMode":"driving"},
	{"name":"天坛","lat":39.8822,"lng":116.4100,"legMode":"driving"}]}`

func newCommunityTestRouter(t *testing.T) (*gin.Engine, string, string) {
	t.Helper()
	store := newMemAuthStore()
	svc := auth.NewService(store)
	cs := community.NewService(community.NewMemStore())
	r := NewRouter(matrix.New(), nil, nil, nil, WithAuth(svc), WithCommunity(cs))

	cookie := func(name string) string {
		doJSON(t, r, http.MethodPost, "/auth/register",
			`{"username":"`+name+`","password":"pass123"}`)
		// 注册即登录,但 doJSON 不透 cookie;按用户名精确定位其会话 ——
		// map 遍历顺序随机,不能"取任意一个"(A 先注册,再来遍历就会拿到 A 的)
		u := store.users[name]
		for tok, s := range store.sessions {
			if s.UserID == u.ID {
				return auth.CookieName + "=" + tok
			}
		}
		return ""
	}
	cookieA := cookie("author_a")
	cookieB := cookie("reader_b")
	return r, cookieA, cookieB
}

func TestCommunityCRUDFlow(t *testing.T) {
	r, cookieA, cookieB := newCommunityTestRouter(t)

	// ── 发布:登录用户 200,响应带帖子与作者 ──
	code, resp := doJSON(t, r, http.MethodPost, "/community/posts",
		`{"title":"北京一日游","content":"经典路线","image_urls":[],"snapshot":`+goodSnapshotJSON+`}`, cookieA)
	if code != http.StatusOK {
		t.Fatalf("发布应 200, got %d body=%s", code, resp)
	}
	var pub struct {
		Post struct {
			ID         int64  `json:"id"`
			OriginName string `json:"origin_name"`
			DestName   string `json:"dest_name"`
			Author     struct {
				ID int64 `json:"id"`
			} `json:"author"`
		} `json:"post"`
	}
	if err := json.Unmarshal([]byte(resp), &pub); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if pub.Post.ID == 0 || pub.Post.OriginName != "天安门" || pub.Post.DestName != "天坛" {
		t.Fatalf("发布响应缺关键字段: %+v", pub.Post)
	}
	id := itoa64(pub.Post.ID)

	// ── 游客浏览列表 200(公开 + 作者信息) ──
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts", "")
	if code != http.StatusOK {
		t.Fatalf("游客列表应 200, got %d", code)
	}
	if !strings.Contains(resp, `"next_cursor"`) || !strings.Contains(resp, `"username":"user1"`) {
		t.Fatalf("列表应带 next_cursor 与作者名, got %s", resp)
	}

	// ── 游客详情 200 且带完整快照 ──
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts/"+id, "")
	if code != http.StatusOK || !strings.Contains(resp, `"snapshot"`) {
		t.Fatalf("详情应 200 且带快照, got %d body=%s", code, resp)
	}

	// ── 未登录发布 401 ──
	code, _ = doJSON(t, r, http.MethodPost, "/community/posts",
		`{"title":"x","content":"y","image_urls":[],"snapshot":`+goodSnapshotJSON+`}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录发布应 401, got %d", code)
	}

	// ── 非 B 用户删除 → 403(不是 404!语义区分是契约的一部分) ──
	code, _ = doJSON(t, r, http.MethodDelete, "/community/posts/"+id, "", cookieB)
	if code != http.StatusForbidden {
		t.Fatalf("非作者删除应 403, got %d", code)
	}

	// ── 作者删除 → 200 → 详情 404 → 列表不再出现 ──
	code, _ = doJSON(t, r, http.MethodDelete, "/community/posts/"+id, "", cookieA)
	if code != http.StatusOK {
		t.Fatalf("作者删除应 200, got %d", code)
	}
	code, _ = doJSON(t, r, http.MethodGet, "/community/posts/"+id, "")
	if code != http.StatusNotFound {
		t.Fatalf("删除后详情应 404, got %d", code)
	}
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts", "")
	if strings.Contains(resp, "北京一日游") {
		t.Fatal("删除后列表不应再出现该帖")
	}
	_ = code
}

func TestCommunityBadRequests(t *testing.T) {
	r, cookieA, _ := newCommunityTestRouter(t)

	t.Run("坏快照逐一 400", func(t *testing.T) {
		bad := map[string]string{
			"mode非法":    `{"title":"t","content":"c","snapshot":{"mode":"flying","points":[{"name":"a","lat":1,"lng":1,"legMode":"driving"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}}`,
			"点数不足":      `{"title":"t","content":"c","snapshot":{"mode":"driving","points":[{"name":"a","lat":1,"lng":1,"legMode":"driving"}]}}`,
			"坐标越界":      `{"title":"t","content":"c","snapshot":{"mode":"driving","points":[{"name":"a","lat":999,"lng":1,"legMode":"driving"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}}`,
			"legMode非法": `{"title":"t","content":"c","snapshot":{"mode":"driving","points":[{"name":"a","lat":1,"lng":1,"legMode":"swimming"},{"name":"b","lat":2,"lng":2,"legMode":"driving"}]}}`,
		}
		for name, body := range bad {
			code, resp := doJSON(t, r, http.MethodPost, "/community/posts", body, cookieA)
			if code != http.StatusBadRequest {
				t.Errorf("%s: 应 400, got %d body=%s", name, code, resp)
			}
		}
	})

	t.Run("删除不存在的帖子 404", func(t *testing.T) {
		code, _ := doJSON(t, r, http.MethodDelete, "/community/posts/99999", "", cookieA)
		if code != http.StatusNotFound {
			t.Fatalf("应 404, got %d", code)
		}
	})

	t.Run("帖子 ID 非数字 400", func(t *testing.T) {
		code, _ := doJSON(t, r, http.MethodGet, "/community/posts/notanumber", "")
		if code != http.StatusBadRequest {
			t.Fatalf("应 400, got %d", code)
		}
	})

	t.Run("before_id 非数字 400", func(t *testing.T) {
		code, _ := doJSON(t, r, http.MethodGet, "/community/posts?before_id=abc", "")
		if code != http.StatusBadRequest {
			t.Fatalf("应 400, got %d", code)
		}
	})
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// TestCommunityInteractions 钉住互动端点的 HTTP 契约:
// 幂等(重复赞/重复取赞都是 200)、401/403 的分流、is_liked 回填、评论公开读。
func TestCommunityInteractions(t *testing.T) {
	r, cookieA, cookieB := newCommunityTestRouter(t)

	code, resp := doJSON(t, r, http.MethodPost, "/community/posts",
		`{"title":"互动帖","content":"来赞我","image_urls":[],"snapshot":`+goodSnapshotJSON+`}`, cookieA)
	if code != http.StatusOK {
		t.Fatalf("发布应 200, got %d body=%s", code, resp)
	}
	var pub struct {
		Post struct {
			ID int64 `json:"id"`
		} `json:"post"`
	}
	json.Unmarshal([]byte(resp), &pub)
	id := itoa64(pub.Post.ID)

	// A 赞 → 计数 1;重复赞 → 仍 200 计数 1(幂等)
	code, resp = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/like", "", cookieA)
	if code != http.StatusOK || !strings.Contains(resp, `"likes_count":1`) {
		t.Fatalf("首赞应 200 计数 1, got %d body=%s", code, resp)
	}
	code, resp = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/like", "", cookieA)
	if code != http.StatusOK || !strings.Contains(resp, `"likes_count":1`) {
		t.Fatalf("重复赞应幂等 200, got %d body=%s", code, resp)
	}
	// B 赞 → 计数 2
	code, resp = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/like", "", cookieB)
	if code != http.StatusOK || !strings.Contains(resp, `"likes_count":2`) {
		t.Fatalf("第二人赞应计数 2, got %d body=%s", code, resp)
	}

	// is_liked 回填:A 看 true,B 看 true,游客看 false
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts/"+id, "", cookieA)
	if code != http.StatusOK || !strings.Contains(resp, `"is_liked":true`) {
		t.Fatalf("A 的详情应 is_liked true, got %d body=%s", code, resp)
	}
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts/"+id, "")
	if code != http.StatusOK || !strings.Contains(resp, `"is_liked":false`) {
		t.Fatalf("游客详情应 is_liked false, got %d body=%s", code, resp)
	}

	// B 评论 → 公开列表可见;未登录评论 401
	code, _ = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/comments",
		`{"content":"好路线!"}`, cookieB)
	if code != http.StatusOK {
		t.Fatalf("发评论应 200, got %d", code)
	}
	code, resp = doJSON(t, r, http.MethodGet, "/community/posts/"+id+"/comments", "")
	if code != http.StatusOK || !strings.Contains(resp, "好路线!") {
		t.Fatalf("游客看评论应 200 且含内容, got %d body=%s", code, resp)
	}
	code, _ = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/comments", `{"content":"x"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录评论应 401, got %d", code)
	}

	// A 取赞 → 计数 1;A 重复取赞 → 仍 1
	doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/unlike", "", cookieA)
	_, resp = doJSON(t, r, http.MethodPost, "/community/posts/"+id+"/unlike", "", cookieA)
	if !strings.Contains(resp, `"likes_count":1`) {
		t.Fatalf("重复取赞后计数应仍为 1, got %s", resp)
	}
}
