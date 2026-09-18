// useAuth.js —— 登录态的单一数据源
//
// 页面刷新后登录态怎么恢复？靠 cookie：浏览器对每个请求自动带上，
// 挂载时 GET /auth/me 问一嗓子"我是谁"，后端认得出就返回用户，
// 认不出就是游客。前端**从不**保管 token —— HttpOnly cookie 的价值
// 就在于 JS 摸不到它，这里存了 token 反而把这道防线拆了。
//
// 教学点：登录/注册之后**不需要刷新页面**。
// user state 一变，依赖它的组件（TopBar 的角标）自动重渲染 ——
// 这就是"状态驱动界面"和"操作完 location.reload()"的差距。

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
