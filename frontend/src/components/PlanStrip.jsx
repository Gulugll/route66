// PlanStrip.jsx —— 底部规划带（横向工具条）
//
// 吸收原左侧面板的全部能力,重排为五个纵向分节的通栏:
//   添加地点 ｜ 地点顺序(横向拖拽 chips) ｜ 规划方式 ｜ 执行 ｜ 结果
// 状态全部归 App(地图/Agent 面板共用同一份 points),本组件只摆位置 + 传话。
//
// 拖拽排序与原 PointList 同一套 HTML5 DnD 逻辑:
// 跨事件传递的下标放 ref(写入立即生效),视觉态放 state。

import { useRef, useState } from 'react'
import { SearchModule } from './SearchModule.jsx'
import { Icon } from './icons.jsx'
import { MODES, MODE_ICONS, MODE_LABELS } from '../theme.js'

const NEXT_MODE = { driving: 'walking', walking: 'transit', transit: 'driving' }

export function PlanStrip({
  points,
  manual,
  mode,
  planPhase,
  planProgress,
  planResult,
  planError,
  canPlan,
  blockReason,
  blockTone,
  onPickPlace,
  onAddManual,
  onManualChange,
  onModeChange,
  onRemove,
  onMove,
  onSetLegMode,
  onPlan,
  onClear,
  onError,
}) {
  const dragIndexRef = useRef(null)
  const [dragIndex, setDragIndex] = useState(null)
  const [overIndex, setOverIndex] = useState(null)

  // 手动输入坐标:工具条空间有限,默认折叠,点开才出现表单
  const [showManual, setShowManual] = useState(false)
  const [manualDraft, setManualDraft] = useState({ name: '', lat: '', lng: '' })
  const [manualErr, setManualErr] = useState('')

  function submitManual() {
    const lat = Number.parseFloat(manualDraft.lat)
    const lng = Number.parseFloat(manualDraft.lng)
    if (Number.isNaN(lat) || Number.isNaN(lng)) {
      setManualErr('纬度/经度必须是数字')
      return
    }
    if (lat < -90 || lat > 90 || lng < -180 || lng > 180) {
      setManualErr('坐标超出有效范围')
      return
    }
    setManualErr('')
    onAddManual({ name: manualDraft.name, lat, lng })
    setManualDraft({ name: '', lat: '', lng: '' })
  }

  function handleDrop(toIndex) {
    const from = dragIndexRef.current
    if (from !== null) onMove(from, toIndex)
    dragIndexRef.current = null
    setDragIndex(null)
    setOverIndex(null)
  }

  const busy = planPhase === 'submitting' || planPhase === 'drawing'

  return (
    <section className="plan-strip" aria-label="路径规划">

      <div className="plane-sec">
        <div className="sec-label"><b>添加地点</b>搜索地名或输入坐标</div>
        <SearchModule onPick={onPickPlace} onError={onError} />
        {showManual ? (
          <div>
            <div className="manual-form">
              <input placeholder="名称" value={manualDraft.name}
                onChange={(e) => setManualDraft({ ...manualDraft, name: e.target.value })} />
              <input placeholder="纬度" value={manualDraft.lat}
                onChange={(e) => setManualDraft({ ...manualDraft, lat: e.target.value })} />
              <input placeholder="经度" value={manualDraft.lng}
                onChange={(e) => setManualDraft({ ...manualDraft, lng: e.target.value })} />
              <button className="btn-ghost" onClick={submitManual}>加一个</button>
            </div>
            {manualErr && <div className="manual-err">{manualErr}</div>}
          </div>
        ) : (
          <button className="manual-toggle" onClick={() => setShowManual(true)}>
            或手动输入坐标 ↓
          </button>
        )}
      </div>

      <div className="plane-sec">
        <div className="sec-label"><b>地点顺序</b>拖动排序 · 共 {points.length} 站</div>
        <div className="chips">
          {points.length === 0 && <span className="chip-empty">还没有地点</span>}
          {points.map((p, i) => (
            <span
              key={p.id}
              className={`chip-point${i === 0 ? ' origin' : ''}${dragIndex === i ? ' is-dragging' : ''}${overIndex === i && dragIndex !== null && dragIndex !== i ? ' is-over' : ''}`}
              draggable
              onDragStart={(e) => {
                dragIndexRef.current = i
                setDragIndex(i)
                e.dataTransfer.effectAllowed = 'move'
              }}
              onDragOver={(e) => {
                e.preventDefault()
                if (overIndex !== i) setOverIndex(i)
              }}
              onDrop={(e) => {
                e.preventDefault()
                handleDrop(i)
              }}
              onDragEnd={() => {
                dragIndexRef.current = null
                setDragIndex(null)
                setOverIndex(null)
              }}
            >
              <span className="chip-handle">≡</span>
              <span className="chip-badge">{i === 0 ? '起点' : `第${i}站`}</span>
              {p.name}
              {/* legMode 挂在"点"上表示"从这点到下一站怎么走",
                  因此最后一个点没有下一站,不显示切换按钮 */}
              {manual && i < points.length - 1 && (
                <button
                  className="chip-mode"
                  data-mode={p.legMode}
                  title={`到下一站：${MODE_LABELS[p.legMode] || '驾车'}（点击切换）`}
                  aria-label={`到下一站：${MODE_LABELS[p.legMode] || '驾车'}`}
                  onClick={() => onSetLegMode(p.id, NEXT_MODE[p.legMode || 'driving'])}
                >
                  <Icon name={MODE_ICONS[p.legMode] || 'car'} size={13} />
                </button>
              )}
              <button className="chip-x" onClick={() => onRemove(p.id)} aria-label={`删除 ${p.name}`}>✕</button>
            </span>
          ))}
        </div>
      </div>

      <div className="plane-sec">
        <div className="sec-label"><b>规划方式</b></div>
        <div>
          <span className="seg">
            <button className={manual ? '' : 'on'} onClick={() => onManualChange(false)}>自动优化</button>
            <button className={manual ? 'on' : ''} onClick={() => onManualChange(true)}>手动顺序</button>
          </span>
        </div>
        <div className="mode-pills">
          {MODES.map((m) => (
            <button
              key={m}
              className={`mode-pill${mode === m ? ' on' : ''}`}
              onClick={() => onModeChange(m)}
              title={manual ? `应用到全部路段` : `自动模式的全局出行方式`}
            >
              <i style={{ background: `var(--${m})` }} />
              {MODE_LABELS[m]}
            </button>
          ))}
        </div>
      </div>

      <div className="plane-sec action-sec">
        <div className="sec-label"><b>执行</b></div>
        <button className="btn-primary" disabled={!canPlan || busy} onClick={onPlan}>
          {busy ? '规划中…' : '开始规划'}
        </button>
        <div className={`action-msg ${blockTone}`}>
          {busy
            ? `第 ${planProgress.done}/${planProgress.total} 段取路网…`
            : blockReason || (
                <button className="btn-text" onClick={onClear}>清空全部地点</button>
              )}
        </div>
      </div>

      <div className="plane-sec grow" aria-label="规划结果">
        <div className="sec-label"><b>规划结果</b></div>
        {planError ? (
          <div className="action-msg danger">{planError}</div>
        ) : planResult ? (
          <div className="result-line">
            <span className="result-order">
              {planResult.order.map((name, i) => (
                <span key={i}>
                  {i > 0 && <span className="arrow">→</span>}
                  {name}
                </span>
              ))}
            </span>
            <span className="result-km">{planResult.total_km.toFixed(1)}<small>km</small></span>
            {planResult.is_degraded &&
              planResult.warnings?.map((w, i) => (
                <span key={i} className="warn-chip">⚠ {w}</span>
              ))}
          </div>
        ) : (
          <span className="chip-empty">添加至少两个地点后开始规划</span>
        )}
      </div>

    </section>
  )
}
