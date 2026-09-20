// useAuth.js —— 登录态的单一数据源
//
// 登录态恢复:依赖 cookie(浏览器自动携带),挂载时 GET /auth/me 查询当前用户,
// 认不出即为游客。前端不保管 token——HttpOnly cookie 的价值在于 JS 不可读,
// 在前端存 token 反而破坏这道防线。
//
// 登录/注册后不需要刷新页面:user state 变化,依赖它的组件自动重渲染。

import { useCallback, useEffect, useState } from 'react'
import { fetchMe, loginUser, logoutUser, registerUser } from '../api.js'

export function useAuth() {
  // ready = "已经问过 /auth/me 了"。没就绪前 TopBar 不渲染登录按钮，
  // 否则刷新页面会闪一下"登录"再变成用户名（布局抖动）
  const [user, setUser] = useState(null)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    fetchMe()
      .then(setUser)
      .catch(() => setUser(null)) // 401 = 游客,正常路径,不是错误
      .finally(() => setReady(true))
  }, [])

  // ── 全局过期兜底 ──
  // API 保护启用后,业务接口在 session 过期时会返回 401;api.js 的 request()
  // 对每个 401 派发 auth:expired 事件,这里统一接住 —— user 置空,
  // App 的登录墙三分支自动接管。业务组件(搜索/规划/画线)完全不用知道这件事。
  // 细节:登录页上的 /auth/me 401 也会触发一次,但那时 user 已经是 null,
  // setUser(null) 幂等,不会造成额外的渲染抖动。
  useEffect(() => {
    const onExpired = () => setUser(null)
    window.addEventListener('auth:expired', onExpired)
    return () => window.removeEventListener('auth:expired', onExpired)
  }, [])

  const login = useCallback(async (username, password) => {
    const data = await loginUser(username, password)
    setUser(data.user)
  }, [])

  const register = useCallback(async (username, password) => {
    // 后端注册成功即登录(发会话 cookie),这里直接当登录结果用
    const data = await registerUser(username, password)
    setUser(data.user)
  }, [])

  const logout = useCallback(async () => {
    await logoutUser()
    setUser(null)
  }, [])

  return { user, ready, login, register, logout }
}
