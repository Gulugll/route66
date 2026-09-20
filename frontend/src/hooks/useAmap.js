// useAmap.js —— 高德地图的生命周期
//
// 高德地图是命令式 API:new AMap.Map / map.add / map.remove,
// 接受的是"做什么";React 是声明式,描述"应该长什么样"。
// 两者共存的边界是 useEffect:
//   React 负责"什么时候做"(依赖变化即重跑),命令式库负责"怎么做"。
//
// 另一个原则:不是所有东西都该放进 state。
//   - 地图实例:创建后不变,变更也不需要触发渲染 → useRef
//   - 地图"就绪/失败"状态:界面据此渲染不同内容 → useState
// 地图实例放入 state 会引发无意义的连锁渲染。

import { useEffect, useRef, useState } from 'react'
import { DEFAULT_CENTER, DEFAULT_ZOOM } from '../theme.js'

// 脚本只加载一次。用模块级变量当缓存 —— 它对整个页面生命周期只存在一份，
// 多个组件同时调用也不会重复插入 <script>。
//
// 用 Promise 当缓存值，而不是用布尔量："正在加载中"也被缓存住了，
// 所以并发调用会等在同一个 Promise 上，不会各插一份脚本。
let scriptPromise = null
let loadedKey = null

function loadAmapScript(key, jscode) {
  // 换 key 必须重新加载脚本（key 是写在 script src 里的），所以 key 变了就重来。
  if (loadedKey === key && scriptPromise) return scriptPromise

  scriptPromise = new Promise((resolve, reject) => {
    // 开了「安全密钥」必须在这之前告诉高德，否则地图拒绝渲染（JS API 2.0 的机制）
    if (jscode) window._AMapSecurityConfig = { securityJsCode: jscode }

    const existing = document.getElementById('amap-script')
    if (existing) existing.remove() // 换 key 时把旧脚本删掉

    const script = document.createElement('script')
    script.id = 'amap-script'
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(key)}`
    script.onload = resolve
    script.onerror = () => reject(new Error('高德脚本加载失败'))
    document.head.appendChild(script)
  })

  loadedKey = key
  return scriptPromise
}

/**
 * @returns {{
 *   containerRef: React.RefObject,
 *   map: object|null,          // 地图实例，交给 MapView 去画内容
 *   status: 'no-key'|'loading'|'ready'|'failed',
 *   error: string,
 *   zoomIn: () => void, zoomOut: () => void,
 * }}
 */
export function useAmap(mapKey, jscode) {
  const containerRef = useRef(null)
  const [map, setMap] = useState(null)
  const [status, setStatus] = useState(mapKey ? 'loading' : 'no-key')
  const [error, setError] = useState('')

  useEffect(() => {
    // 每个 effect 都要能回答："什么时候重跑？跑之前要收拾什么？"
    // 这里的依赖是 [mapKey, jscode]：换 key 就重建地图。

    if (!mapKey) {
      setMap(null)
      setStatus('no-key')
      setError('')
      return
    }

    // cancelled 解决的是"异步竞态"：
    // 如果用户在脚本还没加载完时就换了 key（或组件被卸载），
    // 那个已经发出的加载请求回来时不该再去建地图。
    // 这个模式（在 cleanup 里标记"我不用了"）在 React 里非常常用。
    let cancelled = false

    setStatus('loading')
    setError('')

    loadAmapScript(mapKey, jscode)
      .then(() => {
        if (cancelled || !containerRef.current) return
        if (!window.AMap) throw new Error('高德脚本已加载但 AMap 未定义')

        // 这里才真正创建地图。容器必须是"已经在 DOM 里"的元素 ——
        // 所以用 ref 拿真实 DOM 节点，而不是用 id 字符串到处找。
        const instance = new window.AMap.Map(containerRef.current, {
          zoom: DEFAULT_ZOOM,
          center: DEFAULT_CENTER,
        })

        setMap(instance)
        setStatus('ready')
      })
      .catch((err) => {
        if (cancelled) return
        setError(err.message || '高德地图加载失败')
        setStatus('failed')
      })

    // cleanup 函数：组件卸载或依赖变化时执行。
    // 这里只负责"别让过期的异步结果生效"。真正的地图销毁在下面那个 effect 里。
    return () => {
      cancelled = true
    }
  }, [mapKey, jscode])

  // 销毁地图。
  //
  // ⚠️ 为什么单独写一个 effect，而不是把 map.destroy() 放进上面那个 cleanup？
  // 因为上面那个 cleanup 执行时**拿不到**刚创建出来的 instance
  // （闭包捕获的是 effect 运行那一刻的变量，那时 map 还是 null）。
  // 这个 effect 的依赖是 [map]，所以它的 cleanup 一定能在下一次
  // "map 要变成新值"或"组件卸载"之前拿到旧的 map —— 时机正好。
  useEffect(() => {
    if (!map) return
    return () => {
      // 地图持有 DOM、定时器、事件监听。不销毁 = 内存泄漏。
      // 开发时 React 会故意"挂载→卸载→再挂载"一遍（StrictMode），
      // 所以这个清理函数必须真的能工作，否则地图会闪、或者出现两张。
      map.destroy()
    }
  }, [map])

  const zoomIn = () => map && map.setZoom(map.getZoom() + 1)
  const zoomOut = () => map && map.setZoom(map.getZoom() - 1)

  return { containerRef, map, status, error, zoomIn, zoomOut }
}

/** 换个 key 要整页刷新 —— 高德脚本一旦注入就没法干净地卸载，重载最省事。 */
export function reloadForNewKey() {
  window.location.reload()
}
