// Package community —— 社区模块:路线笔记的发布/浏览/删除(M2)。
//
// 与 auth/repo 同一条设计线:
//  1. 存储走 Store 接口 —— GORM 实现管真库,内存 fake 管单测;
//  2. 校验收口在 Service 层,handler 只做绑定与状态码;
//  3. api 层通过 Option 注入,没装配就不注册路由(未部署的功能 404 比 503 准确)。
//
// 核心设计决策:帖子内容**自包含**。plan_snapshot 是发布时刻的路线快照(JSON),
// 不引用 plans 表 —— 作者删掉自己的规划任务,帖子不能跟着变死链;
// 「按此路线开始规划」也只需要反序列化这一份 JSON,零依赖。
// 代价是冗余存储,一份路线坐标几 KB,可接受。
package community

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// StringList 存进 json 列的字符串数组。
// 显式实现 Value/Scan 而不是依赖 GORM 对切片的隐式处理:
// 隐式行为因驱动/版本而异,显式实现让"存的是 JSON 数组"成为契约。
type StringList []string

func (s StringList) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *StringList) Scan(v any) error {
	if v == nil {
		*s = StringList{}
		return nil
	}
	switch b := v.(type) {
	case []byte:
		return json.Unmarshal(b, s)
	case string:
		return json.Unmarshal([]byte(b), s)
	default:
		return fmt.Errorf("StringList: 不支持的扫描类型 %T", v)
	}
}

// Post 路线笔记。gorm 标签 = 表结构唯一真相源(与 repo.Task/auth.User 同一约定)。
type Post struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// 作者。 community 帖子永远是登录用户的,没有游客帖。
	UserID int64  `gorm:"column:user_id;not null;index:idx_user_del,priority:1" json:"user_id"`
	Title  string `gorm:"column:title;type:varchar(30);not null" json:"title"`
	// 正文 ≤2000 字,发布时校验
	Content string `gorm:"column:content;type:varchar(2000);not null" json:"content"`
	// 路线快照(JSON 列)。校验规则见 validateSnapshot —— 存的是"发布时刻验证过
	// 的合法 JSON",读的时候不再校验(写入口只有一个,读侧信任写入侧)。
	PlanSnapshot json.RawMessage `gorm:"column:plan_snapshot;type:json;not null" json:"-"`
	// 图片 URL 列表(最多 9 张,发布时校验)。URL 由 /community/images 上传接口生成
	// (固定 /uploads/posts/ 前缀),不接受外链 —— 帖子里的图必须是我们自己存的。
	ImageURLs StringList `gorm:"column:image_urls;type:json" json:"image_urls"`
	// 起终点地名冗余列:瀑布流卡片只渲染标题+起终点,不该为列表页解析每行的 JSON
	OriginName string `gorm:"column:origin_name;type:varchar(64);not null" json:"origin_name"`
	DestName   string `gorm:"column:dest_name;type:varchar(64);not null" json:"dest_name"`
	// 计数冗余在行上,避免列表页逐帖 COUNT(M3 起由点赞/评论事务维护)
	LikesCount    int `gorm:"column:likes_count;not null;default:0" json:"likes_count"`
	CommentsCount int `gorm:"column:comments_count;not null;default:0" json:"comments_count"`
	// 软删:作者"删帖"后数据仍在(误删可恢复/审计),查询自动过滤。
	// DeletedAt 置空指针是 gorm.DeletedAt 的约定,任何 Where 都自动追加 deleted_at IS NULL
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
	CreatedAt time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (Post) TableName() string { return "posts" }

// PostLike 点赞记录。复合主键 (user_id, post_id) 是**防重复赞的唯一防线**:
// 数据库约束,不是"先查再插" —— 先查后插在并发下必现双插(两个请求同时查都没有)。
// 幂等由约束兜底:重复 INSERT 报 1062,业务层翻译成"已赞过",不报错。
type PostLike struct {
	// 复合主键的两个成员都必须显式 autoIncrement:false,
	// 否则 GORM 建表会给 int64 主键默认加 AUTO_INCREMENT
	UserID    int64     `gorm:"column:user_id;primaryKey;autoIncrement:false" json:"user_id"`
	PostID    int64     `gorm:"column:post_id;primaryKey;autoIncrement:false" json:"post_id"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

func (PostLike) TableName() string { return "post_likes" }

// Comment 评论。软删字段保留(后续"删评论"功能直接可用),M3 无删除端点。
type Comment struct {
	ID      int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	PostID  int64  `gorm:"column:post_id;not null;index" json:"post_id"`
	UserID  int64  `gorm:"column:user_id;not null" json:"user_id"`
	Content string `gorm:"column:content;type:varchar(500);not null" json:"content"`
	// 评论列表按时间升序分页;deleted_at 上不做复合索引(查询恒为单帖全量,量小)
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at" json:"-"`
	CreatedAt time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (Comment) TableName() string { return "comments" }

// CommentView 对外视图:评论 + 作者展示信息。
type CommentView struct {
	ID        int64     `json:"id"`
	PostID    int64     `json:"post_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Author    Author    `json:"author"`
}

// Author 作者的展示信息。瀑布流/详情页都要渲染"谁发的"。
// 用独立小结构而不是嵌套整个 auth.User:密码哈希等敏感字段根本不该离开 auth 包。
type Author struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

// DisplayName 昵称为空回退用户名 —— 展示规则收口在这一处,前后端共用一条逻辑。
// (前端 TopBar 同款回退;后端兜一次底,两层都不用记"另一层做了没"。)
func (a Author) DisplayName() string {
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.Username
}

// PostView 对外视图:Post + 作者信息,快照只在详情带。
type PostView struct {
	ID            int64           `json:"id"`
	UserID        int64           `json:"user_id"`
	Title         string          `json:"title"`
	Content       string          `json:"content"`
	OriginName    string          `json:"origin_name"`
	DestName      string          `json:"dest_name"`
	ImageURLs     StringList      `json:"image_urls"`
	Snapshot      json.RawMessage `json:"snapshot,omitempty"` // 详情才有,列表页 omitempty
	LikesCount    int             `json:"likes_count"`
	CommentsCount int             `json:"comments_count"`
	// 当前查看者是否已赞。游客恒 false;由 Service 用一次 IN 查询批量回填,
	// 列表页绝不逐帖查"我赞过没有"(N+1 查询是列表页的头号杀手)
	IsLiked   bool      `json:"is_liked"`
	CreatedAt time.Time `json:"created_at"`
	Author    Author    `json:"author"`
}
