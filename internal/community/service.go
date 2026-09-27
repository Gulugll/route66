// service.go —— 社区业务:校验收口 + 发布/浏览/删除。
//
// 校验三来源(全部白名单,非法一律 error,handler 翻译成 400,绝不兜底):
//   - mode/legMode:与 /plan 同一套白名单(直接复用 planner.ParseMode 的取值集合)
//   - 坐标:lat ∈ [-90,90]、lng ∈ [-180,180](同 planner.isValidCoord 语义)
//   - 结构:points 2~50 个、快照 ≤64KB、title ≤30 字、content ≤2000 字
package community

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"awesomeProject/internal/model"
)

// trimSpace 收口 strings.TrimSpace:Service 出入口的字符串都过一遍,调用处不用各自记。
func trimSpace(s string) string { return strings.TrimSpace(s) }

// 快照结构限制。点数上限与规划器一致(50);快照体积上限防"一篇帖子几 MB"。
const (
	maxSnapshotPoints = 50
	minSnapshotPoints = 2
	maxSnapshotBytes  = 64 << 10
	maxTitleRunes     = 30
	maxContentRunes   = 2000
)

// ErrNotAuthor 删除者不是作者。handler 翻译成 403 —— 已登录但权限不够,
// 与 401(没登录)语义严格分开。
var ErrNotAuthor = errors.New("not the author")

// ErrStorageUnavailable 存储故障统一包装。handler 必须给固定文案 + 503,
// 原文(DSN/驱动报错)只进服务端日志 —— 与 auth 包同一条红线。
var ErrStorageUnavailable = errors.New("storage unavailable")

// Snapshot 路线快照的结构。version 为前向兼容预留:
// 将来加字段/改语义时,读旧版本快照按 version 分支,而不是猜。
type Snapshot struct {
	Version int             `json:"version"`
	Mode    string          `json:"mode"`
	Points  []SnapshotPoint `json:"points"`
}

// SnapshotPoint 与前端规划器的点结构对齐(legMode 挂在"点"上,
// 语义是"从这个点到下一点怎么走")。取值与 model.Mode 同一白名单。
type SnapshotPoint struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	LegMode string  `json:"legMode"`
}

// currentSnapshotVersion 当前快照版本。发布时写入。
const currentSnapshotVersion = 1

// validateSnapshot 校验并规范化快照。返回"验证过、补齐了 version 的规范 JSON",
// 存库用这一份 —— 入库前统一形态,读侧就不用防"五花八门的写法"。
func validateSnapshot(raw []byte) (json.RawMessage, Snapshot, error) {
	if len(raw) == 0 {
		return nil, Snapshot{}, fmt.Errorf("缺少路线快照")
	}
	if len(raw) > maxSnapshotBytes {
		return nil, Snapshot{}, fmt.Errorf("路线快照过大(上限 64KB)")
	}
	// 不用 json.Valid + 两次解析:直接 Unmarshal,语法错和字段错一次拿全
	var snap Snapshot
	dec := json.NewDecoder(bytes.NewReader(raw))
	// DisallowUnknownFields:快照字段名拼错(如 legmode)在发布时就该报出来,
	// 而不是静默忽略后"看起来存成功了、详情页渲染却是空的"
	dec.DisallowUnknownFields()
	if err := dec.Decode(&snap); err != nil {
		return nil, Snapshot{}, fmt.Errorf("路线快照格式错误: %v", err)
	}

	// mode 复用 /plan 的白名单 —— 同一个参数,两个入口必须一套标准
	if _, err := parseMode(snap.Mode); err != nil {
		return nil, Snapshot{}, err
	}
	if n := len(snap.Points); n < minSnapshotPoints || n > maxSnapshotPoints {
		return nil, Snapshot{}, fmt.Errorf("路线需要 2~%d 个点(当前 %d 个)", maxSnapshotPoints, n)
	}
	for i, p := range snap.Points {
		// legMode 与 mode 同一白名单:快照里点的出行方式就是规划请求里的段方式
		if _, err := parseMode(p.LegMode); err != nil {
			return nil, Snapshot{}, fmt.Errorf("第 %d 个点的出行方式无效: %v", i+1, err)
		}
		if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
			return nil, Snapshot{}, fmt.Errorf("第 %d 个点坐标越界: lat=%v lng=%v", i+1, p.Lat, p.Lng)
		}
		if utf8.RuneCountInString(p.Name) > 64 {
			return nil, Snapshot{}, fmt.Errorf("第 %d 个点地名过长(上限 64 字)", i+1)
		}
	}

	snap.Version = currentSnapshotVersion
	out, err := json.Marshal(snap)
	if err != nil {
		return nil, Snapshot{}, fmt.Errorf("快照序列化失败: %v", err)
	}
	return out, snap, nil
}

// parseMode 出行方式白名单。与 planner.ParseMode 同一取值集合 ——
// 不直接 import planner 是刻意的:community 只需要"字符串是否合法"这一件事,
// 拖入整个规划包会拉进一堆用不到的依赖。取值列表在测试里与 planner 对齐钉死。
func parseMode(s string) (string, error) {
	switch model.Mode(s) {
	case model.ModeDriving, model.ModeWalking, model.ModeTransit:
		return s, nil
	default:
		return "", fmt.Errorf("出行方式无效: %q(支持 driving/walking/transit)", s)
	}
}

// Service 社区业务。只依赖 Store 接口。
type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// maxImageCount 一篇笔记的图片上限(对齐主流社区),maxImageURLLen 防"合法前缀+超长尾巴"。
const (
	maxImageCount  = 9
	maxImageURLLen = 200
	imageURLPrefix = "/uploads/posts/"
)

// validateImageURLs 校验图片 URL 列表。
// 两条硬规则:
//   - 只收 /uploads/posts/ 前缀 —— 图片必须来自我们自己的上传接口,
//     收外链等于给 XSS/盗链/任意跳转开洞;前缀校验把"发帖带图"收敛成
//     "引用自己刚上传的文件",攻击面直接归零。
//   - 上限 9 张,超出整单拒绝(而不是静默截断 —— 静默丢用户数据是最差的失败方式)。
func validateImageURLs(urls []string) (StringList, error) {
	if len(urls) == 0 {
		return StringList{}, nil
	}
	if len(urls) > maxImageCount {
		return nil, fmt.Errorf("图片最多 %d 张", maxImageCount)
	}
	cleaned := make(StringList, 0, len(urls))
	for i, u := range urls {
		u = trimSpace(u)
		if u == "" {
			continue // 空条目跳过(前端上传失败会留空位)
		}
		if !strings.HasPrefix(u, imageURLPrefix) {
			return nil, fmt.Errorf("第 %d 张图片的地址不合法: 只能使用本站上传的图片", i+1)
		}
		if len(u) > maxImageURLLen {
			return nil, fmt.Errorf("第 %d 张图片的地址过长", i+1)
		}
		cleaned = append(cleaned, u)
	}
	return cleaned, nil
}

// Publish 发布路线笔记。
// snapshotRaw 是前端发来的原始 JSON —— 在这里验明正身后存"规范化版本"。
// imageUrls 是"已通过 /community/images 上传"的 URL 列表(先传图后发帖)。
func (s *Service) Publish(ctx context.Context, userID int64, title, content string, snapshotRaw []byte, imageUrls []string) (*PostView, error) {
	title = trimSpace(title)
	content = trimSpace(content)
	if title == "" {
		return nil, fmt.Errorf("标题不能为空")
	}
	if n := utf8.RuneCountInString(title); n > maxTitleRunes {
		return nil, fmt.Errorf("标题最多 %d 个字", maxTitleRunes)
	}
	if content == "" {
		return nil, fmt.Errorf("正文不能为空")
	}
	if n := utf8.RuneCountInString(content); n > maxContentRunes {
		return nil, fmt.Errorf("正文最多 %d 个字", maxContentRunes)
	}

	snapJSON, snap, err := validateSnapshot([]byte(snapshotRaw))
	if err != nil {
		return nil, err
	}
	images, err := validateImageURLs(imageUrls)
	if err != nil {
		return nil, err
	}

	p := &Post{
		UserID:       userID,
		Title:        title,
		Content:      content,
		PlanSnapshot: snapJSON,
		ImageURLs:    images,
		OriginName:   snap.Points[0].Name,
		DestName:     snap.Points[len(snap.Points)-1].Name,
	}
	if err := s.store.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	// Create 回填了 id/created_at;作者信息就是当前用户自己,不必回查
	return &PostView{
		ID:         p.ID,
		UserID:     p.UserID,
		Title:      p.Title,
		Content:    p.Content,
		OriginName: p.OriginName,
		DestName:   p.DestName,
		ImageURLs:  p.ImageURLs,
		Snapshot:   p.PlanSnapshot,
		CreatedAt:  p.CreatedAt,
		Author:     Author{ID: userID},
	}, nil
}

// ListPosts 瀑布流。beforeID<=0 取最新一页;返回 nextCursor 供前端续拉,
// 0 表示确定没有更多了。viewerID 是当前查看者(0=游客),用于 is_liked 回填。
func (s *Service) ListPosts(ctx context.Context, beforeID int64, limit int, viewerID int64) ([]PostView, int64, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	// limit+1 探测法:多取一条判断"还有没有下一页"。
	// 否则满页时无法区分"正好取完"和"后面还有",前端只能多拉一次空页。
	posts, err := s.store.List(ctx, beforeID, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	var next int64
	if len(posts) > limit {
		posts = posts[:limit]
		next = posts[len(posts)-1].ID // 本页最后一条即下一页的游标
	}
	s.applyIsLiked(ctx, viewerID, posts)
	return posts, next, nil
}

// applyIsLiked 批量回填 is_liked。一次 IN 查询服务整页,
// 绝不做逐帖查询(N+1 是列表页头号性能杀手)。
// 回填失败静默降级为"全部未赞" —— 装饰性字段不该打死整个列表。
func (s *Service) applyIsLiked(ctx context.Context, viewerID int64, posts []PostView) {
	if viewerID <= 0 || len(posts) == 0 {
		return
	}
	ids := make([]int64, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
	}
	liked, err := s.store.LikedIDs(ctx, viewerID, ids)
	if err != nil {
		return
	}
	for i := range posts {
		posts[i].IsLiked = liked[posts[i].ID]
	}
}

// Detail 帖子详情(带完整快照与 is_liked)。viewerID=0 表示游客。
func (s *Service) Detail(ctx context.Context, id, viewerID int64) (*PostView, error) {
	v, err := s.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	// 单帖的 is_liked 直接复用批量回填(一条 IN 查询,只含一个 id)——
	// 与列表同一套代码路径,不出现"详情和列表判定不一致"的分叉
	posts := []PostView{*v}
	s.applyIsLiked(ctx, viewerID, posts)
	v.IsLiked = posts[0].IsLiked
	return v, nil
}

// Like 点赞。幂等:重复赞不报错、计数不动。返回点赞后的最新计数。
func (s *Service) Like(ctx context.Context, userID, postID int64) (int, error) {
	if _, err := s.store.GetByID(ctx, postID); err != nil {
		return 0, s.mapErr(err)
	}
	created, err := s.store.Like(ctx, userID, postID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	_ = created // 幂等语义由 store 兜住,上层只关心最终计数
	return s.countOf(ctx, postID)
}

// Unlike 取赞。幂等:没赞过不报错、计数不动(绝不减成负数)。
func (s *Service) Unlike(ctx context.Context, userID, postID int64) (int, error) {
	if _, err := s.store.GetByID(ctx, postID); err != nil {
		return 0, s.mapErr(err)
	}
	if _, err := s.store.Unlike(ctx, userID, postID); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return s.countOf(ctx, postID)
}

// countOf 读最新计数。比"事务返回值带出计数"多一次主键查询,
// 但换来 store 接口的事务方法保持简单 —— 教学规模下清晰优先。
func (s *Service) countOf(ctx context.Context, postID int64) (int, error) {
	v, err := s.store.GetByID(ctx, postID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return v.LikesCount, nil
}

// mapErr 把存储错误分成"该透传的"与"该包装的"。
func (s *Service) mapErr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
}

// maxCommentRunes 评论长度上限。
const maxCommentRunes = 500

// AddComment 发评论。校验 → 帖子存在 → 事务落库(+计数)。
// 返回视图里的 Author 只有 ID —— 评论者的昵称头像前端本来就有(自己的用户态),
// 要"权威数据"刷新列表时 ListComments 会 JOIN 出全量作者信息。
func (s *Service) AddComment(ctx context.Context, userID, postID int64, content string) (*CommentView, error) {
	content = trimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("评论内容不能为空")
	}
	if n := utf8.RuneCountInString(content); n > maxCommentRunes {
		return nil, fmt.Errorf("评论最多 %d 个字", maxCommentRunes)
	}
	if _, err := s.store.GetByID(ctx, postID); err != nil {
		return nil, s.mapErr(err)
	}
	c := &Comment{PostID: postID, UserID: userID, Content: content}
	if err := s.store.AddComment(ctx, c); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return &CommentView{
		ID: c.ID, PostID: c.PostID, Content: c.Content, CreatedAt: c.CreatedAt,
		Author: Author{ID: userID},
	}, nil
}

// ListComments 单帖评论,按时间升序(对话按发生顺序读)。
func (s *Service) ListComments(ctx context.Context, postID int64) ([]CommentView, error) {
	// 帖子不存在/已删 → 404;评论列表不挂在幽灵帖上
	if _, err := s.store.GetByID(ctx, postID); err != nil {
		return nil, s.mapErr(err)
	}
	cs, err := s.store.ListComments(ctx, postID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return cs, nil
}

// DeletePost 删除。软删 + 只有作者本人能动 —— 作者校验在 Service 而不是
// handler:这是业务规则("谁的帖子谁能删"),不是 HTTP 层的职责。
func (s *Service) DeletePost(ctx context.Context, userID, postID int64) error {
	v, err := s.store.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if v.UserID != userID {
		return ErrNotAuthor
	}
	if err := s.store.Delete(ctx, postID); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return nil
}
