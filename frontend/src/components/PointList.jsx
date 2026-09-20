// PointList.jsx —— 地点列表(可拖拽排序 + 每段出行方式)
//
// 为什么 React 下不需要手动管理拖拽库的元素引用:
// React 重排列表时不重建 DOM 节点,而是靠 key 识别"同一个元素"并移动它。
// 节点引用稳定,任何持有它的第三方(拖拽库、地图实例、canvas)都不会失效。
// 对比:全量重建 DOM 的方案(如"renderList() 重建所有 <li>")必须
// destroy 再重新绑定第三方库,并维护"改了 state 之后要手动调哪些刷新"的隐性约定。
//
// 拖拽本身用浏览器原生 HTML5 DnD,不引入第三方库:
// "拖到谁头上 → setOverIndex(i),松手 → 写入新顺序",
// 全程只改 state,不操作 DOM。

import { useRef, useState } from 'react'
import { Icon } from './icons.jsx'
import { MODE_ICONS, MODES, MODE_LABELS } from '../theme.js'
import { SectionHeader } from './ui.jsx'

/** 单段的出行方式选择器：连接线 + 第 N 段 + 三个方式按钮。 */
function LegRow({ index, mode, onChange }) {
  return (
    <div className="leg-row">
      <span className="leg-line" />
      <span className="leg-label">第{index + 1}段</span>

      {MODES.map((m) => {
        const active = m === mode
        return (
          <button
            key={m}
            type="button"
            // data-mode 让 CSS 能按出行方式给选中态上色（见 app.css）。
            // 这样"颜色 = 什么语义"这件事完全留在样式层，
            // JS 里不需要出现任何色值。
            data-mode={m}
            className={`mode-btn${active ? ' is-active' : ''}`}
            // aria-pressed 告诉读屏："这是个开关，当前是/否按下"。
            // 视觉上靠颜色区分选中态，对看不见颜色的人就完全丢失了信息 ——
            // 所以要额外提供语义。
            aria-pressed={active}
            aria-label={MODE_LABELS[m]}
            title={MODE_LABELS[m]}
            onClick={() => onChange(m)}
          >
            <Icon name={MODE_ICONS[m]} size={14} />
          </button>
        )
      })}

      <span className="leg-line" />
    </div>
  )
}

export function PointList({ points, manual, onRemove, onMove, onSetLegMode }) {
  // 拖拽状态：谁在被拖、当前悬在谁头上。
  //
  // ⚠️ 这里有个容易踩的坑，值得单独说明：
  // "被拖的是第几个"这个值，dragstart 时写下、drop 时读出来。
  // 如果用 useState 存，drop 处理器有可能读到**旧闭包里的 null** ——
  // 因为 state 的更新不保证在下一个事件之前已经生效。
  // 真实拖拽时中间会有一堆 dragover 事件触发重渲染，通常"碰巧"是好的，
  // 但那是运气，不是保证。
  //
  // 正确做法：**要跨事件传递的值放 ref**。ref 的写入是立即生效的，
  // 读到的永远是刚写的那个值。
  // state 只用来管**视觉**（拖拽中要变虚、悬停目标要描边）。
  const dragIndexRef = useRef(null)
  const [dragIndex, setDragIndex] = useState(null)
  const [overIndex, setOverIndex] = useState(null)

  function handleDrop(toIndex) {
    const from = dragIndexRef.current
    if (from !== null) onMove(from, toIndex)
    dragIndexRef.current = null
    setDragIndex(null)
    setOverIndex(null)
  }

  return (
    <div className="module">
      <SectionHeader title="地点顺序" hint="拖动可排序" />

      {points.length === 0 && (
        <p className="option-hint" style={{ color: 'var(--text-tertiary)' }}>
          还没有地点。搜索添加、点地图，或在下面手动输入坐标。
        </p>
      )}

      {/* 一个 <li> = 一个地点 + 它到下一站的那一段。
          把"段"放进同一个 li 里,<ul> 只有一种子元素,
          拖拽序号和数组下标天然一一对应,无需换算。
          (若把段连接线作为兄弟节点插在中间,拖拽的 oldIndex
           会把连接线也算进去,需要额外区分 draggableIndex。)*/}
      <ul className="point-list">
        {points.map((p, i) => (
          <li
            key={p.id}
            className="point-li"
            draggable
            onDragStart={(e) => {
              dragIndexRef.current = i // ref 立即生效,drop 时一定读得到
              setDragIndex(i)          // 仅用于当前卡的视觉态
              // 部分浏览器不设置 effectAllowed 就不触发 drop
              e.dataTransfer.effectAllowed = 'move'
            }}
            onDragOver={(e) => {
              // ⚠️ 必须 preventDefault,否则浏览器认为"此处不接受放置",
              // drop 事件不会触发。HTML5 DnD 的必要步骤。
              e.preventDefault()
              if (overIndex !== i) setOverIndex(i)
            }}
            onDrop={(e) => {
              e.preventDefault()
              handleDrop(i)
            }}
            onDragEnd={() => {
              // dragEnd 必然触发(包括拖出区域后松手),
              // 状态清理统一放在这里,避免残留拖拽态。
              dragIndexRef.current = null
              setDragIndex(null)
              setOverIndex(null)
            }}
          >
            <div
              className={[
                'point-card',
                dragIndex === i ? 'is-dragging' : '',
                overIndex === i && dragIndex !== null && dragIndex !== i ? 'is-drop-target' : '',
              ]
                .filter(Boolean)
                .join(' ')}
            >
              <span className={`badge${i === 0 ? ' badge--origin' : ''}`}>
                {i === 0 ? '起点' : `第${i}站`}
              </span>

              <span className="point-name" title={p.name}>
                {p.name}
              </span>

              {/* 坐标用 <span>,JSX 的 {变量} 是文本节点,内容自动转义。
                  恶意地点名(如 <img onerror=...>)只会显示为普通文字,
                  与 Go 的 html/template 自动转义是同一类保障。 */}
              <span className="point-coords">
                {p.lng.toFixed(2)}, {p.lat.toFixed(2)}
              </span>

              <button
                className="icon-btn"
                onClick={() => onRemove(p.id)}
                aria-label={`删除 ${p.name}`}
                title="删除"
              >
                <Icon name="close" size={14} style={{ color: 'var(--text-tertiary)' }} />
              </button>
            </div>

            {/* 最后一点之后没有"下一段"。自动模式（TSP）下顺序由算法决定，
                用户选的段方式会被忽略，所以干脆不显示，避免误导。 */}
            {manual && i < points.length - 1 && (
              <LegRow index={i} mode={p.legMode} onChange={(m) => onSetLegMode(p.id, m)} />
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
