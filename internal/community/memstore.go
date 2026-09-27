// memstore.go —— Store 的内存实现。
// 消费方是 community 包自己的单测 + api 包的 HTTP 契约测试 ——
// 放正式文件并导出,是因为 Go 的 _test.go 符号对其他包不可见;
// 与其让 api 包复制一份 fake,不如共用,行为永远同源。
// 它不依赖任何外部服务,生产路径不会用到。
package community

import (
	"context"
	"sort"
	"strconv"
	"sync"
)

// MemStore 导出的内存实现 —— api 包的 HTTP 契约测试也要塞 fake
// (与 memAuthStore 同理),与其复制一份,不如导出共用,行为永远同源。
type MemStore struct {
	mu          sync.Mutex
	posts       map[int64]*Post
	likes       map[int64]map[int64]bool // postID → userID → 已赞(复合主键的内存等价物)
	comments    map[int64][]Comment      // postID → 升序评论
	next        int64
	nextComment int64
}

func NewMemStore() *MemStore {
	return &MemStore{
		posts:    map[int64]*Post{},
		likes:    map[int64]map[int64]bool{},
		comments: map[int64][]Comment{},
	}
}

func (m *MemStore) Create(_ context.Context, p *Post) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	p.ID = m.next
	cp := *p
	m.posts[p.ID] = &cp
	return nil
}

// fakeUsername fake 里没有 users 表,作者名按确定性规则生成,
// 让"列表/详情带作者信息"这个契约可断言。
func fakeUsername(userID int64) string { return "user" + strconv.FormatInt(userID, 10) }

func (m *MemStore) GetByID(_ context.Context, id int64) (*PostView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.posts[id]
	if !ok || p.DeletedAt.Valid {
		return nil, ErrNotFound
	}
	cp := *p
	return &PostView{
		ID:         cp.ID,
		UserID:     cp.UserID,
		Title:      cp.Title,
		Content:    cp.Content,
		OriginName: cp.OriginName,
		DestName:   cp.DestName,
		Snapshot:   cp.PlanSnapshot,
		ImageURLs:  cp.ImageURLs,
		// M3 起计数会被读到(点赞/评论的返回值)——fake 与真库行为一致才有意义
		LikesCount:    cp.LikesCount,
		CommentsCount: cp.CommentsCount,
		CreatedAt:     cp.CreatedAt,
		Author:        Author{ID: cp.UserID, Username: fakeUsername(cp.UserID)},
	}, nil
}

func (m *MemStore) List(_ context.Context, beforeID int64, limit int) ([]PostView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]int64, 0, len(m.posts))
	for id, p := range m.posts {
		if p.DeletedAt.Valid {
			continue
		}
		if beforeID > 0 && id >= beforeID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]PostView, 0, len(ids))
	for _, id := range ids {
		p := m.posts[id]
		out = append(out, PostView{
			ID: p.ID, UserID: p.UserID, Title: p.Title, Content: p.Content,
			OriginName: p.OriginName, DestName: p.DestName, ImageURLs: p.ImageURLs,
			CreatedAt: p.CreatedAt,
			Author:    Author{ID: p.UserID, Username: fakeUsername(p.UserID)},
		})
	}
	return out, nil
}

func (m *MemStore) Delete(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.posts[id]
	if !ok || p.DeletedAt.Valid {
		return ErrNotFound
	}
	p.DeletedAt.Valid = true
	return nil
}

// ── 互动(M3):fake 里的"事务"就是锁内连续两步 —— 语义等价,单测足够 ──

func (m *MemStore) Like(_ context.Context, userID, postID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.likes[postID]; !ok {
		m.likes[postID] = map[int64]bool{}
	}
	if m.likes[postID][userID] {
		return false, nil // 已赞过,幂等放行
	}
	m.likes[postID][userID] = true
	if p, ok := m.posts[postID]; ok && !p.DeletedAt.Valid {
		p.LikesCount++
	}
	return true, nil
}

func (m *MemStore) Unlike(_ context.Context, userID, postID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.likes[postID][userID] {
		return false, nil // 没赞过,计数不动(防负数)
	}
	delete(m.likes[postID], userID)
	if p, ok := m.posts[postID]; ok {
		p.LikesCount--
	}
	return true, nil
}

func (m *MemStore) LikedIDs(_ context.Context, userID int64, postIDs []int64) (map[int64]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]bool{}
	for _, pid := range postIDs {
		if m.likes[pid][userID] {
			out[pid] = true
		}
	}
	return out, nil
}

func (m *MemStore) AddComment(_ context.Context, c *Comment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextComment++
	c.ID = m.nextComment
	cp := *c
	m.comments[cp.PostID] = append(m.comments[cp.PostID], cp)
	if p, ok := m.posts[c.PostID]; ok && !p.DeletedAt.Valid {
		p.CommentsCount++
	}
	return nil
}

func (m *MemStore) ListComments(_ context.Context, postID int64) ([]CommentView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cs := m.comments[postID]
	out := make([]CommentView, 0, len(cs))
	for _, c := range cs {
		out = append(out, CommentView{
			ID: c.ID, PostID: c.PostID, Content: c.Content, CreatedAt: c.CreatedAt,
			Author: Author{ID: c.UserID, Username: fakeUsername(c.UserID)},
		})
	}
	return out, nil
}
