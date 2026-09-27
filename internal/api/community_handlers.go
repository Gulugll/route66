// community_handlers.go —— 社区帖子的 HTTP 编排。
// 业务与校验全在 internal/community,这里只做三件事:绑参数、调服务、定状态码。
//
// 状态码语义(与 auth_handlers 同一套约定):
//
//	400 参数/快照不合法   401 没登录   403 不是作者   404 帖子不存在   503 存储故障
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/community"
)

// publishPost 发布路线笔记(POST /community/posts, RequireAuth)。
// snapshot 以原始 JSON 透传给 Service 校验 —— handler 不拆解它,
// 拆解了就会把"校验收口在 Service"的约定撕开一道口子。
func (s *Server) publishPost(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Title     string          `json:"title"`
		Content   string          `json:"content"`
		ImageURLs []string        `json:"image_urls"`
		Snapshot  json.RawMessage `json:"snapshot"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	v, err := s.community.Publish(c.Request.Context(), u.ID, req.Title, req.Content, req.Snapshot, req.ImageURLs)
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"post": v})
}

// listPosts 瀑布流(GET /community/posts, 公开 + OptionalAuth)。
// 游标分页:before_id 缺省 = 第一页;next_cursor=0 表示没有更多。
func (s *Server) listPosts(c *gin.Context) {
	var beforeID int64
	if q := c.Query("before_id"); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil || v < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "before_id 必须是非负整数"})
			return
		}
		beforeID = v
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是整数"})
		return
	}
	posts, next, err := s.community.ListPosts(c.Request.Context(), beforeID, limit, viewerID(c))
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"posts": posts, "next_cursor": next})
}

// viewerID 当前查看者;游客返回 0(即"无视角")。
// OptionalAuth 全局已认人,这里只是把认出结果翻译成 Service 的参数。
func viewerID(c *gin.Context) int64 {
	if u, ok := auth.CurrentUser(c); ok {
		return u.ID
	}
	return 0
}

// postDetail 帖子详情(GET /community/posts/:id, 公开)。带完整快照。
func (s *Server) postDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	v, err := s.community.Detail(c.Request.Context(), id, viewerID(c))
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"post": v})
}

// deletePost 删帖(DELETE /community/posts/:id, RequireAuth)。
// 作者校验在 Service 层 —— "谁的帖子谁能删"是业务规则,不是 HTTP 职责。
func (s *Server) deletePost(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	if err := s.community.DeletePost(c.Request.Context(), u.ID, id); err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// likePost 点赞(POST /community/posts/:id/like, RequireAuth)。幂等:重复赞仍 200。
func (s *Server) likePost(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	n, err := s.community.Like(c.Request.Context(), u.ID, id)
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"liked": true, "likes_count": n})
}

// unlikePost 取赞(POST /community/posts/:id/unlike, RequireAuth)。幂等。
func (s *Server) unlikePost(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	n, err := s.community.Unlike(c.Request.Context(), u.ID, id)
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"liked": false, "likes_count": n})
}

// listComments 评论列表(GET /community/posts/:id/comments, 公开, 时间升序)。
func (s *Server) listComments(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	cs, err := s.community.ListComments(c.Request.Context(), id)
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"comments": cs})
}

// addComment 发评论(POST /community/posts/:id/comments, RequireAuth)。
func (s *Server) addComment(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "帖子 ID 必须是数字"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	v, err := s.community.AddComment(c.Request.Context(), u.ID, id, req.Content)
	if err != nil {
		s.communityError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"comment": v})
}

// communityError 社区错误的统一翻译站。与 auth 错误翻译同一模式:
// 存储故障固定文案 + 503(原文只进日志),其余按错误类型定状态码。
func (s *Server) communityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, community.ErrStorageUnavailable):
		log.Printf("[community] storage error: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "社区服务暂时不可用，请稍后重试"})
	case errors.Is(err, community.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
	case errors.Is(err, community.ErrNotAuthor):
		c.JSON(http.StatusForbidden, gin.H{"error": "只能删除自己的帖子"})
	default:
		// 其余全是校验类错误(参数/快照),400 + 可行动的中文消息
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}
