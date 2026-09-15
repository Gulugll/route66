// PlanAction.jsx —— 「开始规划」按钮 + 规划中的进度
//
// ⭐ 这是相对手写版"从零到一"的新增：**反馈**。
//
// 手写版点完「开始规划」之后：按钮毫无变化，界面完全静止，
// 直到结果突然出现。点地图选点、逐段取路网可能要好几十秒
// （步行/公交是逐对请求，10 个点 90 次请求 ≈ 32 秒），
// 用户根本不知道是"在算"还是"卡死了"。
//
// 这里做两件事：
//   1) 按钮进入禁用态 + 转圈 + 文案变成"规划中…"
//   2) 把 usePlan 的 phase 如实翻译成三条进度项，让"现在在干什么"可见
//
// 顺带说：真实产品里长任务应该做成异步任务（提交 → 拿 task_id → 轮询/推送），
// 这正好是这个项目 Phase 2 要做的事（Redis Stream + 状态机）。
// 现在的进度反馈是"同步长请求"能做到的极限，也是它的直接动机。

import { Icon } from './icons.jsx'
import { PHASE } from '../hooks/usePlan.js'

/** 三种进度项状态对应的图标。 */
function StepIcon({ state }) {
  if (state === 'done') {
    // 绿勾。这个颜色是"通过/完成"的语义，不参与出行方式那套语义色。
    return <Icon name="checkCircle" size={14} style={{ color: 'var(--success)' }} />
  }
  if (state === 'active') {
    return <Icon name="clock" size={14} />
  }
  return <Icon name="circle" size={14} style={{ color: 'var(--border)' }} />
}

export function PlanAction({
  phase,
  progress,
  pointCount,
  canPlan,
  blockReason,
  blockTone,
  error,
  onPlan,
}) {
  const busy = phase === PHASE.SUBMITTING || phase === PHASE.DRAWING
  const done = phase === PHASE.DONE

  // 三条进度项的状态由 phase 推导出来 ——
  // 这是"声明式"的关键：没有一个变量叫 currentStep 需要手动 ++，
  // 界面是 phase 的函数。phase 一变，界面自己就对上了。
  const step1 = 'done' // 点了按钮就一定已经"读到"了 N 个地点
  let step2 = 'pending'
  let step3 = 'pending'

  if (phase === PHASE.SUBMITTING) step2 = 'active'
  else if (busy || done) step2 = 'done'

  if (phase === PHASE.DRAWING) step3 = 'active'
  else if (done) step3 = 'done'

  // 当前在第几段（用 0 基的 progress.done，界面上显示"第 N 段"要 +1）
  const currentLeg = Math.min(progress.done + 1, progress.total)

  const step3Label =
    phase === PHASE.DRAWING && progress.total > 0
      ? `第 ${currentLeg} 段 · 正在取真实路网`
      : done && progress.total > 0
        ? `已绘制 ${progress.total} 段路线`
        : '待绘制路线'

  return (
    <div className="module">
      <button
        className="btn btn--primary btn--block"
        onClick={onPlan}
        // 点不了的原因有两类，用 disabled 挡住交互，看起来一样，
        // 但语义上下面会把"为什么点不了"写出来 —— 只禁用不解释是最气人的交互。
        disabled={busy || !canPlan}
      >
        {busy ? (
          <>
            <Icon name="spinner" size={15} />
            规划中…
          </>
        ) : (
          <>
            <Icon name="arrowRight" size={16} />
            开始规划
          </>
        )}
      </button>

      {/* 不能规划的原因。只在"不是在跑、也点不了"的时候说 ——
          规划中不用重复解释，进度条已经说明了。
          语气分两档：只是"还没填够"用中性灰（用户没做错什么），
          "超过上限"才用红色（这是真的挡住了操作）。 */}
      {!busy && !canPlan && blockReason && (
        <p
          className="option-hint"
          style={{ color: blockTone === 'danger' ? 'var(--danger)' : 'var(--text-secondary)' }}
        >
          {blockReason}
        </p>
      )}

      {/* 规划失败：错误就放在按钮下面。
          ⭐ 这里有个刻意的选择：**不用顶部提示条**。
          提示条会 6 秒后自动消失，而"这次规划为什么失败"是用户要盯着看、
          甚至要照着改参数的信息 —— 它会消失就不合适。
          错误该放哪，取决于用户需不需要对着它办事：
            需要 → 就地、常驻（这里）
            不需要 → 短暂提示条（搜索失败那种） */}
      {!busy && error && (
        <div className="error-bar">
          <Icon name="alertCircle" size={14} style={{ flexShrink: 0, marginTop: 1 }} />
          <span>{error}</span>
        </div>
      )}

      {busy && (
        <div className="progress">
          <div className="progress-item progress-item--done">
            <StepIcon state={step1} />
            <span>已解析 {pointCount} 个地点</span>
          </div>
          <div className={`progress-item progress-item--${step2}`}>
            <StepIcon state={step2} />
            <span>
              {step2 === 'active' ? '正在计算距离与优化顺序' : '距离矩阵与访问顺序'}
            </span>
          </div>
          <div className={`progress-item progress-item--${step3}`}>
            <StepIcon state={step3} />
            <span>{step3Label}</span>
          </div>
        </div>
      )}

      {/* 把"可能要等很久"提前说清楚。
          这是很便宜的一招：用户知道自己要等 30 秒，和不知道要等多久，
          体验差别巨大 —— 前者会去做别的，后者会以为坏了然后狂点。 */}
      {busy && (
        <p className="option-hint" style={{ color: 'var(--text-tertiary)' }}>
          步行 / 公交逐段取数较慢，10 个点可能要等 30 秒以上。期间可以先去做别的事。
        </p>
      )}
    </div>
  )
}
