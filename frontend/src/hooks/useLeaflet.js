// useLeaflet.js —— Leaflet 地图的生命周期
//
// 与被替换的 useAmap 同一个边界设计:命令式地图库经 useEffect 与 React 共存,
// 地图实例放 state(创建一次,不用于渲染细节),状态放 state(驱动界面)。
// 与高德的差异:OSM 瓦片不需要 key,地图"开箱即渲染",没有 no-key/failed 分支;
// 坐标系是 WGS-84,与后端(Valhalla)同系,全链路无坐标转换。
//
// ⚠️ 容器用 callback ref + state,而不是 useRef:
// 地图容器在登录成功后才由 MapView 挂载,App 顶层的 effect 先跑时容器还是 null;
// useRef 版本 effect 永不重跑(旧 useAmap 靠"key 异步到达触发重跑"掩盖了这个时序),
// callback ref 把"容器出现"本身变成一次 state 变化,effect 随之正确执行。

import { useCallback, useEffect, useState } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

import { DEFAULT_CENTER, DEFAULT_ZOOM } from '../theme.js'

// 瓦片源:默认 openstreetmap.de 镜像(OSM 官方数据、无 key、大陆网络可达)。
// 标准 OSM 瓦片服务器(tile.openstreetmap.org)与 CARTO(已要求 API key)
// 在大陆网络均不可用,故不作为默认。部署时可经 VITE_TILE_URL 换任意 XYZ 瓦片源。
const TILE_URL = import.meta.env.VITE_TILE_URL || 'https://tile.openstreetmap.de/{z}/{x}/{y}.png'
const TILE_ATTRIBUTION =
  '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> 贡献者'

/**
 * @returns {{
 *   containerRef: (el: HTMLElement|null) => void,  // callback ref,给 MapView 的容器
 *   map: object|null,          // Leaflet 地图实例,交给 MapView 画内容
 *   status: 'loading'|'ready',
 *   zoomIn: () => void, zoomOut: () => void,
 * }}
 */
export function useLeaflet() {
  const [container, setContainer] = useState(null)
  const [map, setMap] = useState(null)
  const [status, setStatus] = useState('loading')

  // callback ref:容器 DOM 出现/消失都会触发
  const containerRef = useCallback((el) => setContainer(el), [])

  useEffect(() => {
    if (!container) return

    // attributionControl 关掉:页面已有自绘的署名条(见 MapView),
    // 两份署名重叠没有意义 —— 但 OSM 的署名义务必须保留一处。
    // zoomControl 关掉:缩放用页面自绘控件(与旧版布局一致)。
    const instance = L.map(container, {
      zoomControl: false,
      attributionControl: false,
    }).setView([DEFAULT_CENTER[1], DEFAULT_CENTER[0]], DEFAULT_ZOOM)

    L.tileLayer(TILE_URL, {
      maxZoom: 19,
      attribution: TILE_ATTRIBUTION,
    }).addTo(instance)

    setMap(instance)
    setStatus('ready')

    return () => {
      // 地图持有 DOM 与事件监听,不销毁 = 内存泄漏(StrictMode 会挂载两次)
      instance.remove()
      setMap(null)
      setStatus('loading')
    }
  }, [container])

  const zoomIn = () => map && map.zoomIn()
  const zoomOut = () => map && map.zoomOut()

  return { containerRef, map, status, zoomIn, zoomOut }
}
