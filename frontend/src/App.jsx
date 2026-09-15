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
import { MapView } from './components/MapView.jsx'
import { SettingsDialog } from './components/SettingsDialog.jsx'
import { TopBar } from './components/TopBar.jsx'
import { ToastStack } from './components/ui.jsx'

import { useAmap, reloadForNewKey } from './hooks/useAmap.js'
import { usePlan } from './hooks/usePlan.js'
import { usePoints } from './hooks/usePoints.js'
import { useSettings } from './hooks/useSettings.js'
import { useToasts } from './hooks/useToasts.js'

import { MAX_POINTS_BATCH, MAX_POINTS_PAIRWISE } from './theme.js'

export default function App() {
  // ── 各个 hook 管自己那一块 ──
  const { mapKey, jscode, saveSettings } = useSettings()
  const { points, addPoint, removePoint, movePoint, setLegMode } = usePoints()
  const plan = usePlan()
  const { toasts, push, dismiss } = useToasts()
  const amap = useAmap(mapKey, jscode)

  // 默认自动模式：产品的核心卖点就是"多点自动排序"（后端 TSP），
  // 打开页面就该站在主线上。以前默认 manual=true（手动），叠加
  // "勾选 = 手动"的反向 checkbox 文案，结果用户从来没见过自动模式长什么样。
  // 默认值 = 最常用的那条路径，别让用户每次先做一遍配置才能到主线。
  const [manual, setManual] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)

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
  //
  // 为什么要提前算：手写版是点了按钮才发请求，然后等后端返回 400 再 alert。
  // 白跑一个来回，用户还得自己看懂错误。这里在按钮上就直接拦住。
  //
  // ⚠️ 但一定要记住：**前端校验只是体验优化，不是安全边界。**
  // 后端那份校验一个都不能少 —— 别人可以用 curl 直接打你的接口。
  const { canPlan, blockReason, blockTone } = useMemo(() => {
    const n = points.length
    // 一个点都没有时什么都不说：这时用户刚打开页面，
    // 报一句"至少需要一个起点"像是他在开头就做错了。地点列表那块
    // 已经写了"还没有地点，搜索添加…"，引导足够了。
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

  // ── 保存 key 后整页刷新 ──
  const handleSaveSettings = useCallback(
    (key, code) => {
      if (!saveSettings(key, code)) {
        showError('key 不能为空')
        return
      }
      setSettingsOpen(false)
      // 高德脚本是把 key 写在 <script src> 里的，换 key 只能重新加载脚本。
      // 整页刷新比"卸载旧脚本再注入新脚本"省事得多，而且能保证
      // 地图实例、覆盖物、事件监听全部干净重来。
      reloadForNewKey()
    },
    [saveSettings, showError]
  )

  return (
    <div className="app">
      <TopBar onOpenSettings={() => setSettingsOpen(true)} />
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
          onOpenSettings={() => setSettingsOpen(true)}
        />
      </div>

      {/* 提示条堆栈。渲染在最外层，用 position:fixed 浮在页面上方 ——
          这样它出现在哪里不受面板的 overflow 影响。 */}
      <ToastStack toasts={toasts} onDismiss={dismiss} />

      {/* 条件渲染弹层：打开 = 挂载，关闭 = 卸载（见 SettingsDialog 的注释） */}
      {settingsOpen && (
        <SettingsDialog
          initialKey={mapKey}
          initialJscode={jscode}
          onSave={handleSaveSettings}
          onClose={() => setSettingsOpen(false)}
        />
      )}
    </div>
  )
}
