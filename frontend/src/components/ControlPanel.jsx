// ControlPanel.jsx —— 左侧控制面板
//
// 这个组件的作用是**组装**：把四个模块按顺序排好，把从 App 收到的
// props 分发给需要的子组件。
//
// 它自己**没有 state** —— 这是刻意的。
// 数据都归 App 管（因为地图区也要用同一份 points），
// 面板只负责"摆位置 + 传话"。
//
// 判断一个组件该不该有自己的 state，问一个问题就够了：
// "这个值变了，是不是只有我自己关心？"
//   - 是 → 放自己这儿（比如搜索框里正在输入的文字、弹层的开合）
//   - 否 → 提升到最近的那个共同父组件（比如 points，列表和地图都要用）

import { SearchModule } from './SearchModule.jsx'
import { PointList } from './PointList.jsx'
import { PlanOptions } from './PlanOptions.jsx'
import { PlanAction } from './PlanAction.jsx'
import { ResultCard } from './ResultCard.jsx'

export function ControlPanel({
  points,
  manual,
  planPhase,
  planProgress,
  planResult,
  planError,
  canPlan,
  planBlockReason,
  planBlockTone,
  onPickPlace,
  onAddManual,
  onManualChange,
  onRemove,
  onMove,
  onSetLegMode,
  onPlan,
  onError,
}) {
  return (
    <section className="panel">
      <SearchModule onPick={onPickPlace} onError={onError} />

      <PointList
        points={points}
        manual={manual}
        onRemove={onRemove}
        onMove={onMove}
        onSetLegMode={onSetLegMode}
      />

      <PlanOptions
        manual={manual}
        onManualChange={onManualChange}
        pointCount={points.length}
        onAddManual={onAddManual}
      />

      <PlanAction
        phase={planPhase}
        progress={planProgress}
        pointCount={points.length}
        canPlan={canPlan}
        blockReason={planBlockReason}
        blockTone={planBlockTone}
        error={planError}
        onPlan={onPlan}
      />

      <ResultCard result={planResult} />
    </section>
  )
}
