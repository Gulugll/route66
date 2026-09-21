// usePlan.js —— 「开始规划」这条异步链路的完整状态
//
// 链路被显式拆成"阶段"(phase),界面可以如实反映当前在干什么:
//
//     idle ──▶ submitting ──▶ drawing ──▶ done
//              (算距离+排序)   (逐段取路网)
//
// 把长时间的异步过程建模成一组明确的阶段,是前端做加载反馈的标准手法。
// 阶段本身也是 state,由 React 驱动界面。
//
// 两条进入 drawing 的路径:
//   run()            用户在规划带点「开始规划」(先提交 /plan 再画)
//   applyAgentPlan() RouteBot 用 plan_route 工具算出方案后同步过来
//                    (结果已在后端算好,跳过提交,只补画逐段轨迹)

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

  // ── 共享的逐段画线流程 ──
  // 逐段取真实路网轨迹:拿不到(公交没轨迹/网络失败)就降级画直线,
  // 和后端"高德失败降级 haversine"是同一个思想:局部失败不让整体不可用。
  // modes[i] = 第 i 段的出行方式。
  const drawLegs = useCallback(async (ordered, modes, isStale) => {
    const legCount = Math.max(ordered.length - 1, 0)
    setProgress({ done: 0, total: legCount })
    setPhase(PHASE.DRAWING)

    const segs = []
    for (let i = 0; i < legCount; i += 1) {
      const a = ordered[i]
      const b = ordered[i + 1]
      if (!a || !b) continue

      let path = []
      try {
        path = await fetchRoute(a, b, modes[i])
      } catch {
        // 局部失败不该让整个功能不可用
      }
      if (isStale()) return false

      // 即使成功也可能只给 1 个点 —— 那画不出线,退化为直线
      if (!path || path.length < 2) {
        path = [
          [a.lng, a.lat],
          [b.lng, b.lat],
        ]
      }

      const seg = { from: a.id, to: b.id, mode: modes[i], path }
      segs.push(seg)
      // 函数式更新:循环里连续多次更新必须基于 prev,否则后面的覆盖前面的
      setSegments((prev) => [...prev, seg])
      setProgress({ done: i + 1, total: legCount })
    }
    setSegments(segs)
    setPhase(PHASE.DONE)
    return true
  }, [])

  /**
   * 跑一次完整规划。
   *
   * @param {{points: Array, manual: boolean, mode?: string}} args
   *   mode 是自动模式的全局出行方式(默认 driving);手动模式按 points 上的 legMode 逐段算。
   */
  const run = useCallback(
    async ({ points, manual, mode = 'driving' }) => {
      const runId = ++runIdRef.current
      const isStale = () => runIdRef.current !== runId

      setError('')
      setResult(null)
      setSegments([])
      setProgress({ done: 0, total: 0 })
      setPhase(PHASE.SUBMITTING)

      let data
      try {
        data = await planRoute({ points, manual, mode })
      } catch (err) {
        if (isStale()) return
        setError(err.message)
        setPhase(PHASE.IDLE)
        return
      }
      if (isStale()) return

      setResult(data)

      // order_idx 是**下标**数组（指向传入的 points），不是名字数组。
      // 名字只是给人看的标签 —— 搜两次"故宫"就有两个同名点，
      // 拿名字回查坐标永远只查到第一个，线就画错了。这是后端专门
      // 多返回一个 order_idx 的原因。
      const ordered = data.order_idx.map((i) => points[i]).filter(Boolean)
      // 手动模式:每段用各自选的方式(legMode 按列表顺序存,
      // 手动时列表顺序即结果顺序);自动模式:统一用全局方式
      const modes = manual
        ? ordered.map((p) => p.legMode || 'driving')
        : ordered.map(() => mode)

      await drawLegs(ordered, modes, isStale)
    },
    [drawLegs]
  )

  /**
   * RouteBot 的方案同步:plan_route 的结果已经在后端算好,
   * 跳过 /plan 提交,直接采用结果并补画逐段轨迹。
   * ordered 是按访问顺序排好的地点数组,result 是 planner.Result。
   */
  const applyAgentPlan = useCallback(
    async ({ ordered, result, mode = 'driving' }) => {
      const runId = ++runIdRef.current
      const isStale = () => runIdRef.current !== runId

      setError('')
      setResult(result)
      setSegments([])
      await drawLegs(ordered, ordered.map(() => mode), isStale)
    },
    [drawLegs]
  )

  return { phase, result, segments, progress, error, run, reset, applyAgentPlan }
}
