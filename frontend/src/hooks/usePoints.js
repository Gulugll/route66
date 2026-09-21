// usePoints.js —— 地点列表的状态与操作
//
// 数据放在 useState 里,界面是它的函数。
//
// ═══ 不可变更新 ═══
//
// 直接 mutate 数组(points.push(...))不会触发重新渲染:
// React 用 Object.is 比较前后值,push 不改变数组引用,React 认为"没有变化"。
// 因此所有更新都必须返回新数组:
//     setPoints(prev => [...prev, newPoint])
// 这也是 React 声明式模型的基础操作。

import { useCallback, useState } from 'react'

// 生成稳定的 id。
//
// ⚠️ 不用数组下标当 key:下标不是元素"身份"。删除首项后所有下标前移,
// React 会视为"每个位置都换了内容",导致 DOM 重建(输入框失焦、动画重放、
// 拖拽状态丢失)。id 跟随数据,React 据此识别"同一元素只是位置变化"。
//
// crypto.randomUUID 只在安全上下文(https / localhost)可用 ——
// 部署到 http 时需要兜底,这里用计数器。
let idSeq = 0
function nextId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
  idSeq += 1
  return `p${idSeq}`
}

export function usePoints() {
  const [points, setPoints] = useState([])

  /** 追加一个地点。新地点默认"到下一站走驾车"。 */
  const addPoint = useCallback((place) => {
    setPoints((prev) => [
      ...prev,
      {
        id: nextId(),
        // 名字留空时用默认名。这里用 setPoints 的函数式更新:
        // prev 始终是最新值。若在外层用 points.length,连续快速添加时
        // 两次更新会读到同一个旧长度,产生重名。
        name: place.name || `地点${prev.length + 1}`,
        lat: place.lat,
        lng: place.lng,
        // legMode 挂在"点"上,表示"从这个点到下一个点怎么走"。
        // 每个点自带出发方式,无需维护长度必须等于 n-1 的平行数组,
        // 删点时也不存在两份数据同步的问题。
        legMode: 'driving',
      },
    ])
  }, [])

  /** 按 id 删除。用 filter 造新数组，不碰原数组。 */
  const removePoint = useCallback((id) => {
    setPoints((prev) => prev.filter((p) => p.id !== id))
  }, [])

  /**
   * 拖拽排序:把 from 位置的元素挪到 to 位置。
   * React 里只有一步:把新顺序写成新数组,
   * 界面、地图标记、路线全部随这个数组自动更新。
   */
  const movePoint = useCallback((from, to) => {
    if (from === to) return
    setPoints((prev) => {
      if (from < 0 || to < 0 || from >= prev.length || to >= prev.length) return prev
      const next = prev.slice()
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)

      // 段方式跟着点走会变得没有意义：段连接的是"相邻两点"，
      // 顺序一变，原来的第 1 段和第 3 段可能是完全不同的两段路。
      // 所以重排后统一重置成驾车，让用户按新顺序重新选。
      // (保留既有的产品决策:重排后段方式重置为驾车,只是一次 map。)
      return next.map((p) => ({ ...p, legMode: 'driving' }))
    })
  }, [])

  /** 改某一段的出行方式。 */
  const setLegMode = useCallback((id, mode) => {
    setPoints((prev) => prev.map((p) => (p.id === id ? { ...p, legMode: mode } : p)))
  }, [])

  /**
   * 整表替换(RouteBot 方案同步用):按传入顺序重建地点列表。
   * 与其逐个 addPoint(连续 setPoints 的函数式更新虽然正确,但语义是"追加"),
   * 不如一个动作表达"这是新的方案"。
   */
  const replacePoints = useCallback((list) => {
    setPoints(
      (list || []).map((p, i) => ({
        id: nextId(),
        name: p.name || `地点${i + 1}`,
        lat: p.lat,
        lng: p.lng,
        legMode: p.legMode || 'driving',
      }))
    )
  }, [])

  const clearPoints = useCallback(() => setPoints([]), [])

  return { points, addPoint, removePoint, movePoint, setLegMode, replacePoints, clearPoints }
}
