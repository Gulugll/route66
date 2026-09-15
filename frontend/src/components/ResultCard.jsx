// ResultCard.jsx —— 规划结果
//
// 一个"纯展示组件"：给它 result，它画出来；没有 result，它返回 null。
// 没有 state、没有副作用、没有请求 —— 这类组件最好写也最好测。
//
// 拆出它的理由不只是"文件小一点"：把结果从面板里拆出来之后，
// 面板的代码里就只剩"布局"，一眼能看清页面的结构。

import { Icon } from './icons.jsx'
import { Chip } from './ui.jsx'

export function ResultCard({ result }) {
  // 没有结果就什么都不渲染 —— 返回 null 是合法的 JSX，
  // React 会把它当作"这里什么都没有"。
  if (!result) return null

  return (
    <div className="result-card">
      <div className="result-head">
        <span className="module-title">规划结果</span>
        <Chip>已规划</Chip>
      </div>

      <div className="result-block">
        <span className="result-label">访问顺序</span>
        {/* order 是**名字**数组，只用来给人看。
            画线用的是 order_idx（下标），见 usePlan.js 里的说明。 */}
        <span className="result-value">{result.order.join(' → ')}</span>
      </div>

      <div className="total-row">
        <span className="total-value">{result.total_km.toFixed(1)}</span>
        <span className="total-unit">km · 总距离</span>
      </div>

      {/* 降级警告必须显示出来。
          后端已经把"哪些段是估算的"一路带到了 HTTP 响应里（warnings +
          is_degraded），前端的责任就是**别把它藏起来**。
          用户在浏览器上看到的数字，和真实路网差多少，得让他知道。 */}
      {result.is_degraded && result.warnings?.length > 0 && (
        <div className="warning-bar">
          <Icon name="alertTriangle" size={14} style={{ flexShrink: 0, marginTop: 1 }} />
          <div>
            <div style={{ fontWeight: 500, marginBottom: 4 }}>注意</div>
            <ul className="warning-list">
              {/* key 用下标在这里是**可以**的：这个列表是只读的、
                  永远不会被重排或增删。用下标当 key 的禁忌只出现在
                  "列表会变"的场景。判断依据是行为，不是教条。 */}
              {result.warnings.map((w, i) => (
                <li key={i}>• {w}</li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  )
}
