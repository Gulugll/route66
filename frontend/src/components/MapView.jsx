// MapView.jsx —— 地图区
//
// ⭐ 这个文件是全项目 useEffect 用得最重的地方，也是最能说明
// "React 和命令式库怎么共存"的例子。
//
// 核心思路：**把"数据"和"绘制"分成两个 effect。**
//
//   effect A：同步标记   —— 依赖 [map, points]
//   effect B：同步折线   —— 依赖 [map, segments]
//   effect C：绑定点击   —— 依赖 [map, onAddFromMap]
//
// 每个 effect 只干一件事，依赖数组写得明明白白。
// 手写版是一堆函数互相调用（addPoint → renderList + new Marker；
// removePoint → renderList + map.remove；onEnd → renderList + redrawMarkers + initSortable），
// 每加一个功能就得想"要记得同步哪几个地方"。
//
// ⚠️ 一个必须知道的代价：
// 高德的地图对象是**命令式**的 —— 它没有"给我一组新标记，你自己 diff"这种接口。
// 所以下面每次都是"全部删掉、全部重画"。对十几个点是完全可以接受的，
// 但如果以后要显示几千个点，就得自己写增量更新（比较新旧列表，只增删差集）。
// 记住：React 的 diff 只覆盖它自己渲染出来的 DOM，管不到第三方库内部的对象。

import { useEffect, useRef } from 'react'
import { MODE_COLORS, MODE_ICONS, MODE_LABELS, MODES } from '../theme.js'
import { Icon } from './icons.jsx'

export function MapView({ amap, points, segments, onAddFromMap }) {
  const { containerRef, map, status, error, zoomIn, zoomOut } = amap

  // 存"地图上的标记对象"。用 ref 而不是 state，因为改它不需要重渲染 ——
  // 它是我们和高德之间的账本，不是界面状态。
  // 账本里同时记了"这些标记属于哪张地图"：换地图（换 key）时不能拿着
  // 旧地图的标记去新地图上删 —— 那样删不掉，还会残留。
  const markersRef = useRef({ map: null, items: [] })
  const polylinesRef = useRef({ map: null, items: [] })

  // ── effect A：把 points 同步到地图标记 ──
  useEffect(() => {
    if (!map) return

    const book = markersRef.current
    if (book.map === map) {
      book.items.forEach((m) => map.remove(m))
    }

    const items = points.map(
      (p) => new window.AMap.Marker({ position: [p.lng, p.lat], title: p.name })
    )
    if (items.length > 0) map.add(items)

    markersRef.current = { map, items }
  }, [map, points])

  // ── effect B：把 segments 同步到地图折线 ──
  useEffect(() => {
    if (!map) return

    const book = polylinesRef.current
    if (book.map === map) {
      book.items.forEach((pl) => map.remove(pl))
    }

    const items = segments.map(
      (seg) =>
        new window.AMap.Polyline({
          path: seg.path,
          // 颜色来自 theme.js 的语义色表：颜色即语义，图例和线永远同色。
          // 0.75 的不透明度让底图还能透出来 —— 手写版这里是 0.7，
          // 用户反馈过"太不透明"，这里调到 0.75 配合更淡的底图正好。
          strokeColor: MODE_COLORS[seg.mode] || MODE_COLORS.driving,
          strokeWeight: 5,
          strokeOpacity: 0.75,
          lineJoin: 'round',
        })
    )
    if (items.length > 0) map.add(items)

    polylinesRef.current = { map, items }

    // 画完路线自动缩放到能装下整条路线。
    // ⚠️ setFitView 必须在地图尺寸稳定之后调用 —— 容器还在 0 宽时
    // 计算出来的视野是错的。所以这里等一帧再调。
    if (items.length > 0) {
      const timer = setTimeout(() => map.setFitView(), 0)
      return () => clearTimeout(timer)
    }
  }, [map, segments])

  // ── effect C：点地图加一个点 ──
  useEffect(() => {
    if (!map) return

    const handler = (e) => onAddFromMap({ lat: e.lnglat.lat, lng: e.lnglat.lng })
    map.on('click', handler)

    // 清理函数：把监听摘掉。
    // 不摘的话，每次 onAddFromMap 变化都会再挂一个监听 ——
    // 点一下地图会加上 2 个、3 个、5 个点，而且很难查。
    // （onAddFromMap 用 useCallback 包了一层，见 App.jsx，
    //   目的就是让这个函数的引用保持稳定，别让 effect 白跑。）
    return () => map.off('click', handler)
  }, [map, onAddFromMap])

  const ready = status === 'ready'

  return (
    <div className="map-area">
      {/* 地图容器。不管有没有配 key 都渲染出来 ——
          因为 React 需要一个"稳定的、始终存在的" DOM 节点挂地图。
          如果等 key 有了再渲染容器，useAmap 里的 containerRef.current 就是 null 了。 */}
      <div className="map-canvas" ref={containerRef} />

      {status === 'no-key' && (
        <div className="map-empty">
          <Icon name="pin" size={28} style={{ color: 'var(--border)' }} />
          <span className="map-empty-title">还没有配置高德 Key</span>
          {/* 登录墙改造后 key 不再由使用者手填:管理员在管理台(7801)配置,
              经 /config/public 下发到这里。这里的文案只说"去哪找谁",不说"自己填"——
              以前写"填入后即可查看真实路网"还把 JS key 和服务端 AMAP_KEY 混为一谈,
              踩过文案误导的坑,两种 key 的分工见 README「高德 key」一节。 */}
          <span className="map-empty-desc">
            地图渲染需要高德 JS key,由管理员在管理台统一配置
            <br />
            配置保存后刷新本页即可生效
          </span>
        </div>
      )}

      {status === 'loading' && (
        <div className="map-empty">
          <Icon name="spinner" size={24} style={{ color: 'var(--text-tertiary)' }} />
          <span className="map-empty-desc">正在加载高德地图…</span>
        </div>
      )}

      {status === 'failed' && (
        <div className="map-empty">
          <Icon name="alertCircle" size={26} style={{ color: 'var(--danger)' }} />
          <span className="map-empty-title">地图加载失败</span>
          <span className="map-empty-desc">
            {error}
            <br />
            key 无效或未把当前域名加入白名单。请确认页面功能仍可用（手动输入坐标、规划不受影响）。
          </span>
        </div>
      )}

      {/* 图例：只在真的画了路线时才显示。
          它和折线读的是同一张色表（theme.js 的 MODE_COLORS），
          所以不可能出现"图例写驾车是蓝色、线其实画成绿色"这种不一致。 */}
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
          <button className="zoom-btn" onClick={zoomIn} aria-label="放大">
            <Icon name="plus" size={14} />
          </button>
          <button className="zoom-btn" onClick={zoomOut} aria-label="缩小">
            <Icon name="minus" size={14} />
          </button>
        </div>
      )}

      {ready && <div className="attribution">地图数据 © 高德软件</div>}
    </div>
  )
}
