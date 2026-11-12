// settings.js —— 用户地图 MCP 绑定的本地存取。
// key 只存 localStorage,永不上送后端(合规立场的实现落点)。

const STORE_KEY = 'routebot.mapMcp'

/**
 * @returns {{provider:string, key:string}|null} null = 未绑定(走默认底座)
 */
export function getBinding() {
  try {
    const raw = localStorage.getItem(STORE_KEY)
    if (!raw) return null
    const v = JSON.parse(raw)
    if (!v?.provider || !v?.key) return null
    return { provider: v.provider, key: v.key }
  } catch {
    return null
  }
}

export function saveBinding(provider, key) {
  localStorage.setItem(STORE_KEY, JSON.stringify({ provider, key }))
}

export function clearBinding() {
  localStorage.removeItem(STORE_KEY)
}
