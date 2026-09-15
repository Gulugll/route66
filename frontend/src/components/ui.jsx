// ui.jsx —— 小积木（可复用的基础组件）
//
// 判断"该不该抽组件"的标准不是行数，而是**重复次数**：
// 同一段结构出现 3 次以上才值得抽。下面这几个都是这个标准下的产物。
//
// SectionHeader 用了 4 次、TextField 用了 3 次、Checkbox 和 Chip 各 1~2 次。
// 抽出来的好处不只是少写代码 —— 更重要的是"改一处，全局生效"。

import { useId } from 'react'
import { Icon } from './icons.jsx'

/** 模块标题行：左边标题，右边一句说明。 */
export function SectionHeader({ title, hint }) {
  return (
    <div className="module-head">
      <span className="module-title">{title}</span>
      {hint && <span className="module-hint">{hint}</span>}
    </div>
  )
}

/** 状态小徽标。 */
export function Chip({ children }) {
  return <span className="chip">{children}</span>
}

/**
 * 受控输入框 = 标签 + 输入框 + 错误提示。
 *
 * 注意这是"受控组件"：value 由父组件传进来，输入时只调 onChange。
 * input 自己**不保存**任何状态。好处是"界面上显示什么"永远等于
 * "state 里是什么"，不会出现两边不一致。手写版就是靠 document.getElementById
 * 到处读值（比如 $('cityInput').value.trim()），谁改了值、什么时候改的，
 * 得满文件找。
 */
export function TextField({
  label,
  value,
  onChange,
  placeholder,
  error,
  size = 'md',
  icon,
  suffix,
  onEnter,
  inputRef,
}) {
  // useId 生成一个全局唯一的 id，把 <label> 和 <input> 关联起来。
  // 为什么要 useId 而不是自己写 id="city"？因为同一个组件可能在页面上
  // 出现多次，写死 id 就会重复（id 全页必须唯一）。useId 由 React 保证唯一。
  const id = useId()

  return (
    <div className="field">
      {label && (
        <label className="field-label" htmlFor={id}>
          {label}
        </label>
      )}

      <div className={`input-wrap input-wrap--${size}${error ? ' is-error' : ''}`}>
        {icon && <Icon name={icon} size={icon === 'search' ? 15 : 14} style={{ color: 'var(--text-tertiary)', flexShrink: 0 }} />}
        <input
          id={id}
          ref={inputRef}
          className="input"
          value={value}
          placeholder={placeholder}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && onEnter) onEnter()
          }}
        />
        {suffix && <span className="input-suffix">{suffix}</span>}
      </div>

      {error && (
        <div className="error-row">
          <Icon name="alertCircle" size={13} style={{ flexShrink: 0, marginTop: 1 }} />
          <span>{error}</span>
        </div>
      )}
    </div>
  )
}

/**
 * 自绘复选框。
 *
 * 关键细节：真的 <input type="checkbox"> 仍在 DOM 里（用 .sr-only 视觉隐藏），
 * 只是外面套了一层自己画的方框。
 * 为什么不能直接用 <div onClick>？那样键盘用户按不了空格、读屏软件读不出"这是个复选框"。
 * 自绘控件的第一原则就是：**别丢掉原生语义**。
 */
export function Checkbox({ checked, onChange, children, disabled }) {
  return (
    <label className={`checkbox-row${disabled ? ' is-disabled' : ''}`}>
      <input
        type="checkbox"
        className="sr-only"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className={`checkbox${checked ? ' is-checked' : ''}`}>
        {/* 只在选中时渲染勾 —— 条件渲染的常见形态：cond && <元素/> */}
        {checked && <Icon name="check" size={10} />}
      </span>
      <span>{children}</span>
    </label>
  )
}

/**
 * 提示条堆栈 —— 替代手写版的 alert()。
 *
 * 手写版所有错误都用浏览器 alert() 弹：不可控、打断流程、样式和产品无关。
 * 这里改成页面上方的内联提示条，可以关掉，也不阻塞操作。
 *
 * 组件是纯函数：给什么 errors 就画什么，自己不记录状态。
 * "怎么出现、怎么消失"由父组件（App）的 state 决定 —— 这叫"状态提升"。
 */
const TOAST_META = {
  error: { icon: 'alertCircle' },
  info: { icon: 'alertTriangle' },
}

export function ToastStack({ toasts, onDismiss }) {
  if (toasts.length === 0) return null // 没东西就不渲染 —— 返回 null 是合法 JSX

  return (
    <div className="toast-stack" role="status" aria-live="polite">
      {toasts.map((t) => {
        const meta = TOAST_META[t.kind] || TOAST_META.error
        return (
          <div key={t.id} className={`toast${t.kind === 'info' ? ' toast--info' : ''}`}>
            <Icon name={meta.icon} size={14} style={{ flexShrink: 0, marginTop: 2 }} />
            <span style={{ flex: 1 }}>{t.text}</span>
            <button className="toast-close" onClick={() => onDismiss(t.id)} aria-label="关闭提示">
              <Icon name="close" size={13} />
            </button>
          </div>
        )
      })}
    </div>
  )
}
