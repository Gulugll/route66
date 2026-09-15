// useSettings.js —— 高德 key 的本地持久化
//
// 这个项目是开源的，key 由使用者自己配 —— 所以 key 只存浏览器本地
// （localStorage），不进代码、不进仓库。
//
// 这里有个 useState 的细节值得记：**"惰性初始化"**。
//
//   useState(localStorage.getItem('k'))        // ❌ 每次渲染都读一遍 localStorage
//   useState(() => localStorage.getItem('k'))  // ✅ 只在首次挂载时读一次
//
// 传函数进去，React 只会在组件第一次挂载时调用它。
// 像"读磁盘/读 localStorage/算一个很贵的初始值"这种，都该用这种写法。

import { useCallback, useState } from 'react'

const STORAGE_KEY = 'amap_key'
const STORAGE_JSCODE = 'amap_jscode'

export function useSettings() {
  const [mapKey, setMapKey] = useState(() => localStorage.getItem(STORAGE_KEY) || '')
  const [jscode, setJscode] = useState(() => localStorage.getItem(STORAGE_JSCODE) || '')

  /**
   * 保存。返回 true 表示确实写入了。
   * 注意：这里**不做** location.reload() —— 刷新是"副作用"，
   * 应该由调用方（App）决定，因为只有它知道整个应用还有哪些状态要一起重置。
   * hook 只管自己的数据，不擅自操作全局。这是"职责边界"。
   */
  const saveSettings = useCallback((key, code) => {
    const trimmed = key.trim()
    if (!trimmed) return false
    localStorage.setItem(STORAGE_KEY, trimmed)
    localStorage.setItem(STORAGE_JSCODE, code.trim())
    setMapKey(trimmed)
    setJscode(code.trim())
    return true
  }, [])

  return { mapKey, jscode, saveSettings }
}
