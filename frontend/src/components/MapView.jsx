// MapView.jsx —— 地图区(Leaflet + OSM 底图)
//
// React 与命令式地图库共存的样板:把"数据"和"绘制"分成多个 effect。
// 标记与折线同属一个 LayerGroup,全删全画 —— 十几个点的量级下可接受,
// 点数达到千级时再改为增量更新。
//
// 坐标约定:后端/plan 与 /route 返回的路径点是 [lng, lat](经度在前),
// Leaflet 的 API 一律 [lat, lng] —— 在本组件统一转换,别处不出现第二份转换代码。

import { useEffect, useRef } from 'react'
import L from 'leaflet'

import { MODE_COLORS, MODE_LABELS, MODES } from '../theme.js'
import { Icon } from './icons.jsx'

// 编号标记:圆形底 + 序号。不用默认图钉( bundler 下图标 URL 会 404 ),
// divIcon 的 HTML 由我们完全掌控,编号与列表一致。
function markerIcon(index) {
  return L.divIcon({
    className: 'pt-marker',
    html: `<span>${index + 1}</span>`,
    iconSize: [24, 24],
    iconAnchor: [12, 12],
  })
}

export function MapView({ mapApi, points, segments, onAddFromMap, fab }) {
  const { containerRef, map, status } = mapApi
  const layerRef = useRef(null)

  // ── effect A:points + segments 同步到地图(全删全画)──
  useEffect(() => {
    if (!map) return

    if (layerRef.current) layerRef.current.remove()
    const group = L.layerGroup().addTo(map)
    layerRef.current = group

    points.forEach((p, i) => {
      L.marker([p.lat, p.lng], { icon: markerIcon(i), title: p.name })
        .bindTooltip(p.name || `点 ${i + 1}`, { direction: 'top' })
        .addTo(group)
    })

    segments.forEach((seg) => {
      // 后端路径是 [lng, lat],Leaflet 要 [lat, lng]
      const line = seg.path.map(([lng, lat]) => [lat, lng])
      if (line.length < 2) return
      L.polyline(line, {
        // 颜色来自 theme.js 的语义色表:图例和折线永远同色。
        color: MODE_COLORS[seg.mode] || MODE_COLORS.driving,
        weight: 5,
        opacity: 0.75,
        lineJoin: 'round',
      }).addTo(group)
    })

    // 画完路线自动缩放到能装下整条路线;只有点没有路线时对齐所有点。
    const bounds = L.latLngBounds([
      ...points.map((p) => [p.lat, p.lng]),
      ...segments.flatMap((seg) => seg.path.map(([lng, lat]) => [lat, lng])),
    ])
    if (bounds.isValid()) {
      map.fitBounds(bounds, { padding: [40, 40], maxZoom: 15 })
    }
  }, [map, points, segments])

  // ── effect B:点地图加一个点 ──
  useEffect(() => {
    if (!map) return

    const handler = (e) => onAddFromMap({ lat: e.latlng.lat, lng: e.latlng.lng })
    map.on('click', handler)

    // 清理函数必须摘掉监听:否则 onAddFromMap 每次变化都会叠加一个监听,
    // 一次点击会加入多个点。
    return () => map.off('click', handler)
  }, [map, onAddFromMap])

  const ready = status === 'ready'

  return (
    <div className="map-area">
      {/* 地图容器始终渲染:React 需要一个稳定存在的 DOM 节点挂地图 */}
      <div className="map-canvas" ref={containerRef} />
      {fab}

      {/* 图例:只在真的画了路线时才显示。
          它和折线读的是同一张色表(theme.js 的 MODE_COLORS),
          所以不可能出现"图例写驾车是蓝色、线其实画成绿色"的不一致。 */}
      {ready && segments.length > 0 && (
        <div className="floating legend">
          {MODES.map((m) => (
            <div key={m} className="legend-row">
              <span className="legend-bar" style={{ background: MODE_COLORS[m] }} />
              <span>{MODE_LABELS[m]}</span>
            </div>
          ))}
        </div>
      )}

      {ready && (
        <div className="floating zoom-control">
          <button className="zoom-btn" onClick={mapApi.zoomIn} aria-label="放大">
            <Icon name="plus" size={14} />
          </button>
          <button className="zoom-btn" onClick={mapApi.zoomOut} aria-label="缩小">
            <Icon name="minus" size={14} />
          </button>
        </div>
      )}

      {ready && <div className="attribution">地图数据 © OpenStreetMap 贡献者</div>}
    </div>
  )
}
