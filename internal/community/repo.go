// repo.go —— Post 存取。Store 接口 + GORM 实现,照 auth.Store 同一惯例:
// 单测塞内存 fake,不碰真库;GORM 实现是唯一知道表长什么样的地方。
package community

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// ErrNotFound 帖子不存在(或已软删 —— 软删行被查询自动过滤,语义等同不存在)。
var ErrNotFound = errors.New("post not found")

// isDuplicateKey 唯一键冲突判定,与 auth 包同名函数同一实现、同一理由:
// errors.As 断言错误**类型**(MySQL 1062)而不是匹配报错文案。
// 不从 auth 包 import 是刻意的:为 4 行函数拉进账号包的依赖,得不偿失。
func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// Store 帖子存储接口。
type Store interface {
	// Create 落库。plan_snapshot/title 等字段在 Service 层已校验,这里信任入参。
	Create(ctx context.Context, p *Post) error
	// GetByID 单帖(含作者信息)。软删行等同不存在。
	GetByID(ctx context.Context, id int64) (*PostView, error)
	// List 游标分页:取 id < beforeID 的前 limit 条(按 id 降序)。
	// beforeID <= 0 表示第一页(从最新开始)。
	List(ctx context.Context, beforeID int64, limit int) ([]PostView, error)
	// Delete 软删。仅把行标记删除,不做物理 DELETE。
	Delete(ctx context.Context, id int64) error
	// Like 点赞。返回是否"本次新建"—— 已赞过(主键冲突)返回 false 且**不改计数**。
	// 行写入与计数更新在同一个事务里:计数与事实源(post_likes 行数)永远一致。
	Like(ctx context.Context, userID, postID int64) (created bool, err error)
	// Unlike 取赞。返回是否"本次真的删了行"—— 没赞过返回 false,计数不动。
	Unlike(ctx context.Context, userID, postID int64) (removed bool, err error)
	// LikedIDs 批量查"这个用户在这些帖子里赞过哪些"。一条 IN 查询,
	// 服务层拿它回填 is_liked —— 列表页绝不做逐帖查询(N+1)。
	LikedIDs(ctx context.Context, userID int64, postIDs []int64) (map[int64]bool, error)
	// AddComment 发评论。行写入与 comments_count 更新同一事务。
	AddComment(ctx context.Context, c *Comment) error
	// ListComments 单帖评论,按时间升序(与帖子列表降序相反:对话按发生顺序读)。
	ListComments(ctx context.Context, postID int64) ([]CommentView, error)
}

// GormStore 真库实现。复用 repo.NewGorm 建好的连接 —— 一次连接多包共用。
type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore { return &GormStore{db: db} }

// Migrate 建 posts/post_likes/comments 三张表。启动时跑一次,与 auth/repo/settings
// 的迁移同一时机。⚠️ 新增实体必须加进这里 —— AutoMigrate 只管列出的模型,
// M2→M3 加表时漏掉过,症状是单测全绿、真库 503(Table doesn't exist)。
func (s *GormStore) Migrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&Post{}, &PostLike{}, &Comment{})
}

func (s *GormStore) Create(ctx context.Context, p *Post) error {
	return s.db.WithContext(ctx).Create(p).Error
}

// postViewRow 列表/详情的扫描目标:posts 行 + JOIN 出来的作者列。
// 手写列清单而不是 Select("*"):**显式点名要的列**,
// 防止"加了一列忘了同步查询"这类隐式破坏。
type postViewRow struct {
	Post
	AuthorID       int64
	AuthorUsername string
	AuthorNickname string
	AuthorAvatar   string
}

// viewColumns posts 与 users 的列清单。表前缀必须在 ORDER BY / WHERE 里保持一致,
// JOIN 后重名列(id/created_at 两表都有)不指定前缀会产生歧义错误。
const viewColumns = "posts.id, posts.user_id, posts.title, posts.content, " +
	"posts.plan_snapshot, posts.origin_name, posts.dest_name, posts.image_urls, " +
	"posts.likes_count, posts.comments_count, posts.created_at, " +
	"users.id AS author_id, users.username AS author_username, " +
	"users.nickname AS author_nickname, users.avatar_url AS author_avatar"

// notDeleted 软删过滤。⚠️ 必须手写:GORM 的软删条件只在查询挂了 Model()(含
// DeletedAt 字段)时自动追加;这里用 Table("posts")+JOIN+自定义列,它不会管 ——
// 漏了这一句,已删的帖子就会"诈尸"出现在瀑布流里,且不会有任何报错。
const notDeleted = "posts.deleted_at IS NULL"

func (r postViewRow) toView(withSnapshot bool) *PostView {
	v := &PostView{
		ID:            r.Post.ID,
		UserID:        r.Post.UserID,
		Title:         r.Post.Title,
		Content:       r.Post.Content,
		OriginName:    r.Post.OriginName,
		DestName:      r.Post.DestName,
		ImageURLs:     r.Post.ImageURLs,
		LikesCount:    r.Post.LikesCount,
		CommentsCount: r.Post.CommentsCount,
		CreatedAt:     r.Post.CreatedAt,
		Author: Author{
			ID:        r.AuthorID,
			Username:  r.AuthorUsername,
			Nickname:  r.AuthorNickname,
			AvatarURL: r.AuthorAvatar,
		},
	}
	if withSnapshot {
		v.Snapshot = r.Post.PlanSnapshot
	}
	return v
}

func (s *GormStore) GetByID(ctx context.Context, id int64) (*PostView, error) {
	var row postViewRow
	err := s.db.WithContext(ctx).
		Table("posts").
		Select(viewColumns).
		Joins("JOIN users ON users.id = posts.user_id").
		Where(notDeleted).
		Where("posts.id = ?", id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row.toView(true), nil
}

func (s *GormStore) List(ctx context.Context, beforeID int64, limit int) ([]PostView, error) {
	q := s.db.WithContext(ctx).
		Table("posts").
		Select(viewColumns).
		Joins("JOIN users ON users.id = posts.user_id")
	// 游标语义:id < beforeID。首页不传 → 不加条件,天然从最新开始。
	// 不用 OFFSET:翻到深处 OFFSET 要扫过前面所有行,游标只走索引一次命中。
	if beforeID > 0 {
		q = q.Where("posts.id < ?", beforeID)
	}
	q = q.Where(notDeleted)
	var rows []postViewRow
	// 索引 (deleted_at, id) 支撑"软删过滤 + id 降序"的复合条件
	err := q.Order("posts.id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PostView, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r.toView(false))
	}
	return out, nil
}

func (s *GormStore) Delete(ctx context.Context, id int64) error {
	// Delete + DeletedAt 字段 = GORM 自动软删(UPDATE deleted_at)
	res := s.db.WithContext(ctx).Delete(&Post{}, "id = ?", id)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// Like 点赞:插入 + 计数在同一事务。主键冲突(已赞过)→ created=false,
// 事务整体无副作用 —— 幂等语义的根基就在这把复合主键上。
func (s *GormStore) Like(ctx context.Context, userID, postID int64) (bool, error) {
	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		l := &PostLike{UserID: userID, PostID: postID}
		if err := tx.Create(l).Error; err != nil {
			if isDuplicateKey(err) {
				return nil // 已赞过:提交这个什么都没做的"事务",幂等放行
			}
			return err
		}
		// UpdateColumn 绕过 GORM 的 hooks/自动更新时间戳 —— 计数是纯原子自增,
		// gorm.Expr 生成 "likes_count + 1" 让数据库自己算,不读-改-写(有并发窗口)
		if err := tx.Model(&Post{}).Where("id = ?", postID).
			UpdateColumn("likes_count", gorm.Expr("likes_count + 1")).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// Unlike 取赞:删行 + 计数在同一事务。删了行才减计数 ——
// 没赞过的人来取赞,RowsAffected=0,计数原样不动(防减成负数)。
func (s *GormStore) Unlike(ctx context.Context, userID, postID int64) (bool, error) {
	removed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND post_id = ?", userID, postID).Delete(&PostLike{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 没赞过,幂等放行
		}
		if err := tx.Model(&Post{}).Where("id = ?", postID).
			UpdateColumn("likes_count", gorm.Expr("likes_count - 1")).Error; err != nil {
			return err
		}
		removed = true
		return nil
	})
	return removed, err
}

func (s *GormStore) LikedIDs(ctx context.Context, userID int64, postIDs []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(postIDs) == 0 {
		return out, nil
	}
	var likes []PostLike
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND post_id IN ?", userID, postIDs).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	for _, l := range likes {
		out[l.PostID] = true
	}
	return out, nil
}

func (s *GormStore) AddComment(ctx context.Context, c *Comment) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		return tx.Model(&Post{}).Where("id = ?", c.PostID).
			UpdateColumn("comments_count", gorm.Expr("comments_count + 1")).Error
	})
}

func (s *GormStore) ListComments(ctx context.Context, postID int64) ([]CommentView, error) {
	var rows []struct {
		Comment
		AuthorID       int64
		AuthorUsername string
		AuthorNickname string
		AuthorAvatar   string
	}
	err := s.db.WithContext(ctx).
		Table("comments").
		Select("comments.id, comments.post_id, comments.content, comments.created_at, "+
			"users.id AS author_id, users.username AS author_username, "+
			"users.nickname AS author_nickname, users.avatar_url AS author_avatar").
		Joins("JOIN users ON users.id = comments.user_id").
		// 与 posts 的查询同一坑:Table+JOIN 不带软删过滤,必须手写
		Where("comments.deleted_at IS NULL AND comments.post_id = ?", postID).
		Order("comments.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]CommentView, 0, len(rows))
	for _, r := range rows {
		out = append(out, CommentView{
			ID:        r.ID,
			PostID:    r.PostID,
			Content:   r.Content,
			CreatedAt: r.CreatedAt,
			Author: Author{
				ID:        r.AuthorID,
				Username:  r.AuthorUsername,
				Nickname:  r.AuthorNickname,
				AvatarURL: r.AuthorAvatar,
			},
		})
	}
	return out, nil
}
