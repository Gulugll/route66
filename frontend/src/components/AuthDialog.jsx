// AuthDialog.jsx —— 登录/注册（一个组件两副面孔，靠 mode 切换）
//
// mandatory 模式（登录墙）：未登录时整个页面只有它 —— 没有关闭按钮、
// 点蒙层不关、Esc 不关。登录/注册是唯一的出路，所以注册切换必须保留。
// 非 mandatory（旧弹窗形态）的关闭行为全部保留，两种模式一个组件。
//
// 为什么登录/注册合一个组件而不是两个：它们的字段、样式、提交逻辑
// 重合 90%，拆成两个组件反而要维护两份"错误提示 + 回车提交"。
// 判据还是那条：复用的是逻辑和行为，不是碰巧长得像。

import { useEffect, useRef, useState } from 'react'
import { TextField } from './ui.jsx'

export function AuthDialog({ mode = 'login', onSubmit, onClose, onSwitchMode, mandatory = false }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const firstFieldRef = useRef(null)

  useEffect(() => {
    firstFieldRef.current?.focus()
  }, [])

  // Esc 关闭 —— 只在"可选弹窗"模式下生效;登录墙里 Esc 关掉登录页没有意义
  useEffect(() => {
    if (mandatory) return
    const onKey = (e) => {
      if (e.key === 'Escape') onClose?.()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [mandatory, onClose])

  const isLogin = mode === 'login'

  async function handleSubmit() {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (isLogin) {
        await onSubmit.login(username, password)
      } else {
        await onSubmit.register(username, password)
      }
      onClose?.() // 成功:父组件的 user state 已更新,弹层/守卫自动放行
    } catch (e) {
      setError(e.message) // 后端的中文错误直接展示(含防枚举的统一文案)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      className="overlay"
      onClick={mandatory ? undefined : onClose}
      role="presentation"
    >
      <div
        className="dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={isLogin ? '登录' : '注册'}
      >
        <span className="dialog-title">{isLogin ? '登录' : '注册'}</span>

        <TextField
          label="用户名（3~32 个字符）"
          value={username}
          onChange={(v) => {
            setUsername(v)
            setError('')
          }}
          onEnter={handleSubmit}
          inputRef={firstFieldRef}
        />

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

        <p className="dialog-hint">
          {mandatory ? (
            <>
              登录后才能使用路线规划功能。
              <br />
              管理员请使用<a href="http://localhost:7801">管理台</a>（端口 7801）配置高德 key。
            </>
          ) : (
            <>
              {isLogin ? (
                <>
                  没有账号？{' '}
                  <a
                    href="#register"
                    onClick={(e) => {
                      e.preventDefault()
                      onSwitchMode?.('register')
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
                    onClick={(e) => {
                      e.preventDefault()
                      onSwitchMode?.('login')
                    }}
                  >
                    去登录
                  </a>
                </>
              )}
              <br />
              管理员请使用<a href="http://localhost:7801">管理台</a>（端口 7801）配置高德 key。
            </>
          )}
        </p>

        <div className="dialog-actions">
          <button className="btn btn--primary" onClick={handleSubmit} disabled={busy}>
            {busy ? '提交中…' : isLogin ? '登录' : '注册并登录'}
          </button>
          {!mandatory && (
            <button className="btn btn--ghost" onClick={onClose}>
              取消
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
