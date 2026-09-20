// App.jsx —— 组合根（composition root）
//
// App 的职责只有三件：
//   1) 持有"多个子树都要用"的状态（points、manual、高德 key）
//   2) 定义这些状态怎么变（用 hooks 提供的操作 + 业务规则）
//   3) 摆版面：顶栏 / 面板 / 地图 / 弹层 / 提示条
//
// 业务规则往哪放，这里有个判断标准：
//   - "点的增删排序" → usePoints（跟界面无关，纯数据）
//   - "规划这条异步链路" → usePlan（同上）
//   - "'至少 2 个点才能规划'、'点数超上限'" → 留在这里
//     因为它们是**把多个状态凑在一起才能得出的判断**（点数 + 手动模式），
//     塞进任何一个子 hook 都会让那个 hook 需要知道别的 hook 的事。
//
// 这就是"状态提升"的另一面：把状态提到够高，但别再往上提。

import { useCallback, useEffect, useMemo, useState } from 'react'

import { ControlPanel } from './components/ControlPanel.jsx'
import { LoginPage } from './components/LoginPage.jsx'
import { MapView } from './components/MapView.jsx'
import { TopBar } from './components/TopBar.jsx'
import { ToastStack } from './components/ui.jsx'

import { useAmap } from './hooks/useAmap.js'
import { useAuth } from './hooks/useAuth.js'
import { usePlan } from './hooks/usePlan.js'
import { usePoints } from './hooks/usePoints.js'
import { useSettings } from './hooks/useSettings.js'
import { useToasts } from './hooks/useToasts.js'

import { MAX_POINTS_BATCH, MAX_POINTS_PAIRWISE } from './theme.js'

export default function App() {
  // ── 各个 hook 管自己那一块 ──
  const { mapKey, jscode } = useSettings()
  const { points, addPoint, removePoint, movePoint, setLegMode } = usePoints()
  const plan = usePlan()
  const { toasts, push, dismiss } = useToasts()
  const auth = useAuth()
  const amap = useAmap(mapKey, jscode)

  // 默认自动模式：产品的核心卖点就是"多点自动排序"（后端 TSP），
  // 打开页面就该站在主线上。以前默认 manual=true（手动），叠加
  // "勾选 = 手动"的反向 checkbox 文案，结果用户从来没见过自动模式长什么样。
  // 默认值 = 最常用的那条路径，别让用户每次先做一遍配置才能到主线。
  const [manual, setManual] = useState(false)

  // ── 提示条 ──
  // useCallback 包一层，是为了让这个函数的**引用保持稳定**。
  // 它会被 SearchModule 通过 props 收下；如果每次渲染都生成新函数，
  // 下游把它当依赖的 effect 就会白跑一遍。
  // （这是 useCallback 真正该用的场景 —— 不是为了"性能优化"，是为了"稳定引用"。）
  const showError = useCallback((msg) => push(msg, 'error'), [push])

  // ── 地图上点一下 = 加一个点 ──
  const handleAddFromMap = useCallback(
    (coord) => {
      addPoint({ name: '', ...coord }) // 名字留空，由 usePoints 起个默认名
    },
    [addPoint]
  )

  // ── 搜索选中一个候选 = 加一个点 ──
  const handlePickPlace = useCallback(
    (place) => {
      addPoint({ name: place.name, lat: place.lat, lng: place.lng })
    },
    [addPoint]
  )

  // ── 手动输入坐标 ──
  const handleAddManual = useCallback((point) => addPoint(point), [addPoint])

  // ── 「开始规划」的前置检查 ──
  //
  // 这个 useMemo 算出"现在到底能不能规划"，以及不能的话是为什么。
  // 它依赖 [points.length, manual] —— 只有这两样变了才重新计算。
  // 提前算的意义：在按钮上就拦住无效提交，而不是等后端返回 400。
  //
  // ⚠️ 前端校验只是体验优化，不是安全边界。
  // 后端那份校验一个都不能少 —— 接口可以被直接调用。
  const { canPlan, blockReason, blockTone } = useMemo(() => {
    const n = points.length
    // n === 0 时不提示：此时列表区的空态文案已承担引导。
    if (n === 0) return { canPlan: false, blockReason: '', blockTone: '' }
    if (n < 2) return { canPlan: false, blockReason: '还需要一个目的地', blockTone: 'info' }

    // 上限分两档，取决于这次会走后端哪条路径：
    //   手动模式 → 逐段算 → 逐对接口 → 最多 10 个点
    //   自动模式 → 单一驾车 → 批量接口 → 最多 50 个点
    // 这个分档是后端定的（见 internal/api/router.go），前端照抄一份只为提示。
    const limit = manual ? MAX_POINTS_PAIRWISE : MAX_POINTS_BATCH
    if (n > limit) {
      return {
        canPlan: false,
        blockReason: `最多 ${limit} 个点（当前 ${n} 个）—— ${
          manual ? '逐段算距离是逐对请求，点数越多请求量增长很快' : '请减少地点'
        }`,
        blockTone: 'danger',
      }
    }
    return { canPlan: true, blockReason: '', blockTone: '' }
  }, [points.length, manual])

  const handlePlan = useCallback(() => {
    // 按钮在 !canPlan 时已经是 disabled，正常点不到这里。
    // 留这个判断是"兜底"：将来若有人把按钮换成别的触发方式（快捷键、表单提交），
    // 这行能挡住。防御性代码不用很多，但关键入口要有。
    if (!canPlan) return
    // 把当前数据快照交给 usePlan，后面所有事（提交、取路网、更新进度）它负责。
    plan.run({ points, manual })
  }, [canPlan, plan, points, manual])

  // ── 地点一变，之前的规划结果就过期了 ──
  //
  // 加一个点、删一个点、换个出行方式 —— 旧路线立刻就不对了。
  // 与其在每个操作里记得手写一句 plan.reset()（漏一个就出现"结果和地点对不上"），
  // 不如写成一条规则：**只要 points 变了，就作废旧结果。**
  //
  // 这是 effect 的正确用法之一：声明"一条不变量"，
  // 而不是手动在每个修改点重复维护它。
  useEffect(() => {
    plan.reset()
    // 依赖数组里只放 points：plan.reset 是稳定的（useCallback [] 包过），
    // 放进来只会让 lint 高兴，语义上没有增量。这里明确写注释说明。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [points])

  // ── 登录墙 ──
  // 未登录时渲染独立的整页登录页(不是弹窗):背后没有主界面的任何内容,
  // 也不存在半透明蒙层"透出"底下东西的问题。登录/注册成功后
  // LoginPage 内部 location.reload() 整页跳转进主界面(见 LoginPage 文件头:
  // reload 既是用户理解的"跳转",也是地图初始化时序的正确解)。
  // ready=false 是"还没问过 /auth/me"的加载态,必须等它,否则每次刷新
  // 都会闪一帧登录页再跳主界面。session 过期也走这里:/auth/me 401 → 回登录页。
  if (!auth.ready) {
    return (
      <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: 'var(--bg-page)' }}>
        <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>加载中…</span>
      </div>
    )
  }
  if (!auth.user) {
    return <LoginPage auth={auth} />
  }

  return (
    <div className="app">
      <TopBar
        user={auth.user}
        onLogout={async () => {
          await auth.logout()
          push('已退出登录', 'info')
        }}
      />
      <div className="hairline" />

      <div className="body">
        <ControlPanel
          points={points}
          manual={manual}
          planPhase={plan.phase}
          planProgress={plan.progress}
          planResult={plan.result}
          planError={plan.error}
          canPlan={canPlan}
          planBlockReason={blockReason}
          planBlockTone={blockTone}
          onPickPlace={handlePickPlace}
          onAddManual={handleAddManual}
          onManualChange={setManual}
          onRemove={removePoint}
          onMove={movePoint}
          onSetLegMode={setLegMode}
          onPlan={handlePlan}
          onError={showError}
        />

        <div className="divider-v" />

        <MapView
          amap={amap}
          points={points}
          segments={plan.segments}
          onAddFromMap={handleAddFromMap}
        />
      </div>

      {/* 提示条堆栈。渲染在最外层，用 position:fixed 浮在页面上方 ——
          这样它出现在哪里不受面板的 overflow 影响。 */}
      <ToastStack toasts={toasts} onDismiss={dismiss} />
    </div>
  )
}
