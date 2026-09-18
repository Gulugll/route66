// LoginPage.jsx —— 独立的整页登录页（登录墙的"正面"）
//
// 为什么是独立整页而不是弹窗：弹窗的天职是"浮在内容上"（半透明蒙层 +
// fixed 定位），登录墙要求的是"背后根本没有内容" —— 借用弹窗样式做登录页,
// 半透明蒙层就会把底下的东西透出来(2026-09-18 用户实测踩到)。整页 + 不透明
// 背景,视觉和语义都干净。
//
// 登录/注册成功后执行 location.reload() 整页跳转进主界面,两个理由:
//   1) 体验上就是用户理解的"跳转":干净的新页面,浏览器地址栏不变但视图全新;
//   2) 正确性(更重要):主界面的地图容器是登录后才挂载的,而 useAmap 的
//      初始化 effect 依赖 [key, jscode] 早就跑过了 —— 直接切视图地图会
//      永远空白。整页重挂,所有 effect 在容器就位后重新执行,最稳。
//
// 键盘监听(Enter 提交)挂在自己的表单上,不走 document —— 这里没有"要被
// Esc 关掉"的概念,StrictMode 的清理规则照样遵守。

import { useRef, useState } from 'react'
import { TextField } from './ui.jsx'
import { Icon } from './icons.jsx'

export function LoginPage({ auth }) {
  const [mode, setMode] = useState('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const firstFieldRef = useRef(null)
  // 首字段聚焦:ref 回调在真实 DOM 挂上时执行,等价于旧弹窗的 effect 版本
  const focusFirst = (el) => el && el.focus()

  const isLogin = mode === 'login'

  async function handleSubmit(e) {
    e?.preventDefault?.()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (isLogin) {
        await auth.login(username, password)
      } else {
        await auth.register(username, password)
      }
      // 成功:会话 cookie 已种下,整页跳转进主界面(理由见文件头)
      window.location.reload()
    } catch (err) {
      setError(err.message) // 后端的中文错误直接展示(含防枚举的统一文案)
      setBusy(false)
    }
  }

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
      <div style={{ width: '100%', maxWidth: 380 }}>
        {/* 品牌区:和主界面 TopBar 同一套视觉,让"这是同一个产品"一眼成立 */}
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
          <span style={{ fontSize: 20, fontWeight: 500, color: 'var(--ink)' }}>
            路线规划器
          </span>
        </div>

        {/* 表单卡片:白底不透明,和页面底色分离出层次 */}
        <form
          onSubmit={handleSubmit}
          style={{
            background: 'var(--bg-surface)',
            border: '1px solid var(--border)',
            borderRadius: 12,
            padding: '24px 22px',
          }}
        >
          <div style={{ fontSize: 15, fontWeight: 500, color: 'var(--ink)', marginBottom: 14 }}>
            {isLogin ? '登录' : '注册'}
          </div>

          <TextField
            label="用户名（3~32 个字符）"
            value={username}
            onChange={(v) => {
              setUsername(v)
              setError('')
            }}
            onEnter={handleSubmit}
            inputRef={focusFirst}
          />

          <div style={{ height: 12 }} />

          <TextField
            label="密码（至少 6 位）"
            value={password}
            onChange={(v) => {
              setPassword(v)
              setError('')
            }}
            onEnter={handleSubmit}
            error={error}
          />

          <button
            type="submit"
            className="btn btn--primary"
            disabled={busy}
            style={{ width: '100%', marginTop: 16, height: 38 }}
          >
            {busy ? '提交中…' : isLogin ? '登录' : '注册并登录'}
          </button>

          <div style={{ marginTop: 14, fontSize: 13, color: 'var(--text-secondary)', textAlign: 'center' }}>
            {isLogin ? (
              <>
                没有账号？{' '}
                <a
                  href="#register"
                  style={{ color: 'var(--ink)' }}
                  onClick={(e) => {
                    e.preventDefault()
                    setMode('register')
                    setError('')
                  }}
                >
                  注册一个
                </a>
              </>
            ) : (
              <>
                已有账号？{' '}
                <a
                  href="#login"
                  style={{ color: 'var(--ink)' }}
                  onClick={(e) => {
                    e.preventDefault()
                    setMode('login')
                    setError('')
                  }}
                >
                  去登录
                </a>
              </>
            )}
            <br />
            登录后才能使用路线规划功能；管理员请使用
            <a href="http://localhost:7801" style={{ color: 'var(--ink)' }}>管理台</a>（端口 7801）。
          </div>
        </form>
      </div>
    </div>
  )
}
