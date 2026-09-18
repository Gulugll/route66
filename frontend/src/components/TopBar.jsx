// TopBar.jsx —— 顶栏
//
// 登录墙改造后这里只剩"已登录"一种形态：用户名 + 退出。
// 游客根本走不到这一步（App 会先渲染登录页），所以"登录"按钮不再存在；
// "设置"按钮也随设置入口一并退役 —— 高德 key 由管理员在管理台（7801）统一配置。
// 已登录时 session 过期由 App 的登录墙兜底：/auth/me 401 → user=null → 回登录页。

import { Icon } from './icons.jsx'

export function TopBar({ user, onLogout }) {
  return (
    <header className="topbar">
      <div className="brand">
        <Icon name="route" size={26} style={{ color: 'var(--driving)' }} />
        <span className="brand-title">路线规划器</span>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <span
          style={{ fontSize: '13px', color: 'var(--muted)' }}
          title={'角色: ' + user.role}
        >
          {user.username}
          {user.role === 'admin' ? ' · 管理员' : ''}
        </span>
        <button className="btn-ghost" onClick={onLogout}>
          退出
        </button>
      </div>
    </header>
  )
}
