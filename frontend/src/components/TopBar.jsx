// TopBar.jsx —— 顶栏
//
// 最简单的组件：两个 props（标题、点设置的处理器），一段 JSX。
// 看得出来 React 的"组件"可以有多轻 —— 不需要"够复杂"才配抽成组件，
// 只要它能让你在别处一眼看懂结构，就值得抽。

import { Icon } from './icons.jsx'

export function TopBar({ onOpenSettings }) {
  return (
    <header className="topbar">
      <div className="brand">
        <Icon name="route" size={26} style={{ color: 'var(--driving)' }} />
        <span className="brand-title">路线规划器</span>
      </div>

      <button className="btn-ghost" onClick={onOpenSettings}>
        <Icon name="sliders" size={14} />
        设置
      </button>
    </header>
  )
}
