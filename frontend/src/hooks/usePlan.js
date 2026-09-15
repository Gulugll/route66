// usePlan.js —— 「开始规划」这条异步链路的完整状态
//
// 手写版里这条链路是：
//     plan()  →  fetch('/plan')  →  写 result.innerHTML  →  drawRoute()
//     drawRoute() 里再 for 循环 await fetch('/route')，边拿边画
//
// 问题在最后一环：**边拿边画**意味着界面上没有任何进度可言 ——
// 10 个点要走 30 秒，用户只能盯着一个没反应的按钮。
//
// 这里把它显式地拆成"阶段"(phase)，界面就能如实反映当前在干什么：
//
//     idle ──▶ submitting ──▶ drawing ──▶ done
//              (算距离+排序)   (逐段取路网)
//
// 教学点：把"一个长时间的异步过程"建模成一组明确的阶段，
// 是前端做加载反馈的标准手法。阶段本身也是 state，也由 React 驱动界面。

import { useCallback, useRef, useState } from 'react'
import { fetchRoute, planRoute } from '../api.js'

export const PHASE = {
  IDLE: 'idle',
  SUBMITTING: 'submitting',
  DRAWING: 'drawing',
  DONE: 'done',
}

export function usePlan() {
  const [phase, setPhase] = useState(PHASE.IDLE)
  const [result, setResult] = useState(null)
  const [segments, setSegments] = useState([])
  const [progress, setProgress] = useState({ done: 0, total: 0 })
  const [error, setError] = useState('')

  // ── 竞态防护 ──
  // 用户可能在第一次规划还没跑完时又点了一次。两次请求都会返回，
  // 后回来的那个会把先回来的结果覆盖掉 —— 界面就会出现"结果对不上"。
  //
  // 解决办法：给每次运行发一个递增的编号，回来时先核对"我还是最新那次吗"。
  // 这个 ref 用 useRef 而不是 state，因为它**不该触发重新渲染** ——
  // 它只是个"记个号"的盒子。
  const runIdRef = useRef(0)

  const reset = useCallback(() => {
    runIdRef.current += 1 // 让正在跑的那次作废
    setPhase(PHASE.IDLE)
    setResult(null)
    setSegments([])
    setProgress({ done: 0, total: 0 })
    setError('')
  }, [])

  /**
   * 跑一次完整规划。
   *
   * @param {{points: Array, manual: boolean}} args
   */
  const run = useCallback(async ({ points, manual }) => {
    const runId = ++runIdRef.current
    const isStale = () => runIdRef.current !== runId

    setError('')
    setResult(null)
    setSegments([])
    setProgress({ done: 0, total: 0 })
    setPhase(PHASE.SUBMITTING)

    let data
    try {
      data = await planRoute({ points, manual })
    } catch (err) {
      if (isStale()) return
      setError(err.message)
      setPhase(PHASE.IDLE)
      return
    }
    if (isStale()) return

    setResult(data)

    // ── 逐段取真实路网轨迹 ──
    // order_idx 是**下标**数组（指向传入的 points），不是名字数组。
    // 名字只是给人看的标签 —— 搜两次"故宫"就有两个同名点，
    // 拿名字回查坐标永远只查到第一个，线就画错了。这是后端专门
    // 多返回一个 order_idx 的原因。
    const order = data.order_idx
    const legCount = Math.max(order.length - 1, 0)
    setProgress({ done: 0, total: legCount })
    setPhase(PHASE.DRAWING)

    const drawn = []
    for (let i = 0; i < legCount; i += 1) {
      const a = points[order[i]]
      const b = points[order[i + 1]]
      if (!a || !b) continue // 下标越界的脏数据就跳过这一段，别让整条线断掉

      // 手动模式：每段用各自选的方式。自动模式（TSP 重排过顺序）：
      // points[i].legMode 是按**列表顺序**存的，而 order 已经被算法打乱，
      // 两者对不上号，所以自动模式统一按驾车画。
      const mode = manual ? a.legMode || 'driving' : 'driving'

      let path = []
      try {
        path = await fetchRoute(a, b, mode)
      } catch {
        // 拿不到轨迹（公交没有轨迹 / 网络失败）：降级画直线。
        // 和后端"高德失败降级 haversine"是同一个思想：
        // 局部失败不该让整个功能不可用。
      }
      if (isStale()) return

      // 高德的轨迹是「一串点」，但即使成功也可能只给 1 个点 —— 那画不出线。
      if (!path || path.length < 2) {
        path = [
          [a.lng, a.lat],
          [b.lng, b.lat],
        ]
      }

      drawn.push({ from: a.id, to: b.id, mode, path })
      // 每画完一段就更新进度 —— 界面上的"第 N 段"就是这么来的。
      // 注意这里用 setSegments(prev => ...) 的**函数式更新**：
      // 循环里连续多次更新，如果不基于 prev 而是基于闭包里的旧值，
      // 后面的会覆盖前面的，最后只剩一段。
      setSegments((prev) => [...prev, { from: a.id, to: b.id, mode, path }])
      setProgress({ done: i + 1, total: legCount })
    }

    if (isStale()) return
    setSegments(drawn)
    setPhase(PHASE.DONE)
  }, [])

  return { phase, result, segments, progress, error, run, reset }
}
