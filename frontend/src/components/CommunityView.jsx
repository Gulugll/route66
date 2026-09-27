// CommunityView.jsx —— 社区页:双列瀑布流 + 笔记详情。
//
// 视图内二级导航:列表 ↔ 详情是本组件的内部 state(selectedId),
// 不进 App 的顶层视图状态 —— 详情只是列表的"放大",返回列表不应丢失滚动位置之外的东西。
// 但"按此路线开始规划"要把快照交回 App(它持有规划器状态),所以 onImport 由 App 注入。
//
// 瀑布流用 CSS columns 实现,不引第三方库 —— 卡片高度不一时 columns
// 按"填充顺序逐列放",与小红书的"逐行放到最矮列"略有差别,当前规模不值得为它引 JS。

import { useCallback, useEffect, useState } from 'react'

import { deletePost, fetchComments, fetchPost, likePost, listCommunityPosts, unlikePost, addComment } from '../api.js'
import { Avatar } from './ProfileDialog.jsx'
import { MODE_COLORS, MODE_LABELS } from '../theme.js'
import { Icon } from './icons.jsx'
import { PostMap } from './PostMap.jsx'

// coverHue 由帖子 id 派生封面色相:同一篇帖子永远同色(刷新/他人访问一致),
// 不同帖子色彩分散 —— 137° 是黄金角,相邻 id 的色相离得最远。
function coverHue(id) {
  return (id * 137) % 360
}

// coverStyle 站牌封面的底色与文字色:同色相、极浅底(92% 明度)配深字(30%),
// 彩色只作为"这篇帖子的身份色"存在,交互仍是墨色。
function coverStyle(id) {
  const h = coverHue(id)
  return {
    background: `hsl(${h}, 42%, 93%)`,
    color: `hsl(${h}, 45%, 30%)`,
  }
}

// timeAgo 相对时间。收口在这里:卡片、详情、评论都用。
function timeAgo(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return '刚刚'
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  return new Date(iso).toLocaleDateString('zh-CN')
}

export function CommunityView({ user, onImport, onBack, onError }) {
  const [selectedId, setSelectedId] = useState(0)
  // refreshKey:详情页的点赞/删帖会改列表数据,返回列表时强制重拉
  const [refreshKey, setRefreshKey] = useState(0)

  if (selectedId) {
    return (
      <PostDetail
        postId={selectedId}
        user={user}
        onBack={() => {
          setSelectedId(0)
          setRefreshKey((k) => k + 1)
        }}
        onImport={onImport}
        onDeleted={() => {
          setSelectedId(0)
          setRefreshKey((k) => k + 1)
        }}
        onError={onError}
      />
    )
  }
  return (
    <PostList
      user={user}
      refreshKey={refreshKey}
      onOpen={setSelectedId}
      onBack={onBack}
      onError={onError}
    />
  )
}

// ── 列表(瀑布流) ──

function PostList({ refreshKey, onOpen, onBack, onError }) {
  const [posts, setPosts] = useState([])
  const [cursor, setCursor] = useState(0) // 下一页游标;0 = 没有更多
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(
    async (beforeId, replace) => {
      setLoading(true)
      try {
        const data = await listCommunityPosts(beforeId)
        setPosts((prev) => (replace ? data.posts : [...prev, ...data.posts]))
        setCursor(data.next_cursor)
      } catch (err) {
        onError(err.message)
      } finally {
        setLoading(false)
        setLoaded(true)
      }
    },
    [onError]
  )

  // 首次挂载 + 从详情返回(refreshKey 变化)时重拉第一页
  useEffect(() => {
    load(0, true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshKey])

  return (
    <div className="community-page">
      <div className="community-header">
        <button className="btn-ghost" onClick={onBack}>← 返回规划</button>
        <span className="community-title">社区 · 路线笔记</span>
        <span style={{ width: 76 }} /> {/* 占位保持标题居中 */}
      </div>

      {loaded && posts.length === 0 && (
        <div className="community-empty">还没有人分享路线 —— 回规划页做一条,点「分享到社区」</div>
      )}

      <div className="community-grid">
        {posts.map((p) => {
          const cover = p.image_urls?.[0]
          return (
            <div key={p.id} className="community-card" onClick={() => onOpen(p.id)}>
              {/* 封面:有图用第一张图(object-fit:cover 裁到统一高度),
                  无图落回站牌线稿 —— 两类卡片在网格里等高等大。
                  站牌封面的底色仍由帖子 id 派生,线稿是真实信息的抽象。 */}
              {cover ? (
                <div className="community-cover community-cover--img">
                  <img src={cover} alt="" loading="lazy" />
                </div>
              ) : (
                <div className="community-cover" style={coverStyle(p.id)}>
                  <span className="cover-stop">{p.origin_name}</span>
                  <span className="cover-line" />
                  <span className="cover-stop">{p.dest_name}</span>
                </div>
              )}
              <div className="community-card-body">
                <div className="community-card-title">{p.title}</div>
                <div className="community-card-meta">
                  <Avatar avatarUrl={p.author.avatar_url} username={p.author.username} size={20} />
                  <span className="community-card-name">{p.author.nickname || p.author.username}</span>
                  <span style={{ flex: 1 }} />
                  {p.image_urls?.length > 1 && (
                    <span className="community-img-count" title={`${p.image_urls.length} 张图`}>
                      ▣ {p.image_urls.length}
                    </span>
                  )}
                  <Icon name="heart" size={13} />
                  {p.likes_count}
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {cursor > 0 && (
        <div style={{ textAlign: 'center', padding: '4px 0 20px' }}>
          <button className="btn-ghost" onClick={() => load(cursor, false)} disabled={loading}>
            {loading ? '加载中…' : '加载更多'}
          </button>
        </div>
      )}
    </div>
  )
}

// ── 详情 ──

function PostDetail({ postId, user, onBack, onImport, onDeleted, onError }) {
  const [post, setPost] = useState(null)
  const [comments, setComments] = useState([])
  const [commentText, setCommentText] = useState('')
  const [busy, setBusy] = useState(false)

  const loadAll = useCallback(async () => {
    try {
      const [d, c] = await Promise.all([fetchPost(postId), fetchComments(postId)])
      setPost(d.post)
      setComments(c.comments)
    } catch (err) {
      onError(err.message)
    }
  }, [postId, onError])

  useEffect(() => {
    loadAll()
  }, [loadAll])

  if (!post) {
    return (
      <div className="community-page">
        <div className="community-header">
          <button className="btn-ghost" onClick={onBack}>← 返回列表</button>
        </div>
        <div className="community-empty">加载中…</div>
      </div>
    )
  }

  const isAuthor = user && user.id === post.user_id
  // 快照解析失败不阻断浏览(正文/评论照常),只是地图和导入不可用。
  // ⚠️ fetch 已把响应 JSON.parse 过,post.snapshot 到手就是对象;
  // 这里只兼容"万一是字符串"的形态 —— 对对象再 JSON.parse 会先被
  // toString 成 "[object Object]" 然后必抛错,是一个真实踩过的坑。
  let snapshot = null
  try {
    if (!post.snapshot) snapshot = null
    else if (typeof post.snapshot === 'string') snapshot = JSON.parse(post.snapshot)
    else snapshot = post.snapshot
  } catch {
    snapshot = null
  }

  const toggleLike = async () => {
    if (!user) return
    try {
      const data = post.is_liked ? await unlikePost(post.id) : await likePost(post.id)
      setPost((p) => ({ ...p, is_liked: data.liked, likes_count: data.likes_count }))
    } catch (err) {
      onError(err.message)
    }
  }

  const submitComment = async () => {
    if (!commentText.trim() || busy) return
    setBusy(true)
    try {
      await addComment(post.id, commentText.trim())
      setCommentText('')
      await loadAll() // 重拉评论与计数,保证与后端一致
    } catch (err) {
      onError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async () => {
    if (!confirm('删除这篇笔记?')) return
    try {
      await deletePost(post.id)
      onDeleted()
    } catch (err) {
      onError(err.message)
    }
  }

  const handleImport = () => {
    if (!snapshot) {
      onError('这篇笔记没有可用的路线快照')
      return
    }
    onImport(snapshot)
  }

  return (
    <div className="community-page">
      <div className="community-header">
        <button className="btn-ghost" onClick={onBack}>← 返回列表</button>
        {isAuthor ? (
          <button className="btn-ghost" onClick={handleDelete}>删除</button>
        ) : (
          <button className="btn-primary" style={{ padding: '6px 14px', fontSize: 13 }} onClick={handleImport}>
            按此路线开始规划
          </button>
        )}
      </div>

      <div className="post-detail">
        <h2 className="post-title">{post.title}</h2>
        <div className="post-byline">
          <Avatar avatarUrl={post.author.avatar_url} username={post.author.username} size={26} />
          <span className="post-byline-name">{post.author.nickname || post.author.username}</span>
          <span className="post-byline-time">{timeAgo(post.created_at)}</span>
        </div>

        {snapshot ? (
          <PostMap snapshot={snapshot} />
        ) : (
          <div className="community-empty">该笔记未携带路线快照</div>
        )}

        {/* 图片网格:有图才渲染。单图大图、多图等分九宫格 */}
        {post.image_urls?.length > 0 && (
          <div className={`post-images post-images--${post.image_urls.length === 1 ? 'single' : 'grid'}`}>
            {post.image_urls.map((u) => (
              <img key={u} src={u} alt="" loading="lazy" />
            ))}
          </div>
        )}

        <p className="post-content">{post.content}</p>

        <div className="post-like-row">
          <button
            className={`btn-ghost like-btn${post.is_liked ? ' liked' : ''}`}
            onClick={toggleLike}
            disabled={!user}
            title={user ? '' : '登录后才能点赞'}
          >
            <Icon name="heart" size={15} /> {post.likes_count}
          </button>
          {snapshot && (
            <span className="post-route-line">
              {snapshot.points.map((pt, i) => (
                <span key={i}>
                  {i > 0 && (
                    <span
                      style={{ color: MODE_COLORS[pt.legMode] || MODE_COLORS.driving, margin: '0 4px' }}
                      title={MODE_LABELS[pt.legMode] || pt.legMode}
                    >
                      →
                    </span>
                  )}
                  {pt.name}
                </span>
              ))}
            </span>
          )}
        </div>

        <div className="comment-section">
          <div className="comment-heading">评论 {comments.length}</div>
          {comments.map((c) => (
            <div key={c.id} className="comment-item">
              <Avatar avatarUrl={c.author.avatar_url} username={c.author.username} size={26} />
              <div>
                <div className="comment-author">
                  {c.author.nickname || c.author.username}
                  <span className="comment-time">{timeAgo(c.created_at)}</span>
                </div>
                <div className="comment-content">{c.content}</div>
              </div>
            </div>
          ))}
          <div className="comment-input-row">
            <input
              className="input"
              placeholder={user ? '说点什么…' : '登录后参与评论'}
              value={commentText}
              maxLength={520}
              disabled={!user}
              onChange={(e) => setCommentText(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submitComment()}
            />
            <button className="btn-primary" onClick={submitComment} disabled={!user || busy || !commentText.trim()}>
              发送
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
