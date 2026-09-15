// SettingsDialog.jsx —— 设置弹层
//
// 一个值得注意的写法：这个组件**不接收 open 属性**。
// 打开/关闭由父组件决定 —— App 里写的是 {settingsOpen && <SettingsDialog .../>}，
// 也就是"打开"= 挂载，"关闭"= 卸载。
//
// 这样做的好处是**状态自动重置**：
// 下次打开时组件是全新的，useState 的初始值会被重新求值，
// 输入框自动回到"当前保存的 key"。
// 如果改成"始终挂载、用 open 控制显示隐藏"，就得额外写一个 effect
// 去同步 props 到 state —— 那是 React 里最容易写出 bug 的一类代码。
//
// 记住这条经验：**能靠"挂载/卸载"重置的状态，就别用 effect 去同步。**

import { useEffect, useRef, useState } from 'react'
import { Icon } from './icons.jsx'
import { TextField } from './ui.jsx'

export function SettingsDialog({ initialKey, initialJscode, onSave, onClose }) {
  // 惰性初始化：只在挂载时跑一次，直接拿"已保存的值"作为初始值。
  const [key, setKey] = useState(() => initialKey)
  const [jscode, setJscode] = useState(() => initialJscode)
  const [error, setError] = useState('')

  const firstFieldRef = useRef(null)

  // 打开就聚焦第一个输入框 —— 键盘用户不用先 Tab 一遍。
  useEffect(() => {
    firstFieldRef.current?.focus()
  }, [])

  // Esc 关闭。这是个真实的"副作用"（往 document 上挂监听），
  // 所以必须在 cleanup 里摘掉，否则弹层关了监听还在。
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  function handleSave() {
    if (!key.trim()) {
      setError('key 不能为空')
      return
    }
    onSave(key, jscode)
  }

  return (
    // 点遮罩关闭。stopPropagation 让"点对话框内部"不会冒泡上去触发关闭。
    <div className="overlay" onClick={onClose} role="presentation">
      <div
        className="dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="设置"
      >
        <span className="dialog-title">设置</span>

        {/* 用 TextField 的 label，而不是手写 <label> ——
            顺便就拿到了 useId 生成的唯一 id 和 label/input 的关联。 */}
        <TextField
          label="高德 JS API Key（地图用）"
          value={key}
          onChange={(v) => {
            setKey(v)
            setError('')
          }}
          onEnter={handleSave}
          placeholder="粘贴「Web端(JS API)」平台的 key"
          error={error}
          inputRef={firstFieldRef}
        />

        {/* 标签里**不要**写「可选」：2021-12-02 之后申请的 key，
            JS API 2.0 强制要求安全密钥。写成"可选"会让用户留空，
            然后现象是"地图能看、搜索不能用"——最难查的那种。
            实测报错是 INVALID_USER_SCODE。 */}
        <TextField
          label="安全密钥"
          value={jscode}
          onChange={setJscode}
          placeholder="控制台 key 详情里的那串，必填"
        />

        <p className="dialog-hint">
          这个框只管<strong>浏览器端</strong>的地图渲染，key 只存在你本地。
          <br />
          <strong>安全密钥不能留空</strong>：控制台 key 详情里另给一串，
          2021-12-02 之后申请的 key 强制要求。缺了它地图照样显示，
          但搜索、算距离会报 INVALID_USER_SCODE。
          <br />
          地名搜索与真实路网由<strong>后端</strong>代理，那需要<strong>另一把</strong>
          「Web服务」平台的 key（环境变量 AMAP_KEY）。
          两把可合成一把：在控制台给现有 key 追加「Web服务」平台。
        </p>

        <div className="dialog-actions">
          <button className="btn btn--primary" onClick={handleSave}>
            保存并重载地图
          </button>
          <button className="btn btn--ghost" onClick={onClose}>
            取消
          </button>
        </div>
      </div>
    </div>
  )
}
