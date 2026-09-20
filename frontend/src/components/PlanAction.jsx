// PlanAction.jsx —— 「开始规划」按钮 + 规划中的进度
//
// 解算可能耗时数十秒(步行/公交为逐对请求,10 个点约 90 次请求 ≈ 32 秒),
// 长请求期间必须给出可见反馈,否则无法区分"在算"和"卡死"。
//
// 这里做两件事:
//   1) 按钮进入禁用态 + 转圈 + 文案变成"规划中…"
//   2) 把 usePlan 的 phase 翻译成进度项,展示当前执行阶段
//
// 注:生产环境长任务应做成异步任务(提交 → task_id → 轮询/推送),
// 即本项目 Phase 2 的方案;当前是同步长请求下的反馈上限。

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

      {/* 规划失败:错误就放在按钮下面。
          刻意不用顶部提示条:提示条数秒后自动消失,
          而"这次规划为什么失败"是用户需要持续查看、照着改的信息。
          错误展示位置的选取原则:用户需不需要对着它办事——
          需要 → 就地、常驻;不需要 → 短暂提示条(如搜索失败)。 */}
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
