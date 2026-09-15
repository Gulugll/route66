// usePoints.js —— 地点列表的状态与操作
//
// 这个文件回答一个问题："如果不用手写 DOM，那'数据'到底放在哪？"
// 答案是：放在一个 useState 里，界面是它的**函数**。
//
// ═══ React 里最容易踩的坑：不能直接改数组 ═══
//
// 手写版是这样改数据的：
//     state.points.push({...})      // 改完就行了
//     renderList()                   // 然后手动喊一句"去重画界面"
//
// React 里**不能这么干**。因为 React 判断"要不要重新渲染"的方法是
// 比较前后两个值 —— 用的是 Object.is（≈ ===）。而 push 是在**同一个数组**
// 上加元素，引用没变，React 认为"什么都没变"，界面永远不更新。
//
// 所以必须返回一个**新数组**：
//     setPoints(prev => [...prev, newPoint])
//
// 记住这条规矩：**永远不修改（mutate）state，永远造一个新的。**
// 这条规矩是 React 的心智模型的核心，也是从"命令式改 DOM"转向
// "声明式描述状态"的关键一步。

import { useCallback, useState } from 'react'

// 生成稳定的 id。
//
// ⚠️ 为什么要 id，而不是直接用数组下标当 key？
// 因为下标不是"身份"。删掉第 1 项之后，原来的第 2 项就变成了第 1 项 ——
// 下标变了，React 会以为"第 1 项换了个东西"，于是把 DOM 和内部状态
// 都重新来过（输入框失焦、动画重放、拖拽失效）。
// 手写版的 Sortable 引用失效问题，根子就在这里。
//
// id 跟着数据走：删谁排谁，id 都不变，React 就总能认出"还是同一个东西，只是位置变了"。
//
// crypto.randomUUID 只在安全上下文（https / localhost）可用 ——
// 这个项目本地跑是 localhost，没问题；但为了部署到 http 时不炸，
// 留一个计数器兜底。
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
        // 名字留空就起个默认名。注意这里用的是 setPoints 的**函数式更新**：
        // prev 一定是最新值，所以 prev.length 一定是准的。
        // 如果写在外层用 points.length，一旦短时间内连加两个点，
        // 两次都会读到同一个旧长度，两个点会重名。
        name: place.name || `地点${prev.length + 1}`,
        lat: place.lat,
        lng: place.lng,
        // legMode 挂在"点"上，表示"从这个点到下一个点怎么走"。
        // 这样数据是自洽的：每个点自带"出发方式"，不需要再维护一个
        // 长度必须等于 n-1 的平行数组（手写版就是 points 和 legs 两个数组，
        // 删点时要记得同时 splice 两个 —— 忘一个就错位）。
        legMode: 'driving',
      },
    ])
  }, [])

  /** 按 id 删除。用 filter 造新数组，不碰原数组。 */
  const removePoint = useCallback((id) => {
    setPoints((prev) => prev.filter((p) => p.id !== id))
  }, [])

  /**
   * 拖拽排序：把 from 位置的元素挪到 to 位置。
   *
   * 手写版这里是 SortableJS 的 onEnd 回调，操作完还得手动 renderList() +
   * redrawMarkers() + initSortable()（因为重建 DOM 把 Sortable 的引用弄失效了）。
   *
   * React 里只有一步：把新顺序写成新数组。
   * 界面、地图标记、路线全部会跟着这个数组自动更新 —— 没有"忘了调哪个 render"这回事。
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
      // （保留手写版的这个产品决策，只是现在它只是一次 map。）
      return next.map((p) => ({ ...p, legMode: 'driving' }))
    })
  }, [])

  /** 改某一段的出行方式。 */
  const setLegMode = useCallback((id, mode) => {
    setPoints((prev) => prev.map((p) => (p.id === id ? { ...p, legMode: mode } : p)))
  }, [])

  const clearPoints = useCallback(() => setPoints([]), [])

  return { points, addPoint, removePoint, movePoint, setLegMode, clearPoints }
}
