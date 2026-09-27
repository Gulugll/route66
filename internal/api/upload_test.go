// upload_test.go —— 头像上传 + profile 接口的 HTTP 契约测试。
//
// 与 auth_protect_test 同一思路:内存 fake store,不碰真库;
// 上传落盘用 t.TempDir() + os.Chdir,测试结束自动清理。
package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/matrix"
)

// pngBytes PNG 魔数开头的最小字节串。DetectContentType 只看头部签名,
// 不需要是能打开的完整图片 —— 它验证的是"嗅探不看扩展名"这件事。
var pngBytes = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, 64)...)

// newUploadTestRouter 装配认证路由 + 已登录用户,返回引擎、cookie、该用户 ID。
func newUploadTestRouter(t *testing.T) (*gin.Engine, string, int64) {
	t.Helper()
	store := newMemAuthStore()
	svc := auth.NewService(store)
	r := NewRouter(matrix.New(), nil, nil, nil, WithAuth(svc))
	_, body := doJSON(t, r, http.MethodPost, "/auth/register", `{"username":"uploader","password":"pass123"}`)
	_ = body
	// 注册即登录,doJSON 不透 cookie,重新登一次拿会话
	_, loginBody := doJSON(t, r, http.MethodPost, "/auth/login", `{"username":"uploader","password":"pass123"}`)
	_ = loginBody
	// 从 store 拿用户与 token(直接查 store 比 JSON 解析省事且同样钉住契约)
	var userID int64
	for _, u := range store.users {
		userID = u.ID
	}
	var token string
	for tok := range store.sessions {
		token = tok
	}
	return r, auth.CookieName + "=" + token, userID
}

// doUpload 构造 multipart 请求打 /auth/avatar。
func doUpload(t *testing.T, r *gin.Engine, cookie, filename string, content []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/auth/avatar", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestAvatarUpload(t *testing.T) {
	oldWd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })

	r, cookie, userID := newUploadTestRouter(t)

	t.Run("未登录 401", func(t *testing.T) {
		code, _ := doUpload(t, r, "", "a.png", pngBytes)
		if code != http.StatusUnauthorized {
			t.Fatalf("未登录上传应 401, got %d", code)
		}
	})

	t.Run("真 PNG 上传成功且文件落盘", func(t *testing.T) {
		code, resp := doUpload(t, r, cookie, "a.png", pngBytes)
		if code != http.StatusOK {
			t.Fatalf("PNG 上传应 200, got %d body=%s", code, resp)
		}
		if !strings.Contains(resp, "/uploads/avatars/") {
			t.Fatalf("响应应带 avatar_url, got %s", resp)
		}
		matches, _ := filepath.Glob(filepath.Join(".run", "uploads", "avatars",
			string(rune('0'+userID)), "*"))
		if len(matches) != 1 {
			t.Fatalf("落盘文件数应 1, got %d", len(matches))
		}
	})

	t.Run("假扩展名真 PNG 仍成功(嗅探生效)", func(t *testing.T) {
		code, _ := doUpload(t, r, cookie, "virus.exe", pngBytes)
		if code != http.StatusOK {
			t.Fatalf("内容是 PNG 就该放行(扩展名不可信), got %d", code)
		}
	})

	t.Run("非图片内容 400", func(t *testing.T) {
		code, resp := doUpload(t, r, cookie, "note.txt", []byte("just some text"))
		if code != http.StatusBadRequest {
			t.Fatalf("文本内容应 400, got %d body=%s", code, resp)
		}
		if !strings.Contains(resp, "jpg/png/webp") {
			t.Fatalf("400 应说明支持的类型, got %s", resp)
		}
	})

	t.Run("超 10MB 400", func(t *testing.T) {
		big := bytes.Repeat(pngBytes, (10<<20)/len(pngBytes)+1)
		code, _ := doUpload(t, r, cookie, "big.png", big)
		if code != http.StatusBadRequest {
			t.Fatalf("超大文件应 400, got %d", code)
		}
	})
}

func TestUpdateProfileEndpoint(t *testing.T) {
	r, cookie, _ := newUploadTestRouter(t)

	t.Run("未登录 401", func(t *testing.T) {
		code, _ := doJSON(t, r, http.MethodPatch, "/auth/profile", `{"nickname":"x"}`)
		if code != http.StatusUnauthorized {
			t.Fatalf("未登录改资料应 401, got %d", code)
		}
	})

	t.Run("已登录合法更新 200", func(t *testing.T) {
		code, resp := doJSON(t, r, http.MethodPatch, "/auth/profile",
			`{"nickname":"路痴拯救者","bio":"专治规划困难"}`, cookie)
		if code != http.StatusOK {
			t.Fatalf("合法更新应 200, got %d body=%s", code, resp)
		}
	})

	t.Run("昵称超长 400", func(t *testing.T) {
		code, _ := doJSON(t, r, http.MethodPatch, "/auth/profile",
			`{"nickname":"`+strings.Repeat("长", 33)+`"}`, cookie)
		if code != http.StatusBadRequest {
			t.Fatalf("超长昵称应 400, got %d", code)
		}
	})
}
