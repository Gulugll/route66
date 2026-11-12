// LoginPage.jsx —— 登录墙（SSO 版）
//
// 密码登录/注册已移除：身份由统一登录中心（Casdoor OIDC）认证。
// 本页只有一个「使用 DUX SSO 登录」按钮，点击整页跳 /auth/login（后端 302 到授权页）。
// 退出登录走 /auth/login?prompt=login —— 强制显示登录表单，可换账号。
//
// 保留整页跳转进主界面的旧约定（useAmap 的 effect 依赖容器就位，见 git 历史）。

import { Icon } from './icons.jsx'

export function LoginPage() {
  return (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--bg-page)',
        display: 'grid',
        placeItems: 'center',
        padding: '24px 16px',
        fontFamily: 'inherit',
      }}
    >
      <div style={{ width: '100%', maxWidth: 380, textAlign: 'center' }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 10,
            marginBottom: 20,
          }}
        >
          <Icon name="route" size={30} style={{ color: 'var(--driving)' }} />
          <span style={{ fontSize: 20, fontWeight: 500, color: 'var(--ink)' }}>路线规划器</span>
        </div>

        <div
          style={{
            background: 'var(--bg-surface)',
            border: '1px solid var(--border)',
            borderRadius: 12,
            padding: '28px 22px',
          }}
        >
          <div style={{ fontSize: 15, fontWeight: 500, color: 'var(--ink)', marginBottom: 10 }}>
            登录后继续
          </div>
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 18 }}>
            使用统一账号（DUX SSO）登录，支持路线规划与社区功能。
          </p>
          <a
            className="btn btn--primary"
            href="/auth/login"
            style={{
              display: 'block',
              width: '100%',
              height: 38,
              lineHeight: '38px',
              textAlign: 'center',
              textDecoration: 'none',
            }}
          >
            使用 DUX SSO 登录 →
          </a>
        </div>
      </div>
    </div>
  )
}
