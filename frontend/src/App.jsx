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

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { AgentPane } from './components/AgentPane.jsx'
import { CommunityView } from './components/CommunityView.jsx'
import { LoginPage } from './components/LoginPage.jsx'
import { MapView } from './components/MapView.jsx'
import { PlanStrip } from './components/PlanStrip.jsx'
import { ProfileDialog } from './components/ProfileDialog.jsx'
import { ShareDialog } from './components/ShareDialog.jsx'
import { TopBar } from './components/TopBar.jsx'
import { ToastStack } from './components/ui.jsx'

import { useLeaflet } from './hooks/useLeaflet.js'
import { useAuth } from './hooks/useAuth.js'
import { usePlan } from './hooks/usePlan.js'
import { usePoints } from './hooks/usePoints.js'
import { useToasts } from './hooks/useToasts.js'

import { MAX_POINTS_BATCH, MAX_POINTS_PAIRWISE } from './theme.js'

export default function App() {
  // ── 各个 hook 管自己那一块 ──
  const { points, addPoint, removePoint, movePoint, setLegMode, replacePoints, clearPoints } =
    usePoints()
  const plan = usePlan()
  const { toasts, push, dismiss } = useToasts()
  const auth = useAuth()
  const mapApi = useLeaflet()

  const [manual, setManual] = useState(false)
  // 自动模式的全局出行方式(交给后端 mode 字段);手动模式下即时应用到全部路段
  const [mode, setMode] = useState('driving')
  // Agent 面板开合:面板上的"收起"与地图上的浮动入口共享这一个状态
  const [agentOpen, setAgentOpen] = useState(true)
  // 账号资料弹层(M1 社区身份层):TopBar 的用户名按钮打开
  const [profileOpen, setProfileOpen] = useState(false)
  // 顶层视图:planner(规划工作台) | community(社区)。无路由库,
  // 与登录墙同一套"状态决定渲染哪棵子树"的模式 —— 两个视图就别引 router 了。
  const [view, setView] = useState('planner')
  const [shareOpen, setShareOpen] = useState(false)

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

  // ── 全局出行方式:自动模式给后端 mode;手动模式即时套用到全部路段 ──
  const handleModeChange = useCallback(
    (m) => {
      setMode(m)
      if (manual) points.forEach((p) => setLegMode(p.id, m))
    },
    [manual, points, setLegMode]
  )

  // ── RouteBot 方案联动 ──
  // RouteBot 调 plan_route 算出方案后,把结果"落"进工作台:
  // 地点表整体替换为方案顺序(规划带 chips 跟着变),规划带采用结果并补画轨迹。
  // 至此 agent 不只是"会说话",而是真的在操作这个应用的规划能力。
  //
  // skipPlanResetRef:下面的"points 变了就作废旧结果"不变量是给用户操作用的;
  // agent 同步是"换了一套地点+结果"的原子动作,reset 会把它刚写入的结果清掉,
  // 所以同步这一次要跳过。之后的用户编辑照常触发 reset。
  const skipPlanResetRef = useRef(false)
  const handleAgentPlan = useCallback(
    (ordered, result, mode) => {
      skipPlanResetRef.current = true
      replacePoints(ordered)
      plan.applyAgentPlan({ ordered, result, mode })
      push('RouteBot 的方案已同步到规划带', 'info')
    },
    [replacePoints, plan, push]
  )

  // ── 社区:把别人的路线快照"落"进规划器 ──
  // replacePoints 逐点恢复 name/lat/lng/legMode(段方式跟着点走,与分享时一致);
  // 换数据后 [points] effect 会作废旧规划结果 —— 正确,导入的就是新路线。
  // manual/mode 不恢复(YAGNI):用户导入后自己切,快照的 points 已足够复现路线。
  const handleImportSnapshot = useCallback(
    (snapshot) => {
      if (!snapshot?.points?.length) {
        push('这篇笔记没有可用的路线', 'error')
        return
      }
      replacePoints(snapshot.points)
      setView('planner')
      push('已按社区路线开始规划,可继续调整', 'info')
    },
    [replacePoints, push]
  )

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
    plan.run({ points, manual, mode })
  }, [canPlan, plan, points, manual, mode])

  // ── 地点一变，之前的规划结果就过期了 ──
  //
  // 加一个点、删一个点、换个出行方式 —— 旧路线立刻就不对了。
  // 与其在每个操作里记得手写一句 plan.reset()（漏一个就出现"结果和地点对不上"），
  // 不如写成一条规则：**只要 points 变了，就作废旧结果。**
  //
  // 例外:RouteBot 方案同步是"地点+结果"一起换的原子动作,
  // skipPlanResetRef 让这一次变更跳过 reset(见 handleAgentPlan)。
  useEffect(() => {
    if (skipPlanResetRef.current) {
      skipPlanResetRef.current = false
      return
    }
    plan.reset()
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
        view={view}
        onNavView={setView}
        onShare={() => {
          if (points.length < 2) {
            // 明确告诉用户"差什么、去哪补"—— 禁用按钮让人以为功能坏了
            push('先加地点:在地图上点一下,或用搜索/手动输入,至少 2 个才能分享', 'info')
            return
          }
          setShareOpen(true)
        }}
        onLogout={async () => {
          await auth.logout()
          push('已退出登录', 'info')
        }}
        onOpenProfile={() => setProfileOpen(true)}
      />
      <div className="hairline" />

      {view === 'community' ? (
        <CommunityView
          user={auth.user}
          onBack={() => setView('planner')}
          onImport={handleImportSnapshot}
          onError={showError}
        />
      ) : (
        <div className={`body${agentOpen ? '' : ' agent-hidden'}`}>
        <MapView
          mapApi={mapApi}
          points={points}
          segments={plan.segments}
          onAddFromMap={handleAddFromMap}
          fab={
            !agentOpen && (
              <button className="agent-fab" onClick={() => setAgentOpen(true)}>
                ◀ 打开 RouteBot
              </button>
            )
          }
        />

        <AgentPane onCollapse={() => setAgentOpen(false)} onPlan={handleAgentPlan} />

        <PlanStrip
          points={points}
          manual={manual}
          mode={mode}
          planPhase={plan.phase}
          planProgress={plan.progress}
          planResult={plan.result}
          planError={plan.error}
          canPlan={canPlan}
          blockReason={blockReason}
          blockTone={blockTone}
          onPickPlace={handlePickPlace}
          onAddManual={handleAddManual}
          onManualChange={setManual}
          onModeChange={handleModeChange}
          onRemove={removePoint}
          onMove={movePoint}
          onSetLegMode={setLegMode}
          onPlan={handlePlan}
          onClear={clearPoints}
          onError={showError}
        />
        </div>
      )}

      {/* 分享到社区:把当前 points 序列化为快照发布;成功后进社区看自己的帖子 */}
      {shareOpen && (
        <ShareDialog
          points={points}
          manual={manual}
          mode={mode}
          onClose={() => setShareOpen(false)}
          onPublished={(post) => {
            setView('community')
            push(`已发布「${post.title}」`, 'info')
          }}
          onError={showError}
        />
      )}

      {/* 账号资料弹层:保存/上传成功即同步 user 缓存,下次打开就是新值 */}
      {profileOpen && (
        <ProfileDialog
          user={auth.user}
          onClose={() => setProfileOpen(false)}
          onSaved={(patch) => auth.updateUser(patch)}
          onError={showError}
        />
      )}

      {/* 提示条堆栈。渲染在最外层，用 position:fixed 浮在页面上方 ——
          这样它出现在哪里不受面板的 overflow 影响。 */}
      <ToastStack toasts={toasts} onDismiss={dismiss} />
    </div>
  )
}
