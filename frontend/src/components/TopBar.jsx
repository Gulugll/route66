// TopBar.jsx —— 顶栏
//
// 三块:品牌 + 视图导航(规划/社区) + 分享与用户区。
// 游客根本走不到这一步(App 会先渲染登录页)。
// 已登录时 session 过期由 App 的登录墙兜底:/auth/me 401 → user=null → 回登录页。

import { Avatar } from './ProfileDialog.jsx'
import { Icon } from './icons.jsx'

export function TopBar({ user, view, onNavView, onShare, onLogout, onOpenProfile }) {
  // 昵称为空回退 username —— 后端允许空昵称,展示规则收口在这一处
  const displayName = user.nickname || user.username
  const navBtn = (key, label) => (
    <button
      className={`topbar-nav${view === key ? ' on' : ''}`}
      onClick={() => onNavView(key)}
    >
      {label}
    </button>
  )
  return (
    <header className="topbar">
      <div className="brand">
        <Icon name="route" size={26} style={{ color: 'var(--driving)' }} />
        <span className="brand-title">路线规划器</span>
      </div>

      <div className="topbar-navs">
        {navBtn('planner', '规划')}
        {navBtn('community', '社区')}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        {view === 'planner' && (
          // 分享按钮不设 disabled:条件不满足时点了给明确提示,
          // 比一块点了没反应的灰按钮诚实 —— 用户永远不该对着禁用态猜原因
          <button className="btn-ghost" onClick={onShare}>
            分享到社区
          </button>
        )}
        <button
          className="btn-ghost"
          onClick={onOpenProfile}
          style={{ display: 'flex', alignItems: 'center', gap: 8 }}
          title="编辑账号资料"
        >
          <Avatar avatarUrl={user.avatar_url} username={user.username} size={22} />
          <span style={{ fontSize: '13px', color: 'var(--muted)' }}>
            {displayName}
            {user.role === 'admin' ? ' · 管理员' : ''}
          </span>
        </button>
        <button className="btn-ghost" onClick={onLogout}>
          退出
        </button>
      </div>
    </header>
  )
}
