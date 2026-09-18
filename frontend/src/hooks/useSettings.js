// useSettings.js —— JS key 的来源:管理台统一下发
//
// 登录墙改造后（设置入口已下线），key 不再由使用者手填：
// 管理员在管理台（7801）配置 → app_settings 表 → /config/public 下发，这是唯一来源。
// 以前"localStorage 个人覆盖 > DB 下发"的两层优先级随之退役 ——
// 没有设置入口,个人覆盖永远不可能被写进去,留着这层逻辑就是死代码。
//
// 读的时机：mount 时异步拉 /config/public。拉取失败静默回落空态 ——
// 地图区域会显示"未配置"提示,不要让一条网络抖动把整个页面拖垮。
//
// useState 惰性初始化的细节保留（见 git 历史）：
//   useState(() => localStorage.getItem('k'))  // ✅ 只在首次挂载时读一次

import { useEffect, useState } from 'react'
import { fetchPublicConfig } from '../api.js'

export function useSettings() {
  const [mapKey, setMapKey] = useState('')
  const [jscode, setJscode] = useState('')

  useEffect(() => {
    fetchPublicConfig().then((cfg) => {
      if (!cfg) return
      if (cfg.js_key) {
        setMapKey(cfg.js_key)
        setJscode(cfg.js_sec || '')
      }
    })
    // 拉取失败不 setError:地图侧的 no-key/failed 空态已经把话说清了,
    // 这里再弹一条只会重复
  }, [])

  return { mapKey, jscode }
}
