// upload.go —— 文件上传:头像 + 帖子图片。
//
// 两种上传共用同一套安全模式(三道防线,顺序固定,缺一不可):
//  1. http.MaxBytesReader 硬截大小 —— 攻击者发 2GB 的 body,读到上限就断,
//     不是"收完再检查大小";
//  2. http.DetectContentType 嗅探真实类型 —— 扩展名/Content-Type 都是请求者
//     自己写的,不可信;sniff 读的是文件头的魔数;
//  3. 每用户独立子目录 —— 目录名来自服务端认出的 userID,
//     绝不接受请求方指定路径片段(否则就是任意文件写入)。
//
// 与 auth/community 包的分工:这里只管"文件落盘 + URL 长什么样",
// 业务字段(头像写到 user 表 / 图片 URL 进帖子)由各自的 Service 完成。
package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
)

const (
	// 大小上限:头像与帖子单图均为 10MB
	avatarMaxBytes  = 10 << 20
	postImgMaxBytes = 10 << 20
)

// allowTypes 各上传场景允许的图片类型 → 扩展名。键是 DetectContentType 的输出。
var allowTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// saveUpload 三道防线的公共实现。
// subDir 是"/uploads/ 下的业务子目录"(如 "avatars"、"posts"),
// 返回可直接存库的 URL(如 /uploads/avatars/3/1727.png)。
// ok=false 表示已被本函数写好 4xx/5xx 响应,调用方直接 return。
func saveUpload(c *gin.Context, field, subDir string, maxBytes int64) (string, bool) {
	// 防线 1:硬截大小。超限时后续 FormFile/读 body 会报错,统一翻译成 400
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	file, _, err := c.Request.FormFile(field)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("缺少 %s 字段或文件超过上限", field)})
		return "", false
	}
	defer file.Close()

	// 防线 2:嗅探魔数。读前 512 字节判断类型,再 Seek 回去把完整内容交给落盘
	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取文件失败"})
		return "", false
	}
	head = head[:n]
	ext, ok := allowTypes[http.DetectContentType(head)]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 jpg/png/webp"})
		return "", false
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件读取失败"})
		return "", false
	}

	u, ok := auth.CurrentUser(c)
	if !ok {
		// RequireAuth 挂在路由组上,正常到不了这里;防御性兜底
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return "", false
	}

	// 防线 3:目录名只由服务端的 userID 决定;文件名用纳秒时间戳避免覆盖
	dir := filepath.Join(".run", "uploads", subDir, strconv.FormatInt(u.ID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[upload] mkdir failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "存储失败"})
		return "", false
	}
	name := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	dstPath := filepath.Join(dir, name)
	dst, err := os.Create(dstPath)
	if err != nil {
		log.Printf("[upload] create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "存储失败"})
		return "", false
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.MultiReader(bytes.NewReader(head), file)); err != nil {
		log.Printf("[upload] write failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "存储失败"})
		return "", false
	}

	url := "/uploads/" + subDir + "/" + strconv.FormatInt(u.ID, 10) + "/" + name
	return url, true
}

// avatar 处理 POST /auth/avatar(multipart,字段名 file)。
func (s *Server) avatar(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	url, ok := saveUpload(c, "file", "avatars", avatarMaxBytes)
	if !ok {
		return
	}
	if err := s.auth.SetAvatarURL(c.Request.Context(), u.ID, url); err != nil {
		// 库写失败,刚落的文件就是孤儿 —— 拼出本地路径删掉,不留垃圾
		os.Remove(filepath.Join(".run", url))
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			return
		}
		log.Printf("[avatar] db update failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "保存失败，请稍后重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"avatar_url": url})
}

// postImage 处理 POST /community/images(multipart,字段名 file)。
// 只负责"把图存下来返回 URL";URL 进哪篇帖子由发布接口决定(先传图后发帖)。
func (s *Server) postImage(c *gin.Context) {
	url, ok := saveUpload(c, "file", "posts", postImgMaxBytes)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}
