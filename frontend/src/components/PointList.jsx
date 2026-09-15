// PointList.jsx —— 地点列表（可拖拽排序 + 每段出行方式）
//
// ⭐ 这个文件是整次改版最值得看的地方 —— 它直接解决了手写版那个"坑"。
//
// 手写版的困境（app.js 里那段注释写得很明白）：
//     renderList() 会重建所有 <li>（旧元素脱离 DOM），
//     Sortable 持有的元素引用会失效，所以重建后必须 destroy 再 new 重新绑定。
// 于是代码里出现了一个可重入的 initSortable()，以及"改了 state 之后
// 要记得调 renderList + redrawMarkers + initSortable"这种隐性约定。
//
// 现在为什么不需要了？
//
// React 重排列表时**不会重建 DOM 节点**，它靠 key 认出"还是同一个东西"，
// 然后**移动**那个已经存在的节点。节点从头到尾是同一条命，
// 所以任何持有它的第三方（拖拽库、地图实例、canvas）都不会失效。
//
// 换句话说：手写版的问题是"用 DOM 当数据存储 + 每次全量重建"，
// React 用 key 做 diff 恰好把这两点都解决了。
//
// 拖拽本身这里用的是浏览器原生 HTML5 DnD，没引入第三方库。
// 因为"拖拽"在 React 里可以纯靠 state 表达：拖到谁头上 → setOverIndex(i)，
// 松手 → 把新顺序写进数组。全程没有一个 DOM 操作。

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
          把"段"放进同一个 li 里，好处是 <ul> 只有一种子元素，
          拖拽时"第几个"和"数组下标"天然一一对应，不用做换算。
          （手写版是把段连接线作为兄弟节点插在中间，于是 Sortable 的
           oldIndex 把连接线也算进去了，必须改用 oldDraggableIndex —— 那个
           bug 就是这么来的。数据结构设计得好，一整类 bug 就不会出现。）*/}
      <ul className="point-list">
        {points.map((p, i) => (
          <li
            key={p.id}
            className="point-li"
            draggable
            onDragStart={(e) => {
              dragIndexRef.current = i // 立刻生效，drop 时一定读得到
              setDragIndex(i) // 只为了把这张卡变虚
              // 有些浏览器不设 effectAllowed 就不让 drop，设一下更稳
              e.dataTransfer.effectAllowed = 'move'
            }}
            onDragOver={(e) => {
              // ⚠️ 必须 preventDefault，否则浏览器认为"这里不接受放置"，
              // drop 事件根本不会触发。这是 HTML5 DnD 最经典的坑。
              e.preventDefault()
              if (overIndex !== i) setOverIndex(i)
            }}
            onDrop={(e) => {
              e.preventDefault()
              handleDrop(i)
            }}
            onDragEnd={() => {
              // dragEnd 一定会触发（包括拖到外面松手），
              // 所以清理状态放这里最保险，不会留下一个"永远在拖"的幽灵。
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

              {/* 坐标用 <span>，JSX 会自动把内容当**文本**插入。
                  手写版必须自己写 esc() 把 < > & 转义，因为那里是
                  innerHTML = 拼字符串。JSX 里 {变量} 天然就是文本节点，
                  所以一个叫 <img onerror=...> 的地点名在这里只是普通文字。
                  这就是为什么这个 React 版里一行 esc() 都不需要 ——
                  和 Go 的 html/template 自动转义是同一类保障。 */}
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
