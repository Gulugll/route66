// PostMap.jsx —— 笔记详情的快照小地图。
//
// 画的是 plan_snapshot 里的点位与**分段直线**,不是真实路网:
// 真实路网要逐段调 /route(打距离来源,可能没配 key),且快照里本来就只有点位。
// 详情页的地图语义是"这条路线长什么样",直线段 + 分段配色足够传达;
// 用户点「按此路线开始规划」进规划器后再看真实路网。
//
// 复用 useLeaflet(裸地图实例 + callback ref),与主地图同一初始化路径;
// 折线颜色读 theme.js 的 MODE_COLORS(与规划页同色,视觉语言一致)。

import { useEffect } from 'react'
import L from 'leaflet'

import { useLeaflet } from '../hooks/useLeaflet.js'
import { MODE_COLORS } from '../theme.js'

export function PostMap({ snapshot }) {
  const { containerRef, map, status } = useLeaflet()

  useEffect(() => {
    if (status !== 'ready' || !map || !snapshot?.points?.length) return

    const pts = snapshot.points
    const layer = L.layerGroup()

    // 点标记:序号圆点 + 常驻地名标签(tooltip 常开,小地图上没有悬停语境)
    pts.forEach((p, i) => {
      L.circleMarker([p.lat, p.lng], {
        radius: 6,
        color: '#fff',
        weight: 2,
        fillColor: '#1D1D1F',
        fillOpacity: 1,
      })
        .bindTooltip(`${i + 1}. ${p.name || `地点${i + 1}`}`, { permanent: true, direction: 'right' })
        .addTo(layer)
    })

    // 分段直线:段的颜色 = "从这点出发的方式"(legMode 挂在点上,与规划页同语义)
    for (let i = 0; i < pts.length - 1; i++) {
      const a = pts[i]
      const b = pts[i + 1]
      L.polyline(
        [
          [a.lat, a.lng],
          [b.lat, b.lng],
        ],
        {
          color: MODE_COLORS[a.legMode] || MODE_COLORS.driving,
          weight: 3,
          opacity: 0.85,
        }
      ).addTo(layer)
    }

    layer.addTo(map)
    // fitBounds 把视野收进路线范围 —— 小地图只有这一条路线,没理由看全国
    map.fitBounds(L.latLngBounds(pts.map((p) => [p.lat, p.lng])).pad(0.25))

    return () => {
      layer.remove() // snapshot 变化/卸载时清图层,防叠加
    }
  }, [map, status, snapshot])

  return <div ref={containerRef} className="post-map" />
}
